package personapack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 角色包（REQ-283、REQ-131、REQ-135）：發行者的家與使用者的家都在 t.TempDir()，包放在 t.TempDir() 用 file://（或 httptest 的本機 http）。

const homeToml = `[user]
name = "測試"
timezone = "Asia/Taipei"
[slots]
default = "life"
[slots.life]
label = "生活"
remind = "08:30"
check = "18:00"
[accounts]
default = "main"
[accounts.main]
engine = "test-engine"
[web]
port = 18850
[sessions]
autostart = false
`

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// sourceHome 建一個角色包來源的家：公開 athena、venus；kaguya 不公開。裡面放了該收的和絕對不能收的。
func sourceHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	write(t, filepath.Join(home, "home.toml"), homeToml+"[admin]\npublic_personas = [\"athena\", \"venus\"]\n")
	pe := filepath.Join(home, "personas")
	av := filepath.Join(pe, "avatars")
	// 該收的
	write(t, filepath.Join(pe, "athena.md"), "---\nschema: 1\nname: Athena\n---\n智慧女神 v1\n")
	write(t, filepath.Join(pe, "venus.md"), "---\nschema: 1\nname: Venus\n---\n維納斯\n")
	write(t, filepath.Join(av, "athena.png"), "PNG-athena-base")
	write(t, filepath.Join(av, "athena@seaside.png"), "PNG-athena-seaside")
	write(t, filepath.Join(av, "athena@seaside.mp4"), "MP4-athena-seaside")
	write(t, filepath.Join(av, "athena~2.png"), "PNG-athena-2")
	write(t, filepath.Join(av, "full", "athena.png"), "PNG-athena-full")
	write(t, filepath.Join(av, "venus.png"), "PNG-venus")
	write(t, filepath.Join(pe, "wardrobe", "athena", "day.png"), "PNG-wardrobe-day")
	write(t, filepath.Join(pe, "wardrobe", "athena", "sub", "hat.png"), "PNG-wardrobe-hat")
	write(t, filepath.Join(pe, "catalog", "athena.md"), "# Athena 目錄\n公開版衣服\n")
	// 絕對不能收的
	write(t, filepath.Join(av, "athena@seaside.locked.png"), "ADULT-SECRET-locked")
	write(t, filepath.Join(av, "athena.live.json"), "ADULT-SECRET-live")
	write(t, filepath.Join(av, "athena.voice.mp4"), "ADULT-SECRET-voice")
	write(t, filepath.Join(av, "level", "hidden", "athena.png"), "ADULT-SECRET-level")
	write(t, filepath.Join(av, "old_school", "athena.png"), "ADULT-SECRET-old")
	write(t, filepath.Join(av, "src", "athena@moon.mp4"), "ADULT-SECRET-src")
	write(t, filepath.Join(pe, "wardrobe", "athena", "adult-night.png"), "ADULT-SECRET-adultname")
	write(t, filepath.Join(pe, "wardrobe", "athena", "level", "x.png"), "ADULT-SECRET-wlevel")
	write(t, filepath.Join(pe, "wardrobe", "athena.toml"), "prompt = \"ADULT-SECRET-prompt\"\n")
	write(t, filepath.Join(pe, "catalog", "venus.md"), "---\nlevel: adult\n---\nADULT-SECRET-catalog\n")
	write(t, filepath.Join(pe, "mine", "athena.md"), "ADULT-SECRET-mine")
	write(t, filepath.Join(av, "mine", "athena.png"), "ADULT-SECRET-minepng")
	write(t, filepath.Join(pe, "kaguya.md"), "NOT-PUBLIC-kaguya")
	write(t, filepath.Join(av, "kaguya.png"), "NOT-PUBLIC-kaguya-png")
	write(t, filepath.Join(pe, "wardrobe", "kaguya", "a.png"), "NOT-PUBLIC-kaguya-w")
	return home
}

func userHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	write(t, filepath.Join(home, "home.toml"), homeToml)
	return home
}

func buildPack(t *testing.T, home, version string) (*BuildResult, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "pack")
	r, err := Build(BuildOptions{Home: home, Out: out, Version: version, Store: newStore(nil)}) // 服務連得上、沒有任何套件資料
	if err != nil {
		t.Fatal(err)
	}
	return r, out
}

// pointer 寫一個指標檔，指到 out 這個包的 version。
func pointer(t *testing.T, r *BuildResult, base string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pack.toml")
	write(t, p, fmt.Sprintf("version = %q\nurl = %q\nsha256 = %q\n", r.Version, base+"/m/"+r.Version+".json", r.ManifestSHA256))
	return p
}

func paths(m *Manifest) []string {
	var out []string
	for _, f := range m.Files {
		out = append(out, f.Path)
	}
	sort.Strings(out)
	return out
}

func loadManifest(t *testing.T, p string) *Manifest {
	t.Helper()
	var m Manifest
	if err := json.Unmarshal([]byte(read(t, p)), &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

// TestPackBuildOnlyPublic：只收 public_personas 列的人，且只收卡、頭像（含全身照）、公開版衣櫃、目錄卡；別人的、使用者自己的（mine）都不進包；
// manifest 的 sha256 跟檔案一致，f/ 的檔名是內容的 sha256；不是發行者的家、沒寫 public_personas 都不能建。
// Guards: REQ-283, REQ-131, REQ-135
func TestPackBuildOnlyPublic(t *testing.T) {
	home := sourceHome(t)
	res, out := buildPack(t, home, "2026.10.29")
	m := loadManifest(t, res.ManifestPath)
	want := []string{
		"avatars/athena.png", "avatars/athena@seaside.mp4", "avatars/athena@seaside.png", "avatars/athena~2.png", "avatars/full/athena.png", "avatars/venus.png",
		"cards/athena.md", "cards/venus.md", "catalog/athena.md", "wardrobe/athena/day.png", "wardrobe/athena/sub/hat.png",
	}
	if got := paths(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("包裡的檔案：\n%v\n要是：\n%v", got, want)
	}
	if m.Kind != Kind || m.Schema != 1 || m.Version != "2026.10.29" || res.Files != len(want) {
		t.Fatalf("manifest：%+v", m)
	}
	if res.ManifestSHA256 != SumBytes([]byte(read(t, res.ManifestPath))) {
		t.Fatal("印出來的 manifest sha256 要等於檔案的")
	}
	for _, f := range m.Files {
		fp := filepath.Join(out, "f", f.SHA256+filepath.Ext(f.Path))
		sum, size, err := SumFile(fp)
		if err != nil || sum != f.SHA256 || size != f.Size {
			t.Fatalf("%s：f/ 裡的檔案要跟 manifest 一致：%v %s %d", f.Path, err, sum, size)
		}
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	// 沒寫 public_personas → INVALID。
	if _, err := Build(BuildOptions{Home: userHome(t), Out: filepath.Join(t.TempDir(), "p")}); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "public_personas") {
		t.Fatalf("沒寫 public_personas 要 INVALID 並說明：%v", err)
	}
	// 同一天再建一次 → 版號自動加 .2；指定已有的版號 → CONFLICT。
	fresh := filepath.Join(t.TempDir(), "fresh")
	if r2, err := Build(BuildOptions{Home: home, Out: fresh, Now: time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC), Store: newStore(nil)}); err != nil || r2.Version != "2026.10.29" {
		t.Fatalf("新資料夾裡第一份：%v %+v", err, r2)
	} else if r3, err := Build(BuildOptions{Home: home, Out: fresh, Now: time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC), Store: newStore(nil)}); err != nil || r3.Version != "2026.10.29.2" {
		t.Fatalf("同一天第二份要 .2：%v %+v", err, r3)
	}
	if _, err := Build(BuildOptions{Home: home, Out: out, Version: "2026.10.29", Store: newStore(nil)}); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("版號已存在要 CONFLICT：%v", err)
	}
	// --set-pointer：寫 pack.toml，不 commit。
	repo := t.TempDir()
	r4, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "p2"), Version: "2026.11.01", PointerRepo: repo, URLBase: "https://example.com/packs/", Store: newStore(nil)})
	if err != nil {
		t.Fatal(err)
	}
	ptr, err := ReadPointer(r4.PointerFile)
	if err != nil || ptr == nil || ptr.Version != "2026.11.01" || ptr.URL != "https://example.com/packs/m/2026.11.01.json" || ptr.SHA256 != r4.ManifestSHA256 {
		t.Fatalf("指標：%v %+v", err, ptr)
	}
	if !strings.HasSuffix(r4.PointerFile, filepath.Join("packages", "official", "personas", "pack.toml")) {
		t.Fatalf("指標位置：%s", r4.PointerFile)
	}
	if _, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "p3"), PointerRepo: repo}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("只給 --set-pointer 沒給 --url-base 要 INVALID：%v", err)
	}
}

// TestPackBuildNoAdult：隱藏版與非公開的檔案一個都不進包（路徑有 level/、old_school/、mine/、src/、*.locked.*、*.live.json、*.voice.*、名稱有 adult、
// 自己標隱藏版的卡、製作用的衣櫃 toml），而且內容也不會出現在 f/ 裡。
// Guards: REQ-283, REQ-135
func TestPackBuildNoAdult(t *testing.T) {
	home := sourceHome(t)
	res, out := buildPack(t, home, "2026.10.29")
	m := loadManifest(t, res.ManifestPath)
	for _, f := range m.Files {
		for _, bad := range []string{"level", "old_school", "mine", "src", ".locked.", ".live.json", ".voice.", "adult", "kaguya", ".toml"} {
			if strings.Contains(f.Path, bad) {
				t.Errorf("不該收：%s（%s）", f.Path, bad)
			}
		}
	}
	items, err := os.ReadDir(filepath.Join(out, "f"))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		body := read(t, filepath.Join(out, "f", it.Name()))
		if strings.Contains(body, "ADULT-SECRET") || strings.Contains(body, "NOT-PUBLIC") {
			t.Errorf("f/%s 裡有不能公開的內容：%q", it.Name(), body)
		}
	}
	if len(res.Skipped) < 6 {
		t.Errorf("要列出刻意不收的：%v", res.Skipped)
	}
	joined := strings.Join(res.Skipped, "\n")
	for _, want := range []string{"locked", "live.json", "voice", "level/", "catalog/venus.md", "wardrobe/athena.toml"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Skipped 少了 %s：\n%s", want, joined)
		}
	}
	// 規則本身：單獨驗幾個判斷。
	for rel, want := range map[string]bool{"avatars/a.png": false, "avatars/a.locked.png": true, "avatars/level/a.png": true, "wardrobe/a/NSFW.png": true, "cards/a.md": false, "mine/a.md": true, "mine/adult/a.png": true, "avatars/adult/a.png": true} {
		if got := Excluded(rel, nil) != ""; got != want {
			t.Errorf("Excluded(%s)=%v", rel, got)
		}
	}
	if Excluded("catalog/a.md", []byte("title: x\nadult: true\n")) == "" || Excluded("catalog/a.md", []byte("adult: false\n")) != "" {
		t.Error("檔頭的隱藏版標記要看 true／false")
	}
}

func syncOpts(home, ptr string) SyncOptions {
	return SyncOptions{Home: home, PointerPath: ptr, Now: time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)}
}

func mustSync(t *testing.T, o SyncOptions) *SyncResult {
	t.Helper()
	r, err := Sync(context.Background(), o)
	if err != nil {
		t.Fatalf("同步失敗：%v", err)
	}
	return r
}

// TestPackSyncInstallsAndRecords：使用者的家同步一份包：檔案放到 personas/ 對應位置、登記版號與每個檔的 sha256；再同步一次是「已是最新」；
// 發行者的家與沒有角色包的版本都跳過。
// Guards: REQ-283, REQ-131
func TestPackSyncInstallsAndRecords(t *testing.T) {
	pub := sourceHome(t)
	r1, out := buildPack(t, pub, "2026.10.29")
	ptr := pointer(t, r1, "file://"+out)
	user := userHome(t)
	res := mustSync(t, syncOpts(user, ptr))
	if res.Status != StatusSynced || res.Registered != "2026.10.29" || len(res.Installed) != r1.Files || res.Unchanged != 0 {
		t.Fatalf("第一次同步：%+v", res)
	}
	for rel, want := range map[string]string{
		"personas/athena.md":                   "智慧女神 v1",
		"personas/avatars/athena.png":          "PNG-athena-base",
		"personas/avatars/full/athena.png":     "PNG-athena-full",
		"personas/wardrobe/athena/sub/hat.png": "PNG-wardrobe-hat",
		"personas/catalog/athena.md":           "公開版衣服",
	} {
		if got := read(t, filepath.Join(user, rel)); !strings.Contains(got, want) {
			t.Errorf("%s：%q", rel, got)
		}
	}
	if exists(filepath.Join(user, "personas", "kaguya.md")) || exists(filepath.Join(user, "personas", "avatars", "athena@seaside.locked.png")) {
		t.Fatal("不該有非公開的檔案")
	}
	reg, err := LoadRegistry(user)
	if err != nil || reg.Version != "2026.10.29" || len(reg.Files) != r1.Files || reg.Files["cards/athena.md"] != SumBytes([]byte(read(t, filepath.Join(user, "personas", "athena.md")))) {
		t.Fatalf("登記：%v %+v", err, reg)
	}
	if res2 := mustSync(t, syncOpts(user, ptr)); res2.Status != StatusUpToDate || res2.Registered != "2026.10.29" {
		t.Fatalf("第二次要已是最新：%+v", res2)
	}
	// 暫存的 .hgpack 都清掉。
	_ = filepath.Walk(filepath.Join(user, "personas"), func(p string, _ os.FileInfo, _ error) error {
		if strings.Contains(p, ".hgpack") {
			t.Errorf("不該留暫存：%s", p)
		}
		return nil
	})
	// 角色包的來源（有寫 public_personas）跳過；沒有角色包（url 空、或沒有 pack.toml）也不是錯誤。
	if r := mustSync(t, syncOpts(pub, ptr)); r.Status != StatusSource {
		t.Fatalf("角色包的來源：%+v", r)
	}
	empty := filepath.Join(t.TempDir(), "pack.toml")
	write(t, empty, "version = \"\"\nurl = \"\"\nsha256 = \"\"\n")
	if r := mustSync(t, syncOpts(userHome(t), empty)); r.Status != StatusNoPack {
		t.Fatalf("url 空：%+v", r)
	}
	if r := mustSync(t, syncOpts(userHome(t), filepath.Join(t.TempDir(), "nope.toml"))); r.Status != StatusNoPack {
		t.Fatalf("沒有 pack.toml：%+v", r)
	}
}

// TestPackSyncOnlyChanged：新版的包只換有變的檔（sha256 跟登記一樣的跳過，連下載都不做）；manifest 不再列的檔不刪。
// Guards: REQ-283
func TestPackSyncOnlyChanged(t *testing.T) {
	pub := sourceHome(t)
	r1, out1 := buildPack(t, pub, "2026.10.29")
	user := userHome(t)
	mustSync(t, syncOpts(user, pointer(t, r1, "file://"+out1)))
	cacheBefore := cacheNames(t, user)

	write(t, filepath.Join(pub, "personas", "athena.md"), "---\nschema: 1\nname: Athena\n---\n智慧女神 v2\n")
	write(t, filepath.Join(pub, "personas", "avatars", "athena@spring.png"), "PNG-athena-spring")
	if err := os.Remove(filepath.Join(pub, "personas", "avatars", "venus.png")); err != nil {
		t.Fatal(err)
	}
	r2, out2 := buildPack(t, pub, "2026.10.30")
	res := mustSync(t, syncOpts(user, pointer(t, r2, "file://"+out2)))
	if res.Status != StatusSynced || strings.Join(res.Installed, ",") != "avatars/athena@spring.png,cards/athena.md" || res.Unchanged != r2.Files-2 {
		t.Fatalf("只該換 2 個：%+v", res)
	}
	if got := read(t, filepath.Join(user, "personas", "athena.md")); !strings.Contains(got, "v2") {
		t.Fatalf("卡片要是 v2：%q", got)
	}
	if !exists(filepath.Join(user, "personas", "avatars", "venus.png")) {
		t.Fatal("manifest 不再列的檔不刪")
	}
	after := cacheNames(t, user)
	if len(after)-len(cacheBefore) != 2 {
		t.Fatalf("只該多下載 2 個：%d → %d", len(cacheBefore), len(after))
	}
	if reg, _ := LoadRegistry(user); reg.Version != "2026.10.30" || reg.Files["avatars/venus.png"] == "" {
		t.Fatalf("登記：%+v", reg)
	}
}

func cacheNames(t *testing.T, home string) []string {
	t.Helper()
	items, _ := os.ReadDir(cacheDir(home))
	var out []string
	for _, it := range items {
		out = append(out, it.Name())
	}
	return out
}

// TestPackSyncKeepsUserEdits：你改過的檔（跟登記的 sha256 不一樣）不蓋，列在「你改過的，沒換」，其他檔照換；--force 才蓋，蓋之前備份。
// 原本就有、不是包裡來的檔案（沒有登記）也當作你的。
// Guards: REQ-283
func TestPackSyncKeepsUserEdits(t *testing.T) {
	pub := sourceHome(t)
	r1, out1 := buildPack(t, pub, "2026.10.29")
	user := userHome(t)
	write(t, filepath.Join(user, "personas", "venus.md"), "我自己寫的 venus\n") // 沒登記過、內容不同
	res := mustSync(t, syncOpts(user, pointer(t, r1, "file://"+out1)))
	if strings.Join(res.UserEdited, ",") != "cards/venus.md" || read(t, filepath.Join(user, "personas", "venus.md")) != "我自己寫的 venus\n" {
		t.Fatalf("原本就有的檔不蓋：%+v", res)
	}
	write(t, filepath.Join(user, "personas", "athena.md"), "我改過的 athena\n")
	write(t, filepath.Join(pub, "personas", "athena.md"), "---\nname: Athena\n---\n官方 v2\n")
	write(t, filepath.Join(pub, "personas", "avatars", "athena.png"), "PNG-athena-base-v2")
	r2, out2 := buildPack(t, pub, "2026.10.30")
	ptr2 := pointer(t, r2, "file://"+out2)
	res = mustSync(t, syncOpts(user, ptr2))
	if strings.Join(res.UserEdited, ",") != "cards/athena.md,cards/venus.md" || strings.Join(res.Installed, ",") != "avatars/athena.png" {
		t.Fatalf("改過的不蓋、其他照換：%+v", res)
	}
	if read(t, filepath.Join(user, "personas", "athena.md")) != "我改過的 athena\n" || read(t, filepath.Join(user, "personas", "avatars", "athena.png")) != "PNG-athena-base-v2" {
		t.Fatal("檔案內容不對")
	}
	// 登記的版號已經是 v2，同一版再加 --force 也要能蓋（DEV-35：以前會回「已是最新」，改過的檔永遠蓋不掉）。
	o := syncOpts(user, ptr2)
	o.Force = true
	res = mustSync(t, o)
	if !strings.Contains(strings.Join(res.Installed, ","), "cards/athena.md") || !strings.Contains(read(t, filepath.Join(user, "personas", "athena.md")), "官方 v2") {
		t.Fatalf("--force 要蓋：%+v", res)
	}
	if res.Backup == "" || read(t, filepath.Join(user, res.Backup, "personas", "athena.md")) != "我改過的 athena\n" {
		t.Fatalf("蓋之前要備份你的版本：%+v", res)
	}
	if read(t, filepath.Join(user, res.Backup, "personas", "venus.md")) != "我自己寫的 venus\n" {
		t.Fatal("venus 也是你的，被蓋了就要有備份")
	}
	_ = r1
}

// TestPackSyncOfflineKeepsCurrent：下載不到（沒網路）時什麼都不換、登記不變、不退版，寫 pending.json，doctor 提示有一句白話；
// 之後補上就成功並清掉 pending。
// Guards: REQ-283, REQ-131
func TestPackSyncOfflineKeepsCurrent(t *testing.T) {
	pub := sourceHome(t)
	r1, out1 := buildPack(t, pub, "2026.10.29")
	user := userHome(t)
	mustSync(t, syncOpts(user, pointer(t, r1, "file://"+out1)))
	before := read(t, filepath.Join(user, "personas", "athena.md"))
	regBefore := read(t, registryPath(user))

	write(t, filepath.Join(pub, "personas", "athena.md"), "---\nname: Athena\n---\nv2\n")
	r2, out2 := buildPack(t, pub, "2026.10.30")
	gone := pointer(t, r2, "file://"+filepath.Join(t.TempDir(), "不存在"))
	res, err := Sync(context.Background(), syncOpts(user, gone))
	if err == nil || res.Status != StatusFailed || res.Reason == "" || res.RecordsLater { // 沒網路不是「紀錄還沒寫」，不快速再試
		t.Fatalf("要失敗：%v %+v", err, res)
	}
	if read(t, filepath.Join(user, "personas", "athena.md")) != before || read(t, registryPath(user)) != regBefore {
		t.Fatal("失敗要什麼都不換、登記不變")
	}
	p := ReadPending(user)
	if p == nil || p.Version != "2026.10.30" || p.Reason == "" || p.At == "" {
		t.Fatalf("pending：%+v", p)
	}
	if note := PendingNote(user); !strings.Contains(note, "角色包還沒補上") || !strings.Contains(note, "有網路時會自動再試") {
		t.Fatalf("doctor 的話：%q", note)
	}
	if st, err := Status(user, gone); err != nil || st.Registered != "2026.10.29" || st.Latest != "2026.10.30" || st.Pending == nil || st.Last == nil || st.Last.Status != StatusFailed {
		t.Fatalf("現況：%v %+v", err, st)
	}
	// 補上（指標改回能連的）→ 成功並清掉 pending。
	if res, err = Sync(context.Background(), syncOpts(user, pointer(t, r2, "file://"+out2))); err != nil || res.Status != StatusSynced {
		t.Fatalf("補上之後要成功：%v %+v", err, res)
	}
	if ReadPending(user) != nil {
		t.Fatal("成功後 pending 要清掉")
	}
	if note := PendingNote(user); note != "" {
		t.Fatalf("成功後 doctor 沒有話：%q", note)
	}
	if !strings.Contains(read(t, filepath.Join(user, "personas", "athena.md")), "v2") {
		t.Fatal("補上之後要換成 v2")
	}
}

// TestPackSyncNoDowngrade：指標的版號比登記的舊就不動（不退版），連網路都不碰。
// Guards: REQ-283, REQ-131
func TestPackSyncNoDowngrade(t *testing.T) {
	pub := sourceHome(t)
	r1, out1 := buildPack(t, pub, "2026.10.29")
	user := userHome(t)
	mustSync(t, syncOpts(user, pointer(t, r1, "file://"+out1)))
	reg, _ := LoadRegistry(user)
	reg.Version = "2026.11.15"
	if err := saveRegistry(user, reg); err != nil {
		t.Fatal(err)
	}
	// 舊指標的網址其實連不上：不退版就不該去抓。
	old := pointer(t, &BuildResult{Version: "2026.10.29", ManifestSHA256: r1.ManifestSHA256}, "file:///nowhere")
	writePending(user, Pending{Version: "2026.10.01", Reason: "舊的待補", At: "x"})
	res := mustSync(t, syncOpts(user, old))
	if res.Status != StatusNoDowngrade || res.Registered != "2026.11.15" || !strings.Contains(res.Message(), "不退版") {
		t.Fatalf("不退版：%+v", res)
	}
	if reg2, _ := LoadRegistry(user); reg2.Version != "2026.11.15" {
		t.Fatal("登記不能被改")
	}
	if ReadPending(user) != nil {
		t.Fatal("比登記還舊的待補沒意義，要清掉")
	}
	if c, ok := CompareVersions("2026.10.29.2", "2026.10.29"); !ok || c != 1 {
		t.Fatalf("版號比較：%d %v", c, ok)
	}
	if c, _ := CompareVersions("2026.10.29.10", "2026.10.29.9"); c != 1 {
		t.Fatalf("版號比較要用數字：%d", c)
	}
}

// TestPackSyncBadHashChangesNothing：下載的內容跟 manifest 寫的 sha256 不一樣（或 manifest 跟指標記的不一樣、路徑想跳出 personas/）→ 什麼都不換。
// Guards: REQ-283
func TestPackSyncBadHashChangesNothing(t *testing.T) {
	pub := sourceHome(t)
	r1, out1 := buildPack(t, pub, "2026.10.29")
	m := loadManifest(t, r1.ManifestPath)
	var victim File
	for _, f := range m.Files {
		if f.Path == "avatars/athena~2.png" {
			victim = f
		}
	}
	write(t, filepath.Join(out1, "f", victim.SHA256+".png"), "被竄改的內容")
	user := userHome(t)
	res, err := Sync(context.Background(), syncOpts(user, pointer(t, r1, "file://"+out1)))
	if err == nil || res.Status != StatusFailed || !strings.Contains(res.Reason, "sha256") {
		t.Fatalf("要因為 sha256 對不上失敗：%v %+v", err, res)
	}
	if exists(filepath.Join(user, "personas")) {
		items, _ := os.ReadDir(filepath.Join(user, "personas"))
		if len(items) != 0 {
			t.Fatalf("一個檔都不能換：%v", items)
		}
	}
	if reg, _ := LoadRegistry(user); reg.Version != "" {
		t.Fatalf("登記不能動：%+v", reg)
	}
	// manifest 被換掉：跟指標記的 sha256 對不上。
	pub2, out2 := buildPack(t, pub, "2026.10.30")
	ptr := pointer(t, &BuildResult{Version: pub2.Version, ManifestSHA256: strings.Repeat("0", 64)}, "file://"+out2)
	if res, err := Sync(context.Background(), syncOpts(userHome(t), ptr)); err == nil || !strings.Contains(res.Reason, "sha256") {
		t.Fatalf("manifest 被換掉要失敗：%v %+v", err, res)
	}
	// 路徑想跳出 personas/ 的 manifest 在 Validate 就被擋。
	bad := Manifest{Kind: Kind, Schema: 1, Version: "2026.10.31", Files: []File{{Path: "cards/../../../etc/x.md", SHA256: strings.Repeat("a", 64)}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("路徑跳出去要擋")
	}
	for _, p := range []string{"/etc/passwd", "cards/.hidden.md", "avatars/", "other/x.png", "wardrobe/x.png", "cards/Upper.md", "cards/a/b.md"} {
		if _, ok := Target(p); ok {
			t.Errorf("Target(%q) 不該通過", p)
		}
	}
}

// TestPackSyncHTTP：從本機的 http 下載（httptest）；非本機的 http:// 不收。
// Guards: REQ-283
func TestPackSyncHTTP(t *testing.T) {
	pub := sourceHome(t)
	r1, out1 := buildPack(t, pub, "2026.10.29")
	var (
		mu   sync.Mutex
		hits []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		http.FileServer(http.Dir(out1)).ServeHTTP(w, r)
	}))
	defer srv.Close()
	user := userHome(t)
	res := mustSync(t, syncOpts(user, pointer(t, r1, srv.URL)))
	if res.Status != StatusSynced || len(res.Installed) != r1.Files {
		t.Fatalf("http 同步：%+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits[0] != "/m/2026.10.29.json" || len(hits) != 1+r1.Files {
		t.Fatalf("請求：%v", hits)
	}
	// 非 https 又不是本機：擋。
	res, err := Sync(context.Background(), syncOpts(userHome(t), pointer(t, r1, "http://example.com/packs")))
	if err == nil || !strings.Contains(res.Reason, "https://") {
		t.Fatalf("非本機 http 要擋：%v %+v", err, res)
	}
	// 伺服器回 404：失敗不換。
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	if res, err := Sync(context.Background(), syncOpts(userHome(t), pointer(t, r1, missing.URL))); err == nil || !strings.Contains(res.Reason, "404") {
		t.Fatalf("404 要失敗：%v %+v", err, res)
	}
}

// TestPackSyncRefusesRedirectAwayFromHTTPS：https 的角色包被轉址到 http（連本機也一樣）不跟、什麼都不換（PLT-25）；
// http 那台一個請求都沒收到。
func TestPackSyncRefusesRedirectAwayFromHTTPS(t *testing.T) {
	pub := sourceHome(t)
	r1, out1 := buildPack(t, pub, "2026.10.29")
	var (
		mu   sync.Mutex
		hits []string
	)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		http.FileServer(http.Dir(out1)).ServeHTTP(w, r)
	}))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/m/") { // 清單照給，檔案轉到 http
			http.FileServer(http.Dir(out1)).ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer tls.Close()
	user := userHome(t)
	o := syncOpts(user, pointer(t, r1, tls.URL))
	o.HTTP = tls.Client()
	res, err := Sync(context.Background(), o)
	if err == nil || !strings.Contains(res.Reason, "不安全") || len(res.Installed) != 0 {
		t.Fatalf("被轉到 http 要擋：%v %+v", err, res)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hits) != 0 {
		t.Fatalf("http 那台不能收到任何請求：%v", hits)
	}
	// 對照：同一台 https 直接給檔案就同步得到（這個測試的環境沒問題）
	ok := httptest.NewTLSServer(http.FileServer(http.Dir(out1)))
	defer ok.Close()
	o2 := syncOpts(userHome(t), pointer(t, r1, ok.URL))
	o2.HTTP = ok.Client()
	if res := mustSync(t, o2); res.Status != StatusSynced {
		t.Fatalf("https 照常同步：%+v", res)
	}
}

// TestPackSyncReportsOnlyNewCards：NewCards 只列這次換進來、家裡本來沒有的官方角色卡（DEV-115 新角色打招呼）。
// 全新安裝的第一次同步（登記是空的）不算；本來就有、只是更新內容的不算；登記過但被刪掉、這次補回來的不算；
// 頭像、衣櫃這些不是卡的檔不算；我的卡（personas/mine/）同步不碰，也不會出現。
func TestPackSyncReportsOnlyNewCards(t *testing.T) {
	pub := sourceHome(t)
	r1, out1 := buildPack(t, pub, "2026.10.29")
	user := userHome(t)
	write(t, filepath.Join(user, "personas", "mine", "mycat.md"), "---\nname: 我的貓\n---\n喵\n")
	if res := mustSync(t, syncOpts(user, pointer(t, r1, "file://"+out1))); len(res.NewCards) != 0 {
		t.Fatalf("第一次安裝不打招呼：%+v", res.NewCards)
	}

	// 第二版：新角色 luna、athena 只更新內容；使用者自己刪掉了 venus（登記過）。
	write(t, filepath.Join(pub, "home.toml"), homeToml+"[admin]\npublic_personas = [\"athena\", \"venus\", \"luna\"]\n")
	write(t, filepath.Join(pub, "personas", "luna.md"), "---\nschema: 1\nname: Luna\n---\n月亮\n")
	write(t, filepath.Join(pub, "personas", "avatars", "luna.png"), "PNG-luna")
	write(t, filepath.Join(pub, "personas", "athena.md"), "---\nschema: 1\nname: Athena\n---\n智慧女神 v2\n")
	if err := os.Remove(filepath.Join(user, "personas", "venus.md")); err != nil {
		t.Fatal(err)
	}
	r2, out2 := buildPack(t, pub, "2026.10.30")
	res := mustSync(t, syncOpts(user, pointer(t, r2, "file://"+out2)))
	if strings.Join(res.NewCards, ",") != "luna" {
		t.Fatalf("只有 luna 是新角色：%+v（installed %v）", res.NewCards, res.Installed)
	}
	installed := strings.Join(res.Installed, ",")
	if !strings.Contains(installed, "cards/venus.md") || !strings.Contains(installed, "cards/athena.md") || !strings.Contains(installed, "avatars/luna.png") {
		t.Fatalf("venus 補回、athena 更新、luna 頭像都要換進來：%v", res.Installed)
	}
	// 同一版再同步：已是最新，沒有新角色。
	if again := mustSync(t, syncOpts(user, pointer(t, r2, "file://"+out2))); again.Status != StatusUpToDate || len(again.NewCards) != 0 {
		t.Fatalf("再同步一次不該再有新角色：%+v", again)
	}

	// 已經在用、只是從沒同步過角色包的家（沒有登記檔，但 personas/ 已經有官方卡）：不算全新安裝，
	// 家裡真的沒有的角色照樣算新的；原本就有的不算。
	old := userHome(t)
	write(t, filepath.Join(old, "personas", "athena.md"), "---\nschema: 1\nname: Athena\n---\n搬家帶來的\n")
	first := mustSync(t, syncOpts(old, pointer(t, r2, "file://"+out2)))
	if strings.Join(first.NewCards, ",") != "luna,venus" {
		t.Fatalf("舊家第一次同步：家裡沒有的 luna、venus 是新角色，athena 不是：%+v", first.NewCards)
	}
}
