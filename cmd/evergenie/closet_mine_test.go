package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// TestClosetMineOnFixtureHome：真 serve＋fixture 家＋三個真行程（closet、closet-mine、habits）：
// closet-mine 預設啟用（DEV-112）→ 沒設定紀錄就 running；把 PNG 放進 runtime/share/，character 請求 done，
// closet.person 出現、公開路由 200 而且位元組等於上傳檔；outfit 請求（要點數）只有剪影；full 請求只對擁有的公開；
// photo 請求給官方衣服被拒。DEV-160 S3：新上傳在 personas/mine/photos/<照片編號>.<ext>；沒寫 draft 直接生效；
// 刪單張後公開資料夾沒有、拿回後回來；要用點數換開／關對應 cost 與剪影；刪一整套可以拿回。
// Guards: REQ-126
func TestClosetMineOnFixtureHome(t *testing.T) {
	t.Setenv("EVERGENIE_PACKAGE_BIN_DIR", buildPackageBinaries(t, "habits", "closet", "closet-mine"))
	img := map[string][]byte{"fox": testPNG(11), "beach": testPNG(12), "full": testPNG(13), "full2": testPNG(14), "photo": testPNG(15)}
	var home string
	_, sock, web := startFixtureWebWith(t, func(d string) {
		home = d
		for _, id := range []string{"habits", "closet", "closet-mine"} {
			copyOfficialPackage(t, d, id)
		}
	})
	t.Setenv("EVERGENIE_AGENT", "code")
	pkgs := sdk.NewPackageClient(sdk.Dial(sock))
	state := func(id string) sdk.Package {
		r, err := pkgs.Get(context.Background(), &sdk.PackageGetRequest{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		return *r.Package
	}
	waitUntil(t, "closet running", 30*time.Second, func() bool { return state("closet").State == sdk.PackageStateRunning })

	// 預設啟用（DEV-112，作者 2026-11-03：上傳是展示間的基本用途，不再是選用）：沒有設定紀錄就跑起來，不寫 packages.toml。
	waitUntil(t, "closet-mine running", 30*time.Second, func() bool {
		p := state("closet-mine")
		if p.State == sdk.PackageStateFailed || p.State == sdk.PackageStateRejected {
			t.Fatalf("closet-mine 壞了：%s %s", p.State, p.Error)
		}
		return p.State == sdk.PackageStateRunning
	})
	if p := state("closet-mine"); !p.Enabled || p.Optional {
		t.Fatalf("closet-mine 要預設啟用、不是選用：%+v", p)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "settings", "packages.toml")); strings.Contains(string(data), "closet-mine") {
		t.Fatalf("預設啟用不寫設定：%s", data)
	}
	if kinds := hgOK(t, sock, "data", "kinds"); !strings.Contains(kinds, "closet-mine.request") || !strings.Contains(kinds, "U-<數字>") {
		t.Fatalf("data kinds：%s", kinds)
	}

	// get 回公開路由的狀態碼與內容；ok 只看是不是 200。
	get := func(path string) (int, []byte) {
		w := httptest.NewRecorder()
		web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Code, w.Body.Bytes()
	}
	served := func(path string) bool { code, _ := get(path); return code == http.StatusOK }
	upload := func(name string, data []byte) {
		p := filepath.Join(home, "runtime", "share", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	send := func(fields map[string]any) map[string]any {
		fields["state"] = "pending"
		body, _ := json.Marshal(fields)
		id := strings.TrimSpace(hgOK(t, sock, "data", "add", "closet-mine.request", "--json", string(body)))
		var f map[string]any
		waitUntil(t, "請求 "+id+" 被處理", 30*time.Second, func() bool {
			f = fieldsOf(t, sock, "closet-mine.request", id)
			return f != nil && f["state"] != "pending"
		})
		f["_id"] = id
		return f
	}

	// 1. 新角色。
	upload("fox.png", img["fox"])
	r := send(map[string]any{"type": "character", "upload": "/share/fox.png", "name": "小狐", "desc": "一隻狐狸", "persona": "myfox"})
	if r["state"] != "done" || r["result"] != "myfox-base" {
		t.Fatalf("character：%v", r)
	}
	waitUntil(t, "closet.person/myfox 出現", 30*time.Second, func() bool {
		p := fieldsOf(t, sock, "closet.person", "myfox")
		return p != nil && len(p["outfits"].([]any)) == 1
	})
	person := fieldsOf(t, sock, "closet.person", "myfox")
	if person["name"] != "小狐" {
		t.Fatalf("角色名要取自角色卡：%v", person)
	}
	base := person["outfits"].([]any)[0].(map[string]any)
	if base["source"] != "mine" || base["owned"] != true {
		t.Fatalf("base 衣服：%v", base)
	}
	card, _ := os.ReadFile(filepath.Join(home, "personas", "mine", "myfox.md"))
	if !strings.HasPrefix(string(card), "---\nschema: 1\nname: \"小狐\"\n---\n一隻狐狸") {
		t.Fatalf("角色卡：%q", card)
	}
	waitUntil(t, "公開路由有 base 的圖", 30*time.Second, func() bool { return served("/pkg-files/closet/o/myfox/base/1.png") })
	if _, b := get("/pkg-files/closet/o/myfox/base/1.png"); !bytes.Equal(b, img["fox"]) {
		t.Fatal("公開的圖要等於上傳檔")
	}
	// 角色本身的頭像在角色卡的慣例路徑 avatars/<代號>.png（複製角色進套件只認它），基本款第一張的照片紀錄指著它（DEV-160 S3）。
	if b, err := os.ReadFile(filepath.Join(home, "personas", "mine", "avatars", "myfox.png")); err != nil || !bytes.Equal(b, img["fox"]) {
		t.Fatalf("角色的頭像要在 avatars/myfox.png：%v", err)
	}
	if p := fieldsOf(t, sock, "closet.photo", "myfox-base--u"+strings.TrimPrefix(r["_id"].(string), "U-")); p == nil || p["file"] != "avatars/myfox.png" {
		t.Fatalf("基本款第一張的照片紀錄：%v", p)
	}

	// 2. 替角色加要點數的衣服：只有剪影。
	upload("beach.png", img["beach"])
	r = send(map[string]any{"type": "outfit", "upload": "beach.png", "persona": "myfox", "name": "海邊", "free": false})
	if r["state"] != "done" {
		t.Fatalf("outfit：%v", r)
	}
	outfitID := strings.TrimPrefix(r["result"].(string), "myfox-")
	photoOf := func(record string, req map[string]any) string {
		return record + "--u" + strings.TrimPrefix(req["_id"].(string), "U-")
	}
	beachPhoto := photoOf("myfox-"+outfitID, r)
	waitUntil(t, "海邊出現在 person", 30*time.Second, func() bool {
		p := fieldsOf(t, sock, "closet.person", "myfox")
		return p != nil && len(p["outfits"].([]any)) == 2
	})
	waitUntil(t, "海邊有剪影", 30*time.Second, func() bool { return served("/pkg-files/closet/s/myfox/" + outfitID + "/1.png") })
	if served("/pkg-files/closet/o/myfox/" + outfitID + "/1.png") {
		t.Fatal("沒兌換的真圖要 404")
	}
	if _, sil := get("/pkg-files/closet/s/myfox/" + outfitID + "/1.png"); bytes.Equal(sil, img["beach"]) {
		t.Fatal("剪影不能等於真圖")
	}

	// 3. 全身照：擁有的有、沒擁有的沒有。
	upload("full.png", img["full"])
	if r = send(map[string]any{"type": "full", "upload": "full.png", "persona": "myfox", "outfit": "base"}); r["state"] != "done" {
		t.Fatalf("full base：%v", r)
	}
	upload("full2.png", img["full2"])
	if r = send(map[string]any{"type": "full", "upload": "full2.png", "persona": "myfox", "outfit": outfitID}); r["state"] != "done" {
		t.Fatalf("full 海邊：%v", r)
	}
	waitUntil(t, "base 的全身照公開", 30*time.Second, func() bool { return served("/pkg-files/closet/f/myfox/base/1.png") })
	if _, b := get("/pkg-files/closet/f/myfox/base/1.png"); !bytes.Equal(b, img["full"]) {
		t.Fatal("全身照要等於上傳檔")
	}
	if served("/pkg-files/closet/f/myfox/" + outfitID + "/1.png") {
		t.Fatal("沒擁有的全身照要 404")
	}
	// 對帳先放檔案、再寫 closet.person：公開路由有圖不代表紀錄已經更新，要等紀錄也對上（負載高時曾時好時壞）。
	waitUntil(t, "full 欄位只有擁有的才有", 30*time.Second, func() bool {
		p := fieldsOf(t, sock, "closet.person", "myfox")
		if p == nil {
			return false
		}
		for _, o := range p["outfits"].([]any) {
			m := o.(map[string]any)
			if (m["owned"] == true) != (m["full"] != "") {
				return false
			}
		}
		return true
	})

	// 4. 輪播照片：再加一張。
	upload("photo.png", img["photo"])
	if r = send(map[string]any{"type": "photo", "upload": "photo.png", "persona": "myfox", "outfit": "base"}); r["state"] != "done" {
		t.Fatalf("photo：%v", r)
	}
	waitUntil(t, "base 第 2 張公開", 30*time.Second, func() bool { return served("/pkg-files/closet/o/myfox/base/2.png") })

	// 4b. 新上傳（新角色的頭像以外）一律放 personas/mine/photos/<照片編號>.<ext>（照片編號 <衣服編號>--u<請求編號>），不再寫 avatars/<代號>@…。
	photoID := photoOf("myfox-base", r)
	minePhoto := func(id string) string { return filepath.Join(home, "personas", "mine", "photos", id+".png") }
	if b, err := os.ReadFile(minePhoto(photoID)); err != nil || !bytes.Equal(b, img["photo"]) {
		t.Fatalf("新上傳的檔要在 photos/%s.png：%v", photoID, err)
	}
	if old, _ := filepath.Glob(filepath.Join(home, "personas", "mine", "avatars", "myfox*")); len(old) != 1 {
		t.Fatalf("personas/mine/avatars/ 只有角色的頭像：%v", old)
	}
	if p := fieldsOf(t, sock, "closet.photo", photoID); p == nil || p["file"] != "photos/"+photoID+".png" || p["source"] != "mine" || p["hidden"] != false || p["adopted"] != true {
		t.Fatalf("照片紀錄：%v", p)
	}
	// 沒寫 draft＝直接生效：衣服 active、照片已採用。
	if o := fieldsOf(t, sock, "closet.outfit", "myfox-"+outfitID); o == nil || o["state"] != "active" || o["cost"] != float64(1) {
		t.Fatalf("直接生效的衣服：%v", o)
	}
	if p := fieldsOf(t, sock, "closet.photo", beachPhoto); p == nil || p["adopted"] != true || p["file"] != "photos/"+beachPhoto+".png" {
		t.Fatalf("直接生效的照片：%v", p)
	}

	// 4c. 刪單張：紀錄封存、檔改名成備份，公開資料夾沒有第 2 張；拿回：檔改回原名、第 2 張回來。
	if r = send(map[string]any{"type": "remove_photo", "persona": "myfox", "photo_id": photoID}); r["state"] != "done" {
		t.Fatalf("remove_photo：%v", r)
	}
	waitUntil(t, "刪了的第 2 張不公開", 30*time.Second, func() bool { return !served("/pkg-files/closet/o/myfox/base/2.png") })
	if baks, _ := filepath.Glob(minePhoto(photoID) + ".bak-*-delete-u*"); len(baks) != 1 || fileExists(minePhoto(photoID)) {
		t.Fatalf("刪了檔要改名成備份：%v", baks)
	}
	if rec := recordOf(t, sock, "closet.photo", photoID); rec == nil || rec["archived"] != true {
		t.Fatalf("刪了紀錄要封存：%v", rec)
	}
	if r = send(map[string]any{"type": "restore_photo", "persona": "myfox", "photo_id": photoID}); r["state"] != "done" {
		t.Fatalf("restore_photo：%v", r)
	}
	waitUntil(t, "拿回的第 2 張公開", 30*time.Second, func() bool {
		code, b := get("/pkg-files/closet/o/myfox/base/2.png")
		return code == http.StatusOK && bytes.Equal(b, img["photo"])
	})
	if !fileExists(minePhoto(photoID)) {
		t.Fatal("拿回時檔改回原名")
	}

	// 4d. 要用點數換：關（cost = 0）→ 擁有、真圖公開；開（cost = 1）→ 回到剪影。
	if r = send(map[string]any{"type": "set_cost", "persona": "myfox", "outfit": outfitID, "cost": 0}); r["state"] != "done" {
		t.Fatalf("set_cost 0：%v", r)
	}
	waitUntil(t, "免費後真圖公開", 30*time.Second, func() bool { return served("/pkg-files/closet/o/myfox/" + outfitID + "/1.png") })
	if r = send(map[string]any{"type": "set_cost", "persona": "myfox", "outfit": outfitID, "cost": 1}); r["state"] != "done" {
		t.Fatalf("set_cost 1：%v", r)
	}
	waitUntil(t, "要點數後回到剪影", 30*time.Second, func() bool {
		return !served("/pkg-files/closet/o/myfox/"+outfitID+"/1.png") && served("/pkg-files/closet/s/myfox/"+outfitID+"/1.png")
	})

	// 4e. 刪一整套（衣服與照片都封存）、拿回（照片跟著回來）；基本款不能刪。
	if r = send(map[string]any{"type": "remove_outfit", "persona": "myfox", "outfit": "base"}); r["state"] != "rejected" {
		t.Fatalf("基本款不能刪：%v", r)
	}
	if r = send(map[string]any{"type": "remove_outfit", "persona": "myfox", "outfit": outfitID}); r["state"] != "done" {
		t.Fatalf("remove_outfit：%v", r)
	}
	waitUntil(t, "刪了的衣服不在 person", 30*time.Second, func() bool {
		p := fieldsOf(t, sock, "closet.person", "myfox")
		return p != nil && len(p["outfits"].([]any)) == 1 && !served("/pkg-files/closet/s/myfox/"+outfitID+"/1.png")
	})
	if rec := recordOf(t, sock, "closet.photo", beachPhoto); rec == nil || rec["archived"] != true {
		t.Fatalf("刪一整套時照片也封存：%v", rec)
	}
	if r = send(map[string]any{"type": "restore_outfit", "persona": "myfox", "outfit": outfitID}); r["state"] != "done" {
		t.Fatalf("restore_outfit：%v", r)
	}
	waitUntil(t, "拿回的衣服回來", 30*time.Second, func() bool {
		p := fieldsOf(t, sock, "closet.person", "myfox")
		return p != nil && len(p["outfits"].([]any)) == 2 && served("/pkg-files/closet/s/myfox/"+outfitID+"/1.png")
	})
	if rec := recordOf(t, sock, "closet.photo", beachPhoto); rec == nil || rec["archived"] != false {
		t.Fatalf("拿回時照片也回來：%v", rec)
	}

	// 5. 官方衣服不能改。
	hgOK(t, sock, "data", "add", "closet.outfit", "--id", "builder-base", "--json", `{"persona":"builder","outfit":"base","name":"BASE","cost":0,"state":"active","source":"official"}`)
	r = send(map[string]any{"type": "photo", "upload": "photo.png", "persona": "builder", "outfit": "base"})
	if r["state"] != "rejected" || !strings.Contains(r["reason"].(string), "官方") {
		t.Fatalf("官方衣服：%v", r)
	}
	// 非圖片檔（假副檔名）被拒。
	upload("fake.png", []byte("just text pretending to be a png"))
	r = send(map[string]any{"type": "character", "upload": "fake.png", "name": "假的", "persona": "myfake"})
	if r["state"] != "rejected" || !strings.Contains(r["reason"].(string), "PNG") {
		t.Fatalf("假圖片：%v", r)
	}
	if _, err := os.Stat(filepath.Join(home, "personas", "mine", "myfake.md")); err == nil {
		t.Fatal("被拒的請求不能留下角色卡")
	}

	// 關掉：行程收掉、寫明確的 false（使用者自己關的，之後照他的意思不會自己打開）、資料種類看不到請求寫不進去。
	hgOK(t, sock, "packages", "disable", "closet-mine")
	if p := state("closet-mine"); p.Enabled || p.State != sdk.PackageStateStopped {
		t.Fatalf("disable：%+v", p)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "settings", "packages.toml")); !strings.Contains(string(data), "closet-mine = false") {
		t.Fatalf("關掉要寫明確的 closet-mine = false：%s", data)
	}
	if code, _, _ := hg(t, sock, "", "data", "add", "closet-mine.request", "--json", `{"type":"character","upload":"x.png","name":"x","state":"pending"}`); code == 0 {
		t.Fatal("停用時不該能寫請求")
	}
	// 再開：回到預設（刪掉紀錄），行程起來。
	out := hgOK(t, sock, "packages", "enable", "closet-mine")
	if !strings.Contains(out, "已啟用") {
		t.Fatalf("enable 輸出：%s", out)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "settings", "packages.toml")); strings.Contains(string(data), "closet-mine") {
		t.Fatalf("開回來是預設，不留紀錄：%s", data)
	}
	waitUntil(t, "closet-mine running again", 30*time.Second, func() bool { return state("closet-mine").State == sdk.PackageStateRunning })

	// 這裡做的角色複製進套件要帶著頭像（DEV-160 S3 審查：核心 copyAvatars 只認 personas/mine/avatars/<代號>.<ext>）。主人本人才能做。
	t.Setenv("EVERGENIE_AGENT", "")
	hgOK(t, sock, "packages", "new", "garden")
	hgOK(t, sock, "personas", "copy", "myfox", "--to-package", "garden", "--slug", "fox")
	copied, _ := filepath.Glob(filepath.Join(home, "packages", "mine", "garden", "personas", "avatars", "fox.*"))
	if len(copied) != 1 || filepath.Base(copied[0]) != "fox.png" {
		t.Fatalf("複製進套件的角色要有頭像 fox.png：%v", copied)
	}
	if b, _ := os.ReadFile(copied[0]); !bytes.Equal(b, img["fox"]) {
		t.Fatal("套件裡的頭像要等於當初上傳的圖")
	}
}

// recordOf 讀一筆資料（含 archived）；不存在回 nil。
func recordOf(t *testing.T, sock, kind, id string) map[string]any {
	t.Helper()
	code, out, _ := hg(t, sock, "", "data", "get", kind, id, "--json")
	if code != 0 {
		return nil
	}
	return jsonLine(t, out)
}

// fieldsOf 讀一筆資料的欄位；不存在回 nil。
func fieldsOf(t *testing.T, sock, kind, id string) map[string]any {
	t.Helper()
	code, out, _ := hg(t, sock, "", "data", "get", kind, id, "--json")
	if code != 0 {
		return nil
	}
	return jsonLine(t, out)["fields"].(map[string]any)
}
