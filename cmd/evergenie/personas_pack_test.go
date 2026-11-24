package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
)

// packSource 是角色包來源的家（真的服務在跑）：helper、builder 都公開，各一套送的 base（cost 0）與一套要兌換的衣服，
// 還有隱藏版的資料夾（跟著資料夾走的紀錄 dress.variant 指的 avatars/hidden、wardrobe/helper/night）。
// helper 另有主人自己的兩套（source = mine：一套已採用、一套設計中，圖在 personas/mine/avatars/）與一套官方、還在設計中的（圖已經在官方位置 avatars/helper@next.png）：
// 都還沒升成官方，不能進官方角色包（PKG-68，作者 2026-11-14）。
func packSource(t *testing.T) (string, string) {
	t.Helper()
	home, sock := startFixtureAPIWith(t, func(dir string) {
		dressPackage(t, dir)
		cfg, err := os.ReadFile(filepath.Join(dir, "home.toml"))
		if err != nil {
			t.Fatal(err)
		}
		putFile(t, filepath.Join(dir, "home.toml"), string(cfg)+"\n[admin]\npublic_personas = [\"helper\", \"builder\"]\n")
		for _, f := range []string{"avatars/helper.png", "avatars/builder.png", "avatars/full/helper.png", "wardrobe/helper/sea.png", "wardrobe/builder/work.png",
			"avatars/hidden/helper.png", "wardrobe/helper/night/1.png", "wardrobe/builder/moonlit/1.png",
			"mine/avatars/helper@my-24.png", "mine/avatars/helper@my-38.png", "avatars/helper@next.png"} {
			putFile(t, filepath.Join(dir, "personas", filepath.FromSlash(f)), "假圖 "+f)
		}
	})
	for id, rec := range map[string]string{
		"helper-base":  `{"persona":"helper","outfit":"base","name":"基本","cost":0,"state":"active","source":"official"}`,
		"helper-sea":   `{"persona":"helper","outfit":"sea","name":"海邊","cost":3,"state":"active","source":"official"}`,
		"builder-base": `{"persona":"builder","outfit":"base","name":"基本","cost":0,"state":"active","source":"official"}`,
		"builder-work": `{"persona":"builder","outfit":"work","name":"工作服","cost":2,"state":"active","source":"official"}`,
		"helper-my-24": `{"persona":"helper","outfit":"my-24","name":"主人的私服","cost":0,"state":"active","source":"mine"}`,
		"helper-my-38": `{"persona":"helper","outfit":"my-38","name":"主人設計中的","cost":0,"state":"draft","source":"mine"}`,
		"helper-next":  `{"persona":"helper","outfit":"next","name":"官方設計中的","cost":0,"state":"draft","source":"official"}`,
	} {
		hgOK(t, sock, "data", "add", "dress.outfit", "--id", id, "--json", rec)
	}
	// 隱藏版：一個在 avatars/ 的子資料夾，一個在 wardrobe/<代號>/ 底下；moonlit 的名字裡沒有任何「成人」字眼。
	hgOK(t, sock, "data", "add", "dress.variant", "--id", "hidden", "--json", `{"name":"隱藏版","dir":"avatars/hidden","button":true}`)
	hgOK(t, sock, "data", "add", "dress.variant", "--id", "night", "--json", `{"name":"夜","dir":"wardrobe/helper/night","button":true}`)
	hgOK(t, sock, "data", "add", "dress.variant", "--id", "moonlit", "--json", `{"name":"月光","dir":"wardrobe/builder/moonlit","button":true}`)
	return home, sock
}

// packFiles 回角色包清單裡的邏輯路徑。
func packFiles(t *testing.T, out, version string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "m", version+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		Records []struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"records"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range m.Files {
		got = append(got, f.Path)
	}
	for _, r := range m.Records {
		got = append(got, r.Kind+"/"+r.ID)
	}
	slices.Sort(got)
	return got
}

// 角色包從來源的家到新的家（DEV-157，PRS-4、PRS-14、PRS-16、PRS-22、PRS-28）：
// 來源的家的服務在跑 → `personas pack build` 帶上衣服紀錄、隱藏版的資料夾（跟著資料夾走的紀錄指的）整個不收、印一行總結；
// 主人自己的與設計中的衣服（還沒升成官方）不帶、總結說幾筆沒帶（PKG-68、PRS-29）；
// 新的家 `personas pack sync` 交給它的服務做 → 衣服紀錄寫進去、圖拿得到、登記檔記的卡認得出是官方的；隱藏版的檔案與紀錄都沒進來。
// Guards: PRS-4, PRS-14, PRS-16, PRS-22, PRS-28
func TestPersonasPackFromSourceToNewHome(t *testing.T) {
	src, srcSock := packSource(t)
	out := filepath.Join(t.TempDir(), "pack")
	repo := t.TempDir()
	built := hgOK(t, srcSock, "personas", "pack", "build", "--home", src, "--out", out, "--version", "2026.11.14", "--set-pointer", repo, "--url-base", "file://"+out)
	for _, want := range []string{"角色包 2026.11.14：2 位角色、", "4 筆紀錄（3 筆還沒升成官方或還在設計中的衣服沒帶：helper-my-24、helper-my-38、helper-next）（它們的圖 1 個檔也沒收）",
		"沒收：avatars/helper@next.png：是沒帶的紀錄 dress.outfit/helper-next 的圖，不進官方角色包",
		"沒收：dress.outfit：3 筆還沒升成官方或還在設計中的衣服沒帶（套件宣告官方角色包只帶 source 是 official、state 是 active／retired 的）：helper-my-24、helper-my-38、helper-next",
		"排除的資料夾（跟著資料夾走的紀錄指的，例如隱藏版）：avatars/hidden、wardrobe/builder/moonlit、wardrobe/helper/night",
		"收進來的子資料夾：wardrobe/helper/（", "收進來的子資料夾：wardrobe/builder/（", "收進來的子資料夾：avatars/full/（"} {
		if !strings.Contains(built, want) {
			t.Fatalf("build 少了「%s」：\n%s", want, built)
		}
	}
	files := packFiles(t, out, "2026.11.14")
	for _, want := range []string{"cards/helper.md", "cards/builder.md", "avatars/helper.png", "avatars/full/helper.png", "wardrobe/helper/sea.png", "wardrobe/builder/work.png",
		"dress.outfit/helper-base", "dress.outfit/helper-sea", "dress.outfit/builder-base", "dress.outfit/builder-work"} {
		if !slices.Contains(files, want) {
			t.Errorf("包裡少了 %s：%v", want, files)
		}
	}
	for _, f := range files {
		if strings.Contains(f, "hidden") || strings.Contains(f, "night") || strings.Contains(f, "moonlit") || strings.HasPrefix(f, "dress.variant/") {
			t.Errorf("隱藏版不能進包：%s", f)
		}
	}
	// 還沒升成官方的衣服（主人自己的、設計中的）：紀錄、圖、連名稱都不能在包裡（PKG-68）。
	if n := len(files); n == 0 || strings.Contains(strings.Join(files, "\n"), "my-") || strings.Contains(strings.Join(files, "\n"), "next") {
		t.Errorf("還沒升成官方的衣服不能進包：%v", files)
	}
	for _, bad := range []string{"my-24", "my-38", "helper-next", "主人的私服", "主人設計中的", "官方設計中的"} {
		if strings.Contains(mustRead(t, filepath.Join(out, "m", "2026.11.14.json")), bad) {
			t.Errorf("包的清單裡不能有「%s」", bad)
		}
	}
	pointer := filepath.Join(repo, "packages", "official", "personas", "pack.toml")
	t.Setenv("EVERGENIE_PERSONAS_PACK_POINTER", pointer)

	user, userSock, board := startFixtureWebWith(t, func(dir string) { dressPackage(t, dir) })
	synced := hgOK(t, userSock, "personas", "pack", "sync", "--home", user, "--json")
	var res personapack.SyncResult
	if err := json.Unmarshal([]byte(synced), &res); err != nil {
		t.Fatalf("%v：%s", err, synced)
	}
	if res.Status != personapack.StatusSynced || res.Version != "2026.11.14" || len(res.RecordsAdded) != 4 || res.RecordsLater {
		t.Fatalf("交給服務同步：%+v", res)
	}
	list := hgOK(t, userSock, "data", "list", "dress.outfit", "--json")
	for _, id := range []string{"helper-base", "helper-sea", "builder-base", "builder-work"} {
		if !strings.Contains(list, `"`+id+`"`) {
			t.Errorf("新的家少了衣服紀錄 %s：%s", id, list)
		}
	}
	for _, bad := range []string{"helper-my-24", "helper-my-38", "helper-next"} {
		if strings.Contains(list, bad) {
			t.Errorf("還沒升成官方的衣服不能進新的家：%s", bad)
		}
	}
	if v := hgOK(t, userSock, "data", "list", "dress.variant", "--json"); strings.Contains(v, "hidden") || strings.Contains(v, "night") || strings.Contains(v, "moonlit") {
		t.Errorf("隱藏版的紀錄不能進新的家：%s", v)
	}
	for _, f := range []string{"avatars/hidden", "wardrobe/helper/night", "wardrobe/builder/moonlit", "avatars/helper@next.png", "mine/avatars/helper@my-24.png"} {
		if fileExists(filepath.Join(user, "personas", filepath.FromSlash(f))) {
			t.Errorf("隱藏版的資料夾不能進新的家：%s", f)
		}
	}
	rec := httptest.NewRecorder()
	board.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/avatars/persona/helper.png", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "假圖 avatars/helper.png" {
		t.Errorf("頭像要拿得到：%d %q", rec.Code, rec.Body.String())
	}
	// 登記檔用包裡的邏輯路徑（cards/<代號>.md）記；整理家（home tidy）靠它認得官方的卡（PRS-22）。
	cards, ok, err := personapack.RegisteredCards(user)
	if err != nil || !ok || !cards["helper"] || !cards["builder"] {
		t.Fatalf("登記過的官方卡：%v %v %v", cards, ok, err)
	}

	// 來源的家的衣櫃套件停用了：讀不到它的衣服目錄，也不知道隱藏版的資料夾在哪 → 帶紀錄的包不建；--no-records 衣服資料夾整個不收。
	hgOK(t, srcSock, "packages", "disable", "dress")
	code, _, errOut := hg(t, srcSock, "", "personas", "pack", "build", "--home", src, "--out", filepath.Join(t.TempDir(), "p"), "--version", "2026.11.15")
	if code == 0 || !strings.Contains(errOut, "dress（停用中；dress.outfit、dress.variant）") || !strings.Contains(errOut, "--no-records") {
		t.Fatalf("套件停用時不建包：%d %s", code, errOut)
	}
	out2 := filepath.Join(t.TempDir(), "p2")
	if b := hgOK(t, srcSock, "personas", "pack", "build", "--home", src, "--out", out2, "--version", "2026.11.15", "--no-records"); !strings.Contains(b, "wardrobe/：套件 dress 讀不到") {
		t.Errorf("要說衣服資料夾為什麼不收：\n%s", b)
	}
	files = packFiles(t, out2, "2026.11.15")
	if !slices.Contains(files, "avatars/helper.png") {
		t.Errorf("頂層頭像照收：%v", files)
	}
	for _, f := range files {
		if strings.HasPrefix(f, "wardrobe/") || strings.HasPrefix(f, "dress.") {
			t.Errorf("--no-records、套件停用：衣服資料夾與紀錄都不收：%s", f)
		}
	}
}

// 已經在用的家不會壞（DEV-157 第 7 件）：這一版沒有角色包（指標空）照舊什麼都不做；角色包來源的家不同步；
// 主人改過的衣服紀錄，下一版的包換了也不蓋；服務沒在跑時來源的家不加 --no-records 就不建包。
// Guards: PRS-4, PRS-5, PRS-15, PRS-28
func TestPersonasPackKeepsExistingHomes(t *testing.T) {
	src, srcSock := packSource(t)
	// 沒有角色包：這一版的指標（官方套件 personas 的 pack.toml）是空的。
	plain, plainSock := startFixtureAPIWith(t, nil)
	before := homeSnapshot(t, filepath.Join(plain, "personas"))
	if got := hgOK(t, plainSock, "personas", "pack", "sync", "--home", plain); !strings.Contains(got, personapack.NoPackMessage) {
		t.Fatalf("沒有角色包：%s", got)
	}
	if after := homeSnapshot(t, filepath.Join(plain, "personas")); after != before {
		t.Fatal("沒有角色包時什麼都不能動")
	}
	// 來源的家：不同步。
	if got := hgOK(t, srcSock, "personas", "pack", "sync", "--home", src); !strings.Contains(got, "這個家是角色包的來源") {
		t.Fatalf("來源的家：%s", got)
	}

	out, repo := filepath.Join(t.TempDir(), "pack"), t.TempDir()
	hgOK(t, srcSock, "personas", "pack", "build", "--home", src, "--out", out, "--version", "2026.11.14", "--set-pointer", repo, "--url-base", "file://"+out)
	t.Setenv("EVERGENIE_PERSONAS_PACK_POINTER", filepath.Join(repo, "packages", "official", "personas", "pack.toml"))
	user, userSock := startFixtureAPIWith(t, func(dir string) { dressPackage(t, dir) })
	hgOK(t, userSock, "personas", "pack", "sync", "--home", user)
	// 主人改了 helper-sea 的名字；下一版的包裡 helper-sea、helper-base 都換了。
	hgOK(t, userSock, "data", "set", "dress.outfit", "helper-sea", "--json", `{"name":"我的海邊"}`)
	hgOK(t, srcSock, "data", "set", "dress.outfit", "helper-sea", "--json", `{"name":"新海邊"}`)
	hgOK(t, srcSock, "data", "set", "dress.outfit", "helper-base", "--json", `{"name":"新基本"}`)
	hgOK(t, srcSock, "personas", "pack", "build", "--home", src, "--out", out, "--version", "2026.11.15", "--set-pointer", repo, "--url-base", "file://"+out)
	got := hgOK(t, userSock, "personas", "pack", "sync", "--home", user, "--json")
	var res personapack.SyncResult
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != personapack.StatusSynced || strings.Join(res.RecordsKept, ",") != "dress.outfit/helper-sea" || strings.Join(res.RecordsReplaced, ",") != "dress.outfit/helper-base" {
		t.Fatalf("改過的不蓋、沒改的換：%+v", res)
	}
	if one := hgOK(t, userSock, "data", "get", "dress.outfit", "helper-sea", "--json"); !strings.Contains(one, "我的海邊") {
		t.Fatalf("主人改的要留著：%s", one)
	}

	// 來源的家的服務沒在跑：不加 --no-records 不建包，什麼都不寫。
	offline := copyFixtureHome(t, false)
	putFile(t, filepath.Join(offline, "home.toml"), mustRead(t, filepath.Join(offline, "home.toml"))+"\n[admin]\npublic_personas = [\"helper\"]\n")
	dest := filepath.Join(t.TempDir(), "offline-pack")
	code, stdout, stderr := hg(t, userSock, "", "personas", "pack", "build", "--home", offline, "--out", dest, "--version", "2026.11.14")
	if code == 0 || !strings.Contains(stderr, "連不到這個家的 EverGenie 服務") || !strings.Contains(stderr, "--no-records") || fileExists(dest) {
		t.Fatalf("服務沒在跑不建包：%d\n%s\n%s", code, stdout, stderr)
	}
}

// homeSnapshot 回資料夾裡每個檔的路徑與內容（排序過），用來比有沒有變。
func homeSnapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		lines = append(lines, rel+"="+mustRead(t, p))
		return nil
	})
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
