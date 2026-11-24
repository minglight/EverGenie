package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// testPNG 產生一張 64×64 的 PNG，seed 不同圖樣就不同。
func testPNG(seed int) []byte {
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((seed*37 + x*3 + y*(seed+1)) % 251)})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

type closetHome struct {
	dir  string
	sock string
	web  http.Handler
	img  map[string][]byte // 代號 → 真圖
}

// startClosetHome：fixture 家＋官方 closet 與 habits 套件（行程都編起來），builder 人設（agent code）的三張圖放進 personas/avatars/。
func startClosetHome(t *testing.T) *closetHome {
	t.Helper()
	t.Setenv("EVERGENIE_PACKAGE_BIN_DIR", buildPackageBinaries(t, "habits", "closet"))
	h := &closetHome{img: map[string][]byte{"builder": testPNG(1), "builder@night": testPNG(2), "builder@sea": testPNG(3)}}
	h.dir, h.sock, h.web = startFixtureWebWith(t, func(dir string) {
		copyOfficialPackage(t, dir, "habits")
		copyOfficialPackage(t, dir, "closet")
		for code, data := range h.img {
			p := filepath.Join(dir, "personas", "avatars", code+".png")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Setenv("EVERGENIE_AGENT", "code")
	pkgs := sdk.NewPackageClient(sdk.Dial(h.sock))
	for _, id := range []string{"closet", "habits"} {
		waitUntil(t, id+" 套件 running", 30*time.Second, func() bool {
			r, err := pkgs.Get(context.Background(), &sdk.PackageGetRequest{ID: id})
			if err != nil {
				t.Fatal(err)
			}
			if r.Package.State == sdk.PackageStateFailed || r.Package.State == sdk.PackageStateRejected {
				t.Fatalf("%s 套件壞了：%s %s", id, r.Package.State, r.Package.Error)
			}
			return r.Package.State == sdk.PackageStateRunning
		})
	}
	return h
}

func (h *closetHome) outfit(t *testing.T, sock, id string, cost int, state string) {
	t.Helper()
	persona, outfit, _ := strings.Cut(id, "-")
	body, _ := json.Marshal(map[string]any{"persona": persona, "outfit": outfit, "name": strings.ToUpper(outfit), "cost": cost, "state": state, "source": "official"})
	hgOK(t, sock, "data", "add", "closet.outfit", "--id", id, "--json", string(body))
}

func (h *closetHome) fields(t *testing.T, kind, id string) map[string]any {
	t.Helper()
	code, out, _ := hg(t, h.sock, "", "data", "get", kind, id, "--json")
	if code != 0 {
		return nil
	}
	return jsonLine(t, out)["fields"].(map[string]any)
}

// seedTwoPoints 照 TestHabitsPackageRunsOnFixtureHome：一個每週 1 次的習慣、前兩週各打一次卡 → 1 點，
// fixture 家是新家，第一個習慣另外送起手禮 1 點（HAB-20）→ 共 2 點。
func (h *closetHome) seedTwoPoints(t *testing.T) {
	t.Helper()
	now := time.Now()
	monday := now.AddDate(0, 0, -((int(now.Weekday())+6)%7)-14)
	d1, d2 := monday.Format("2006-01-02"), monday.AddDate(0, 0, 7).Format("2006-01-02")
	id := strings.TrimSpace(hgOK(t, h.sock, "data", "add", "habits.habit", "--json",
		`{"name":"重訓","agent":"code","cadence":"weekly","target":1,"mode":"cumulative","start":"`+d1+`","status":"active"}`))
	for _, d := range []string{d1, d2} {
		hgOK(t, h.sock, "data", "add", "habits.checkin", "--json", `{"habit":"`+id+`","date":"`+d+`","source":"manual","key":"manual:`+id+`:`+d+`:"}`)
	}
	waitUntil(t, "closet.state 看到 2 點", 30*time.Second, func() bool {
		f := h.fields(t, "closet.state", "main")
		return f != nil && f["points"] == float64(2) && f["wallet"] == true
	})
}

func (h *closetHome) get(path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// request 以目前的身分（EVERGENIE_AGENT）送一筆兌換請求，回編號。
func (h *closetHome) request(t *testing.T, persona, outfit string) string {
	t.Helper()
	body := `{"type":"unlock","persona":"` + persona + `","outfit":"` + outfit + `","state":"pending"}`
	return strings.TrimSpace(hgOK(t, h.sock, "data", "add", "closet.request", "--json", body))
}

func (h *closetHome) waitRequest(t *testing.T, id string) map[string]any {
	t.Helper()
	var f map[string]any
	waitUntil(t, "請求 "+id+" 被處理", 20*time.Second, func() bool {
		f = h.fields(t, "closet.request", id)
		return f != nil && f["state"] != "pending"
	})
	return f
}

// TestClosetPackageRunsOnFixtureHome：真 serve＋fixture 家：closet 與 habits 行程都起來，六種資料註冊完整；
// 建 base 與 night 兩套、種出 2 點（習慣 1 點＋起手禮 1 點）後，closet.state 與 closet.person 算出來。
// Guards: REQ-125
func TestClosetPackageRunsOnFixtureHome(t *testing.T) {
	h := startClosetHome(t)
	kinds := hgOK(t, h.sock, "data", "kinds")
	for _, want := range []string{"closet.outfit", "closet.unlock", "closet.rotation", "closet.request", "Q-<數字>", "closet.person", "closet.state", "enum(active|draft|retired)", "enum(official|mine)"} {
		if !strings.Contains(kinds, want) {
			t.Errorf("data kinds 少了「%s」：\n%s", want, kinds)
		}
	}
	// 編號對不上的衣服資料格式本身收得下，但程式會略過（不出現在 person）；cost 缺少被擋。
	if code, _, errOut := hg(t, h.sock, "", "data", "add", "closet.outfit", "--id", "builder-x", "--json", `{"persona":"builder","outfit":"x","name":"X","state":"active","source":"official"}`); code == 0 || !strings.Contains(errOut, "cost") {
		t.Fatalf("缺 cost 要被擋：%d %q", code, errOut)
	}
	h.outfit(t, h.sock, "builder-base", 0, "active")
	h.outfit(t, h.sock, "builder-night", 1, "active")
	hgOK(t, h.sock, "data", "add", "closet.outfit", "--id", "builder-bogus", "--json", `{"persona":"builder","outfit":"other","name":"對不上","cost":0,"state":"active","source":"official"}`)
	h.seedTwoPoints(t)

	var person map[string]any
	waitUntil(t, "closet.person 算出兩套", 20*time.Second, func() bool {
		person = h.fields(t, "closet.person", "builder")
		return person != nil && len(person["outfits"].([]any)) == 2
	})
	if person["name"] != "Builder" || person["agent"] != "code" || person["worn"] != "base" {
		t.Fatalf("person：%v", person)
	}
	outfits := person["outfits"].([]any)
	base, night := outfits[0].(map[string]any), outfits[1].(map[string]any)
	if base["outfit"] != "base" || base["owned"] != true || night["owned"] != false {
		t.Fatalf("outfits：%v", outfits)
	}
	if st := h.fields(t, "closet.state", "main"); st["earned"] != float64(2) || st["spent"] != float64(0) {
		t.Fatalf("state：%v", st)
	}
}

// TestClosetPublicRouteHidesLocked：沒兌換的真圖在公開路由 404、只有剪影；秘書送的兌換請求被拒、主人送的成功；
// 兌換後才有真圖；再送一次被拒；點數不夠時第三套被拒。
// Guards: REQ-125, REQ-175
func TestClosetPublicRouteHidesLocked(t *testing.T) {
	h := startClosetHome(t)
	h.outfit(t, h.sock, "builder-base", 0, "active")
	h.outfit(t, h.sock, "builder-night", 1, "active")
	h.outfit(t, h.sock, "builder-sea", 2, "active") // 2 點：night 花掉 1 點之後只剩 1 點，換不起
	h.seedTwoPoints(t)

	waitUntil(t, "剪影出現", 20*time.Second, func() bool { return h.get("/pkg-files/closet/s/builder/night/1.png").Code == http.StatusOK })
	sil := h.get("/pkg-files/closet/s/builder/night/1.png")
	if sil.Header().Get("Content-Type") != "image/png" || bytes.Equal(sil.Body.Bytes(), h.img["builder@night"]) {
		t.Fatalf("剪影不能等於真圖：%v", sil.Header())
	}
	if w := h.get("/pkg-files/closet/o/builder/night/1.png"); w.Code != http.StatusNotFound {
		t.Fatalf("兌換前真圖要 404：%d", w.Code)
	}
	if w := h.get("/pkg-files/closet/o/builder/base/1.png"); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), h.img["builder"]) {
		t.Fatalf("免費的 base 要公開：%d", w.Code)
	}

	// 秘書（EVERGENIE_AGENT=code）送的請求被拒。
	q := h.request(t, "builder", "night")
	if f := h.waitRequest(t, q); f["state"] != "rejected" || !strings.Contains(f["reason"].(string), "主人") {
		t.Fatalf("秘書送的請求：%v", f)
	}
	if w := h.get("/pkg-files/closet/o/builder/night/1.png"); w.Code != http.StatusNotFound {
		t.Fatalf("被拒之後真圖仍要 404：%d", w.Code)
	}
	// 秘書直接寫 closet.unlock 也沒用：被封存、真圖仍不公開。
	hgOK(t, h.sock, "data", "add", "closet.unlock", "--id", "builder-sea", "--json", `{"persona":"builder","outfit":"sea","cost":0,"at":"2026-10-26T10:00:00+08:00"}`)
	waitUntil(t, "偽造的 unlock 被封存", 20*time.Second, func() bool {
		return h.fields(t, "closet.unlock", "builder-sea") != nil && func() bool {
			code, out, _ := hg(t, h.sock, "", "data", "list", "closet.unlock", "--json")
			return code == 0 && !strings.Contains(out, "builder-sea")
		}()
	})
	if w := h.get("/pkg-files/closet/o/builder/sea/1.png"); w.Code != http.StatusNotFound {
		t.Fatalf("偽造 unlock 之後真圖要 404：%d", w.Code)
	}

	// 預設身分（沒有 EVERGENIE_AGENT）＝ owner(cli)：主人本人，通過。
	t.Setenv("EVERGENIE_AGENT", "")
	q = h.request(t, "builder", "night")
	if f := h.waitRequest(t, q); f["state"] != "done" || f["unlock"] != "builder-night" {
		t.Fatalf("主人的請求：%v", f)
	}
	if u := h.fields(t, "closet.unlock", "builder-night"); u == nil || u["cost"] != float64(1) || u["by"] != "closet" || u["request"] != q {
		t.Fatalf("unlock：%v", u)
	}
	if w := h.get("/pkg-files/closet/o/builder/night/1.png"); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), h.img["builder@night"]) {
		t.Fatalf("兌換後真圖要公開：%d", w.Code)
	}
	if w := h.get("/pkg-files/closet/s/builder/night/1.png"); w.Code != http.StatusNotFound {
		t.Fatalf("兌換後剪影要拿掉：%d", w.Code)
	}
	// 再送一次同樣的被拒；剩的點數不夠換第三套，被拒。
	q = h.request(t, "builder", "night")
	if f := h.waitRequest(t, q); f["state"] != "rejected" || !strings.Contains(f["reason"].(string), "已經擁有這套了") {
		t.Fatalf("重複請求：%v", f)
	}
	q = h.request(t, "builder", "sea")
	if f := h.waitRequest(t, q); f["state"] != "rejected" || !strings.Contains(f["reason"].(string), "點數不夠") {
		t.Fatalf("點數不夠：%v", f)
	}
	if w := h.get("/pkg-files/closet/o/builder/sea/1.png"); w.Code != http.StatusNotFound {
		t.Fatalf("sea 真圖仍要 404：%d", w.Code)
	}
	waitUntil(t, "habits 的錢包算進花掉的 1 點", 20*time.Second, func() bool {
		f := h.fields(t, "habits.wallet", "main")
		return f != nil && f["spent"] == float64(1) && f["points"] == float64(1)
	})
}

// TestAvatarEndpointFollowsRotation：輪播只勾 night → 頭像變成 night 的圖；有 night 的 mp4 才有影片，沒有就 404、roster 的 video_url 為空。
// Guards: REQ-113, REQ-125
func TestAvatarEndpointFollowsRotation(t *testing.T) {
	h := startClosetHome(t)
	h.outfit(t, h.sock, "builder-base", 0, "active")
	h.outfit(t, h.sock, "builder-night", 0, "active")
	hgOK(t, h.sock, "data", "add", "closet.rotation", "--id", "builder", "--json", `{"outfits":["night"]}`)

	waitUntil(t, "頭像變成 night 的圖", 20*time.Second, func() bool {
		w := h.get("/avatars/code.png")
		return w.Code == http.StatusOK && bytes.Equal(w.Body.Bytes(), h.img["builder@night"])
	})
	if w := h.get("/avatars/code.mp4"); w.Code != http.StatusNotFound {
		t.Fatalf("沒有 night 的 mp4 要 404：%d", w.Code)
	}
	video := func() string {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/web/get_roster", strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		h.web.ServeHTTP(w, r)
		var resp sdk.WebGetRosterResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		for _, a := range resp.Agents {
			if a.ID == "code" {
				return a.VideoURL
			}
		}
		t.Fatal("名冊沒有 code")
		return ""
	}
	if v := video(); v != "" {
		t.Fatalf("沒有 worn mp4 時 video_url 要空：%q", v)
	}
	// 放進 night 的動態：程式下一輪（vault 有變）就公開。
	mp4 := filepath.Join(h.dir, "personas", "avatars", "builder@night.mp4")
	if err := os.WriteFile(mp4, []byte("night-mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	hgOK(t, h.sock, "data", "set", "closet.outfit", "builder-night", "--json", `{"desc":"有動態了"}`)
	waitUntil(t, "worn 的 mp4 出現", 20*time.Second, func() bool {
		w := h.get("/avatars/code.mp4")
		return w.Code == http.StatusOK && w.Body.String() == "night-mp4"
	})
	if v := video(); v != "/avatars/code.mp4" {
		t.Fatalf("video_url：%q", v)
	}
	// 輪播改成只有 base（沒有 mp4）：圖換回 base、舊的 mp4 不能留。
	hgOK(t, h.sock, "data", "set", "closet.rotation", "builder", "--json", `{"outfits":["base"]}`)
	waitUntil(t, "頭像換回 base", 20*time.Second, func() bool {
		w := h.get("/avatars/code.png")
		return w.Code == http.StatusOK && bytes.Equal(w.Body.Bytes(), h.img["builder"])
	})
	if w := h.get("/avatars/code.mp4"); w.Code != http.StatusNotFound {
		t.Fatalf("換回 base 之後 mp4 要 404：%d", w.Code)
	}
}
