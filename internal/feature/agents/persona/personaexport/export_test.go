package personaexport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// memStore 是測試用的套件資料：照 Store 介面記在記憶體，寫入次數算得出來。
type memStore struct {
	kinds []KindInfo
	recs  map[string]sdk.Record // <種類>/<編號>
	puts  int
	fail  string // Put 這一筆時失敗
}

func (m *memStore) Kinds(context.Context) ([]KindInfo, error) { return m.kinds, nil }
func (m *memStore) List(_ context.Context, kind string) ([]sdk.Record, error) {
	var out []sdk.Record
	for _, r := range m.recs {
		if r.Kind == kind && !r.Archived {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memStore) Get(_ context.Context, kind, id string) (*sdk.Record, error) {
	r, ok := m.recs[kind+"/"+id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}
func (m *memStore) Put(_ context.Context, rec sdk.Record, rev string) error {
	key := rec.Kind + "/" + rec.ID
	if key == m.fail {
		return fmt.Errorf("寫不進去")
	}
	old, ok := m.recs[key]
	if (ok && old.Revision != rev) || (!ok && rev != "") {
		return fmt.Errorf("revision 不符")
	}
	m.puts++
	fields := map[string]any{"schema": float64(1), "id": rec.ID, "by": "owner(cli)", "created_at": "2026-11-04T10:00:00+08:00"}
	for k, v := range rec.Fields {
		fields[k] = v
	}
	rec.Fields, rec.Revision = fields, fmt.Sprintf("r%d", m.puts)
	m.recs[key] = rec
	return nil
}

func (m *memStore) put(t *testing.T, kind, id string, fields map[string]any) {
	t.Helper()
	// 走 JSON 一趟：數字變 float64，跟經 API 讀到的一樣。
	data, _ := json.Marshal(fields)
	var f map[string]any
	_ = json.Unmarshal(data, &f)
	if err := m.Put(context.Background(), sdk.Record{Kind: kind, ID: id, Fields: f}, m.recs[kind+"/"+id].Revision); err != nil {
		t.Fatal(err)
	}
}

// closetKinds 照官方衣櫃的宣告（outfit 跟著角色、variant 跟著資料夾、unlock 不跟）；outfit 也照樣宣告 pack_only（PKG-68），
// 那只管官方角色包：這個檔的測試都帶著它跑，主人點名的匯出與匯入不受影響（TestExportIgnoresPackOnly）。
func closetKinds() []KindInfo {
	str := func(n string, req bool) sdk.DataFieldDecl {
		return sdk.DataFieldDecl{Name: n, Type: "string", Required: req}
	}
	return []KindInfo{
		{Kind: "closet.outfit", Decl: sdk.DataKindDecl{Kind: "outfit", IDs: "given", Export: sdk.DataExportByPersona, PersonaField: "persona", Fields: []sdk.DataFieldDecl{
			str("persona", true), str("outfit", true), str("name", true),
			{Name: "cost", Type: "int", Required: true},
			{Name: "state", Type: "enum", Enum: []string{"active", "draft", "retired"}, Required: true},
			{Name: "source", Type: "enum", Enum: []string{"official", "mine"}, Required: true},
		}, PackOnly: map[string][]string{"source": {"official"}, "state": {"active", "retired"}}, PackOnlyNote: "還沒升成官方或還在設計中的衣服"}},
		{Kind: "closet.variant", Decl: sdk.DataKindDecl{Kind: "variant", IDs: "given", Export: sdk.DataExportByFolder, FolderField: "dir", Fields: []sdk.DataFieldDecl{
			str("name", true), str("dir", true), {Name: "button", Type: "bool"}, {Name: "on", Type: "bool"}, {Name: "windows", Type: "list"},
		}, ExportOmit: []string{"on"}}},
		{Kind: "closet.unlock", Decl: sdk.DataKindDecl{Kind: "unlock", IDs: "given", Fields: []sdk.DataFieldDecl{str("persona", true), str("outfit", true)}}},
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("讀 %s：%v", p, err)
	}
	return string(b)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// homeA：兩位要匯出的角色（官方位置的 luna、我的 myhelper）、一位不匯出的 athena；
// 衣服目錄三筆、隱藏版兩個（一個資料夾有 luna 也有 athena 的圖，一個只有 athena 的）、一筆兌換紀錄（closet.unlock）。
func homeA(t *testing.T) (string, *memStore) {
	t.Helper()
	h := t.TempDir()
	p := func(rel string) string { return filepath.Join(h, "personas", filepath.FromSlash(rel)) }
	writeFile(t, p("luna.md"), "---\nschema: 1\nname: 露娜\n---\n個性\n")
	writeFile(t, p("athena.md"), "---\nschema: 1\nname: 雅典娜\n---\n")
	writeFile(t, p("mine/myhelper.md"), "---\nschema: 1\nname: \"小幫手\"\nstatus: design\n---\n")
	for _, f := range []string{"avatars/luna.png", "avatars/luna@sea.png", "avatars/luna@sea~2.webp", "avatars/luna.mp4", "avatars/full/luna@sea.png",
		"avatars/athena.png", "avatars/full/athena.png", "avatars/luna.locked.png", "avatars/lunaxx.png",
		"mine/avatars/luna@my-3.png", "mine/avatars/myhelper.png", "mine/avatars/full/myhelper.png",
		"wardrobe/luna/look.md", "wardrobe/luna/src/prompt.txt", "wardrobe/athena/look.md", "catalog/luna.md", "catalog/athena.md",
		"avatars/hidden/luna@sea.png", "avatars/hidden/full/luna.png", "avatars/hidden/athena.png", "avatars/night/athena.png"} {
		writeFile(t, p(f), "內容 "+f)
	}
	writeFile(t, p("wardrobe/luna.toml"), "prompt = \"…\"\n")
	s := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	outfit := func(persona, outfit string, cost int, source string) map[string]any {
		return map[string]any{"persona": persona, "outfit": outfit, "name": outfit, "cost": cost, "state": "active", "source": source}
	}
	s.put(t, "closet.outfit", "luna-base", outfit("luna", "base", 0, "official"))
	s.put(t, "closet.outfit", "luna-sea", outfit("luna", "sea", 1, "official"))
	s.put(t, "closet.outfit", "luna-my-3", outfit("luna", "my-3", 1, "mine"))
	s.put(t, "closet.outfit", "myhelper-base", outfit("myhelper", "base", 0, "mine"))
	s.put(t, "closet.outfit", "athena-base", outfit("athena", "base", 0, "official"))
	s.put(t, "closet.variant", "hidden", map[string]any{"name": "隱藏版", "dir": "avatars/hidden", "button": true, "on": true, "windows": []any{"22:00-02:00"}})
	s.put(t, "closet.variant", "night", map[string]any{"name": "夜", "dir": "avatars/night"})
	s.put(t, "closet.unlock", "luna-sea", map[string]any{"persona": "luna", "outfit": "sea"})
	return h, s
}

func exportTo(t *testing.T, home string, s Store, personas ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := Export(context.Background(), ExportOptions{Home: home, Personas: personas, Store: s}, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func importBytes(home string, s Store, data []byte, dry, force bool) (*ImportResult, error) {
	return Import(context.Background(), ImportOptions{Home: home, Store: s, DryRun: dry, Force: force, Now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}, bytes.NewReader(data), int64(len(data)))
}

func zipNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

// 匯出 A 家的兩位角色、匯入 B 家：卡、圖、全身照、衣櫃資料夾、目錄卡、她們的衣服目錄、有她圖的隱藏版與那個資料夾裡她的圖都到 B；
// 別的角色的圖與紀錄、製作用的檔、兌換紀錄（closet.unlock）都不跟（PRS-8、PRS-9、D-63）。
func TestExportImportRoundTrip(t *testing.T) {
	a, sa := homeA(t)
	data := exportTo(t, a, sa, "luna", "myhelper")
	names := strings.Join(zipNames(t, data), "\n")
	for _, bad := range []string{"athena", "src/", ".locked.", "wardrobe/luna.toml", "lunaxx"} {
		if strings.Contains(names, bad) {
			t.Errorf("匯出檔不該有「%s」：\n%s", bad, names)
		}
	}

	b := t.TempDir()
	sb := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	res, err := importBytes(b, sb, data, false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"luna.md", "mine/myhelper.md", "avatars/luna.png", "avatars/luna@sea.png", "avatars/luna@sea~2.webp", "avatars/luna.mp4",
		"avatars/full/luna@sea.png", "mine/avatars/luna@my-3.png", "mine/avatars/myhelper.png", "mine/avatars/full/myhelper.png",
		"wardrobe/luna/look.md", "catalog/luna.md", "avatars/hidden/luna@sea.png", "avatars/hidden/full/luna.png"}
	for _, f := range want {
		if got := readFile(t, filepath.Join(b, "personas", filepath.FromSlash(f))); got != readFile(t, filepath.Join(a, "personas", filepath.FromSlash(f))) {
			t.Errorf("%s 內容不對：%q", f, got)
		}
	}
	for _, f := range []string{"athena.md", "avatars/athena.png", "avatars/full/athena.png", "avatars/hidden/athena.png", "avatars/night/athena.png",
		"wardrobe/athena/look.md", "catalog/athena.md", "avatars/luna.locked.png", "wardrobe/luna/src/prompt.txt", "wardrobe/luna.toml", "avatars/lunaxx.png"} {
		if fileExists(filepath.Join(b, "personas", filepath.FromSlash(f))) {
			t.Errorf("B 不該有 %s", f)
		}
	}
	if len(res.Added) != len(want) || res.Unchanged != 0 || len(res.Kept) != 0 {
		t.Errorf("檔案：%+v", res)
	}
	gotRecs := append([]string(nil), res.RecordsAdded...)
	sort.Strings(gotRecs)
	if strings.Join(gotRecs, ",") != "closet.outfit/luna-base,closet.outfit/luna-my-3,closet.outfit/luna-sea,closet.outfit/myhelper-base,closet.variant/hidden" {
		t.Fatalf("紀錄：%v", gotRecs)
	}
	if r := sb.recs["closet.outfit/luna-sea"]; r.Fields["cost"] != float64(1) || r.Fields["source"] != "official" || r.Fields["by"] != "owner(cli)" {
		t.Errorf("衣服目錄的欄位：%+v", r.Fields)
	}
	if _, ok := sb.recs["closet.unlock/luna-sea"]; ok {
		t.Error("兌換紀錄（closet.unlock）不跟著走")
	}
	if !fileExists(filepath.Join(b, "settings", "persona-import.toml")) || fileExists(filepath.Join(b, "runtime", "persona-import", "staging", "20261104-120000")) {
		t.Error("要寫登記檔、暫存資料夾要清掉")
	}
	if !strings.Contains(res.Message(), "新增 14 個檔") || !strings.Contains(res.Message(), "紀錄新增 5 筆") {
		t.Errorf("摘要：%s", res.Message())
	}

	// 再匯入一次：全部一樣，什麼都不寫。
	puts := sb.puts
	res, err = importBytes(b, sb, data, false, false)
	if err != nil || len(res.Added)+len(res.Replaced)+len(res.Kept) != 0 || res.Unchanged != len(want) || res.RecordsUnchanged != 5 || sb.puts != puts {
		t.Fatalf("第二次：%+v %v", res, err)
	}
}

// B 本機改過的檔與紀錄不蓋（列在「你改過的，沒換」）；上次匯入之後沒改過的照新的換；--force 才蓋，蓋之前備份。
func TestImportKeepsLocalEditsUnlessForced(t *testing.T) {
	a, sa := homeA(t)
	b := t.TempDir()
	sb := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	if _, err := importBytes(b, sb, exportTo(t, a, sa, "luna"), false, false); err != nil {
		t.Fatal(err)
	}
	// A 改了卡與一張圖、一筆衣服；B 自己改了另一張圖與另一筆衣服。
	writeFile(t, filepath.Join(a, "personas", "luna.md"), "---\nschema: 1\nname: 露娜\n---\n新版\n")
	writeFile(t, filepath.Join(a, "personas", "avatars", "luna.png"), "A 的新圖")
	writeFile(t, filepath.Join(a, "personas", "avatars", "luna@sea.png"), "A 的新海邊")
	writeFile(t, filepath.Join(b, "personas", "avatars", "luna@sea.png"), "B 自己換的海邊")
	sa.put(t, "closet.outfit", "luna-base", map[string]any{"persona": "luna", "outfit": "base", "name": "新名字", "cost": 0, "state": "active", "source": "official"})
	sa.put(t, "closet.outfit", "luna-sea", map[string]any{"persona": "luna", "outfit": "sea", "name": "A 改", "cost": 2, "state": "active", "source": "official"})
	sb.put(t, "closet.outfit", "luna-sea", map[string]any{"persona": "luna", "outfit": "sea", "name": "B 改", "cost": 1, "state": "active", "source": "official"})
	data := exportTo(t, a, sa, "luna")

	res, err := importBytes(b, sb, data, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Kept, ",") != "personas/avatars/luna@sea.png" || strings.Join(res.RecordsKept, ",") != "closet.outfit/luna-sea" {
		t.Fatalf("你改過的：%v %v", res.Kept, res.RecordsKept)
	}
	if readFile(t, filepath.Join(b, "personas", "avatars", "luna@sea.png")) != "B 自己換的海邊" || sb.recs["closet.outfit/luna-sea"].Fields["name"] != "B 改" {
		t.Fatal("改過的被蓋了")
	}
	if readFile(t, filepath.Join(b, "personas", "luna.md")) != readFile(t, filepath.Join(a, "personas", "luna.md")) || readFile(t, filepath.Join(b, "personas", "avatars", "luna.png")) != "A 的新圖" {
		t.Fatal("上次匯入、沒改過的要換成新的")
	}
	if sb.recs["closet.outfit/luna-base"].Fields["name"] != "新名字" || res.Backup != "" {
		t.Fatalf("沒改過的紀錄要換、沒蓋東西就不備份：%+v %q", sb.recs["closet.outfit/luna-base"].Fields, res.Backup)
	}
	if !strings.Contains(res.Message(), "--force") {
		t.Errorf("摘要要提 --force：%s", res.Message())
	}

	res, err = importBytes(b, sb, data, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(b, "personas", "avatars", "luna@sea.png")) != "A 的新海邊" || sb.recs["closet.outfit/luna-sea"].Fields["name"] != "A 改" {
		t.Fatal("--force 要蓋")
	}
	if res.Backup == "" || readFile(t, filepath.Join(b, filepath.FromSlash(res.Backup), "personas", "avatars", "luna@sea.png")) != "B 自己換的海邊" {
		t.Fatalf("--force 要先備份檔案：%q", res.Backup)
	}
	var old sdk.Record
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(b, filepath.FromSlash(res.Backup), "records", "closet.outfit", "luna-sea.json"))), &old); err != nil || old.Fields["name"] != "B 改" {
		t.Fatalf("--force 要先備份紀錄：%+v %v", old, err)
	}
}

// sha256 對不上（或檔案被改、少檔、多檔、路徑不合規定、夾帶別的角色）整個檔不收，B 什麼都沒寫。
func TestImportRejectsTamperedFile(t *testing.T) {
	a, sa := homeA(t)
	good := exportTo(t, a, sa, "luna")
	rewrite := func(edit func(m *Manifest, files map[string][]byte)) []byte {
		zr, err := zip.NewReader(bytes.NewReader(good), int64(len(good)))
		if err != nil {
			t.Fatal(err)
		}
		var man Manifest
		files := map[string][]byte{}
		for _, f := range zr.File {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			if f.Name == manifestName {
				_ = json.Unmarshal(b, &man)
			} else {
				files[f.Name] = b
			}
		}
		edit(&man, files)
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for name, b := range files {
			w, _ := zw.Create(name)
			_, _ = w.Write(b)
		}
		mb, _ := json.Marshal(man)
		w, _ := zw.Create(manifestName)
		_, _ = w.Write(mb)
		_ = zw.Close()
		return buf.Bytes()
	}
	cases := map[string][]byte{
		"內容被改": rewrite(func(_ *Manifest, f map[string][]byte) { f["files/avatars/luna.png"] = []byte("換掉的") }),
		"少一個檔": rewrite(func(_ *Manifest, f map[string][]byte) { delete(f, "files/catalog/luna.md") }),
		"多一個檔": rewrite(func(_ *Manifest, f map[string][]byte) { f["files/avatars/luna@x.png"] = []byte("x") }),
		"跑出資料夾": rewrite(func(m *Manifest, f map[string][]byte) {
			m.Files = append(m.Files, m.Files[0])
			m.Files[len(m.Files)-1].Path = "extra/../../home.toml"
		}),
		"全家共用設定": rewrite(func(m *Manifest, f map[string][]byte) {
			m.Files[0].Path = "extra/_style.md"
		}),
		"夾帶別的角色": rewrite(func(m *Manifest, f map[string][]byte) {
			for i := range m.Files {
				if m.Files[i].Path == "avatars/luna.png" {
					m.Files[i].Path = "avatars/athena.png"
					f["files/avatars/athena.png"] = f["files/avatars/luna.png"]
					delete(f, "files/avatars/luna.png")
				}
			}
		}),
		"紀錄不是她的": rewrite(func(m *Manifest, _ map[string][]byte) {
			m.Records[0].Fields["persona"] = "athena"
		}),
		"隱藏版不是她的資料夾": rewrite(func(m *Manifest, _ map[string][]byte) {
			for i := range m.Records {
				if m.Records[i].Kind == "closet.variant" {
					m.Records[i].Fields["dir"] = "avatars/night"
				}
			}
		}),
		"隱藏版資料夾是全家設定": rewrite(func(m *Manifest, _ map[string][]byte) {
			for i := range m.Records {
				if m.Records[i].Kind == "closet.variant" {
					m.Records[i].Fields["dir"] = "_shared"
				}
			}
		}),
		"大小跟 manifest 不一樣": rewrite(func(m *Manifest, f map[string][]byte) {
			f["files/avatars/luna.png"] = append(f["files/avatars/luna.png"], 'x')
		}),
		"大小寫不同的同一個位置": rewrite(func(m *Manifest, f map[string][]byte) {
			c := m.Files[0] // avatars/full/luna@sea.png → 再加一個 avatars/full/luna@sea.PNG
			c.Path = strings.TrimSuffix(c.Path, ".png") + ".PNG"
			m.Files = append(m.Files, c)
			f["files/"+c.Path] = f["files/"+m.Files[0].Path]
		}),
		"一位角色兩張卡": rewrite(func(m *Manifest, f map[string][]byte) {
			for _, x := range m.Files {
				if x.Path == "cards/luna.md" {
					x.Path = "extra/mine/luna.md"
					m.Files = append(m.Files, x)
					f["files/extra/mine/luna.md"] = f["files/cards/luna.md"]
				}
			}
		}),
		"不是匯出檔": []byte("hello"),
	}
	for name, data := range cases {
		b := t.TempDir()
		sb := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
		if _, err := importBytes(b, sb, data, false, false); err == nil || !strings.Contains(err.Error(), "什麼都沒寫") {
			t.Errorf("%s：要整包不收，實際 %v", name, err)
		}
		if fileExists(filepath.Join(b, "personas")) || len(sb.recs) != 0 || fileExists(filepath.Join(b, "settings", "persona-import.toml")) {
			t.Errorf("%s：B 不該有任何東西", name)
		}
	}
}

// 沒裝衣櫃（或衣櫃停用）的家：圖照樣匯入，紀錄略過並列出原因；--dry-run 驗得一樣完整但什麼都不寫。
func TestImportWithoutClosetAndDryRun(t *testing.T) {
	a, sa := homeA(t)
	data := exportTo(t, a, sa, "luna")

	b := t.TempDir()
	res, err := importBytes(b, &memStore{recs: map[string]sdk.Record{}}, data, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RecordsSkipped) != 4 || !strings.Contains(res.RecordsSkipped[0], "closet 套件沒裝或停用") || len(res.RecordsAdded) != 0 {
		t.Fatalf("略過：%v", res.RecordsSkipped)
	}
	if !fileExists(filepath.Join(b, "personas", "luna.md")) || !strings.Contains(res.Message(), "略過 4 筆紀錄") {
		t.Fatalf("檔案要照樣匯入：%s", res.Message())
	}

	// 這個家的衣櫃沒說 outfit 能跟著角色匯入（舊版）：也略過，說清楚原因。
	old := closetKinds()
	old[0].Decl.Export = ""
	res, err = importBytes(t.TempDir(), &memStore{kinds: old, recs: map[string]sdk.Record{}}, data, false, false)
	if err != nil || len(res.RecordsAdded) != 1 || !strings.Contains(strings.Join(res.RecordsSkipped, "\n"), "沒說這種資料能跟著角色匯入") {
		t.Fatalf("舊版衣櫃：%+v %v", res, err)
	}

	dry := t.TempDir()
	sd := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	res, err = importBytes(dry, sd, data, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || len(res.Added) == 0 || len(res.RecordsAdded) != 4 || !strings.Contains(res.Message(), "預覽") {
		t.Fatalf("預覽：%+v", res)
	}
	entries, _ := os.ReadDir(dry)
	if len(entries) != 0 || sd.puts != 0 {
		t.Fatalf("--dry-run 不能寫任何東西：%v、寫了 %d 筆", entries, sd.puts)
	}
	// 預覽也驗 sha256。
	bad := bytes.Replace(data, []byte("內容 avatars/luna.png"), []byte("內容 avatars/luna.pnG"), 1)
	if _, err := importBytes(dry, sd, bad, true, false); err == nil {
		t.Fatal("預覽也要驗出內容被改")
	}
}

// 匯出的參數：沒給角色、套件帶來的角色、找不到卡都說清楚；只帶點名的角色。
func TestExportArgs(t *testing.T) {
	a, sa := homeA(t)
	for _, c := range []struct {
		personas []string
		want     string
	}{
		{nil, "--persona"},
		{[]string{"garden.fairy"}, "跟著套件走"},
		{[]string{"Bad"}, "格式不對"},
		{[]string{"nobody"}, "找不到角色"},
	} {
		_, err := Export(context.Background(), ExportOptions{Home: a, Personas: c.personas, Store: sa}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v：要「%s」，實際 %v", c.personas, c.want, err)
		}
	}
	out := filepath.Join(t.TempDir(), "luna")
	res, path, err := ExportToFile(context.Background(), ExportOptions{Home: a, Personas: []string{"luna", "luna"}, Store: sa}, out)
	if err != nil || path != out+Ext || strings.Join(res.Personas, ",") != "luna" || res.Records != 4 {
		t.Fatalf("%+v %s %v", res, path, err)
	}
	if _, _, err := ExportToFile(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"}, Store: sa}, out); err == nil || !strings.Contains(err.Error(), "已經有") {
		t.Fatalf("已經有這個檔不蓋：%v", err)
	}
}

// 同一個代號不能一邊在官方位置、一邊在「我的」（角色庫同代號官方優先，會悄悄換掉這個家原本那位）：整個檔不收，請先改代號。
func TestImportRefusesSlugInOtherLocation(t *testing.T) {
	a, sa := homeA(t)
	for _, c := range []struct{ persona, local string }{{"luna", "mine/luna.md"}, {"myhelper", "myhelper.md"}} {
		b := t.TempDir()
		writeFile(t, filepath.Join(b, "personas", filepath.FromSlash(c.local)), "這個家自己的")
		sb := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
		_, err := importBytes(b, sb, exportTo(t, a, sa, c.persona), false, true)
		if err == nil || !strings.Contains(err.Error(), "同代號") || len(sb.recs) != 0 || fileExists(filepath.Join(b, "personas", "avatars")) {
			t.Fatalf("%s：要整個不收，實際 %v", c.persona, err)
		}
	}
}

// 家裡 personas/ 底下的資料夾是符號連結時不往裡面寫（不會寫到家外面）。
func TestImportRefusesSymlinkedFolder(t *testing.T) {
	a, sa := homeA(t)
	b, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(b, "personas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(b, "personas", "avatars")); err != nil {
		t.Fatal(err)
	}
	_, err := importBytes(b, &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}, exportTo(t, a, sa, "luna"), false, true)
	if err == nil || !strings.Contains(err.Error(), "符號連結") {
		t.Fatalf("要擋：%v", err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("寫到家外面了：%v", entries)
	}
}

// 這個家封存過的編號不重用（--force 也不寫）；紀錄寫到一半失敗時照實說已經寫了哪些，登記照存。
func TestImportArchivedAndPartialRecordFailure(t *testing.T) {
	a, sa := homeA(t)
	data := exportTo(t, a, sa, "luna")

	b := t.TempDir()
	sb := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	sb.put(t, "closet.outfit", "luna-base", map[string]any{"persona": "luna", "outfit": "base", "name": "舊", "cost": 0, "state": "active", "source": "official"})
	r := sb.recs["closet.outfit/luna-base"]
	r.Archived = true
	sb.recs["closet.outfit/luna-base"] = r
	res, err := importBytes(b, sb, data, false, true)
	if err != nil || !strings.Contains(strings.Join(res.RecordsSkipped, "\n"), "closet.outfit/luna-base：這個家已經封存了這一筆") || sb.recs["closet.outfit/luna-base"].Fields["name"] != "舊" {
		t.Fatalf("封存的不重用：%+v %v", res, err)
	}

	c := t.TempDir()
	sc := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}, fail: "closet.outfit/luna-sea"}
	_, err = importBytes(c, sc, data, false, false)
	if err == nil || !strings.Contains(err.Error(), "紀錄 closet.outfit/luna-sea 寫不進去") || !strings.Contains(err.Error(), "已經寫進去的紀錄：closet.outfit/luna-base") {
		t.Fatalf("寫到一半失敗要照實說：%v", err)
	}
	reg := readFile(t, filepath.Join(c, "settings", "persona-import.toml"))
	if !fileExists(filepath.Join(c, "personas", "luna.md")) || !strings.Contains(reg, "closet.outfit/luna-base") || strings.Contains(reg, "closet.outfit/luna-sea") {
		t.Fatalf("檔案已換進來、登記只記寫成功的：\n%s", reg)
	}
}

// 匯出隱藏版只在她的圖真的收進來時才帶紀錄（圖都被排除，例如只有 .locked. 的，就不帶紀錄）。
func TestExportVariantOnlyWithItsImages(t *testing.T) {
	a, sa := homeA(t)
	writeFile(t, filepath.Join(a, "personas", "avatars", "locked", "luna.locked.png"), "x")
	sa.put(t, "closet.variant", "locked", map[string]any{"name": "只有鎖住的", "dir": "avatars/locked"})
	var buf bytes.Buffer
	res, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"}, Store: sa}, &buf)
	if err != nil || res.Records != 4 {
		t.Fatalf("不該帶 closet.variant/locked：%+v %v", res, err)
	}
}

func manifestOf(t *testing.T, data []byte) Manifest {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == manifestName {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			var m Manifest
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			return m
		}
	}
	t.Fatal("沒有 manifest.json")
	return Manifest{}
}

// 套件宣告 export_omit 的欄位（PKG-58，衣櫃隱藏版的開關 on）：匯出時拿掉；匯入時本機已有這筆就保留本機的值，
// 只差這個欄位不算「你改過的」；--force 蓋的時候也保留本機的開關。時段 windows 照帶。
func TestExportOmitKeepsLocalSwitch(t *testing.T) {
	a, sa := homeA(t)
	data := exportTo(t, a, sa, "luna")
	var hidden *Record
	for i, r := range manifestOf(t, data).Records {
		if r.Key() == "closet.variant/hidden" {
			hidden = &manifestOf(t, data).Records[i]
		}
	}
	if hidden == nil {
		t.Fatal("匯出檔要有 closet.variant/hidden")
	}
	if _, ok := hidden.Fields["on"]; ok {
		t.Errorf("開關 on 不跟著走：%v", hidden.Fields)
	}
	if w, _ := hidden.Fields["windows"].([]any); len(w) != 1 || w[0] != "22:00-02:00" {
		t.Errorf("時段 windows 照帶：%v", hidden.Fields)
	}

	b := t.TempDir()
	sb := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	if _, err := importBytes(b, sb, data, false, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := sb.recs["closet.variant/hidden"].Fields["on"]; ok {
		t.Fatalf("新匯入的隱藏版沒有開關：%v", sb.recs["closet.variant/hidden"].Fields)
	}
	// B 自己打開開關；A 改了時段。再匯入：只差開關不算改過，時段換新的、開關留 B 的。
	f := sb.recs["closet.variant/hidden"].Fields
	sb.put(t, "closet.variant", "hidden", map[string]any{"name": f["name"], "dir": f["dir"], "button": f["button"], "windows": f["windows"], "on": true})
	sa.put(t, "closet.variant", "hidden", map[string]any{"name": "隱藏版", "dir": "avatars/hidden", "button": true, "on": false, "windows": []any{"21:00-23:00"}})
	res, err := importBytes(b, sb, exportTo(t, a, sa, "luna"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	got := sb.recs["closet.variant/hidden"].Fields
	if strings.Join(res.RecordsReplaced, ",") != "closet.variant/hidden" || len(res.RecordsKept) != 0 {
		t.Fatalf("只差開關要照常換：換 %v、沒換 %v", res.RecordsReplaced, res.RecordsKept)
	}
	if got["on"] != true || fmt.Sprint(got["windows"]) != "[21:00-23:00]" {
		t.Fatalf("開關留本機、時段換新：%v", got)
	}
	// B 改了名字、關掉開關；--force 蓋名字，開關照樣留 B 的。
	sb.put(t, "closet.variant", "hidden", map[string]any{"name": "B 的名字", "dir": "avatars/hidden", "button": true, "windows": got["windows"], "on": false})
	res, err = importBytes(b, sb, exportTo(t, a, sa, "luna"), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := sb.recs["closet.variant/hidden"].Fields; got["name"] != "隱藏版" || got["on"] != false || res.Backup == "" {
		t.Fatalf("--force 蓋名字、留開關：%v（備份 %q）", got, res.Backup)
	}
}

// 自己的隱藏版（personas/mine/adult/，DEV-141）一律不匯出：資料夾裡有她的圖、隱藏版紀錄也在，匯出檔裡都沒有，並列出原因。
func TestExportNeverCarriesAdult(t *testing.T) {
	a, sa := homeA(t)
	writeFile(t, filepath.Join(a, "personas", "mine", "adult", "luna.png"), "adult")
	writeFile(t, filepath.Join(a, "personas", "mine", "adult", "full", "luna.png"), "adult-full")
	sa.put(t, "closet.variant", "adult", map[string]any{"name": "自己的隱藏版", "dir": "mine/adult", "button": true})
	// 舊家的 level/ 資料夾（level/hidden、level/extra）與名字含 adult 這類字的資料夾也算自己的隱藏版（跟角色包同一份規則）。
	writeFile(t, filepath.Join(a, "personas", "avatars", "level", "hidden", "luna.png"), "h1")
	writeFile(t, filepath.Join(a, "personas", "avatars", "level", "extra", "luna.png"), "h2")
	writeFile(t, filepath.Join(a, "personas", "avatars", "nsfw", "luna.png"), "h3")
	sa.put(t, "closet.variant", "h1", map[string]any{"name": "隱藏版", "dir": "avatars/level/hidden"})
	sa.put(t, "closet.variant", "h2", map[string]any{"name": "隱藏版 2", "dir": "avatars/level/extra"})
	sa.put(t, "closet.variant", "h3", map[string]any{"name": "h3", "dir": "avatars/nsfw"})
	var buf bytes.Buffer
	res, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"}, Store: sa}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range zipNames(t, buf.Bytes()) {
		if IsAdultPath(n) || strings.Contains(n, "level") {
			t.Fatalf("匯出檔不能有自己的隱藏版：%s", n)
		}
	}
	for _, r := range manifestOf(t, buf.Bytes()).Records {
		if k := r.Key(); k == "closet.variant/adult" || k == "closet.variant/h1" || k == "closet.variant/h2" || k == "closet.variant/h3" {
			t.Fatal("自己的隱藏版的紀錄不能匯出")
		}
	}
	skipped := strings.Join(res.Skipped, "\n")
	if !strings.Contains(skipped, "personas/mine/adult：自己的隱藏版不匯出") || !strings.Contains(skipped, "personas/avatars/level/hidden：自己的隱藏版不匯出") {
		t.Fatalf("要列出沒帶的原因：%v", res.Skipped)
	}
	if !IsAdultPath("mine/adult/luna.png") || !IsAdultPath("x/ADULT") || !IsAdultPath("avatars/level/extra") || IsAdultPath("mine/avatars/luna.png") {
		t.Fatal("IsAdultPath 判斷不對")
	}
}

// 主人點名的角色匯出不看套件的 pack_only（PKG-68 只管官方角色包）：主人自己的衣服（source = mine）、設計中的（state = draft）
// 照舊跟著角色匯出，對方的家匯入也照舊寫進去。
func TestExportIgnoresPackOnly(t *testing.T) {
	a, sa := homeA(t)
	sa.put(t, "closet.outfit", "luna-my-9", map[string]any{"persona": "luna", "outfit": "my-9", "name": "設計中的", "cost": 0, "state": "draft", "source": "mine"})
	sa.put(t, "closet.outfit", "luna-next", map[string]any{"persona": "luna", "outfit": "next", "name": "官方設計中", "cost": 0, "state": "draft", "source": "official"})
	var buf bytes.Buffer
	exp, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"}, Store: sa}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(exp.Skipped, "\n"), "沒帶") {
		t.Errorf("匯出不該因為 pack_only 少帶：%v", exp.Skipped)
	}
	sb := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	res, err := importBytes(t.TempDir(), sb, buf.Bytes(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), res.RecordsAdded...)
	sort.Strings(got)
	if strings.Join(got, ",") != "closet.outfit/luna-base,closet.outfit/luna-my-3,closet.outfit/luna-my-9,closet.outfit/luna-next,closet.outfit/luna-sea,closet.variant/hidden" {
		t.Fatalf("主人自己的與設計中的衣服照舊跟著匯出、匯入：%v（略過 %v）", got, res.RecordsSkipped)
	}
	if r := sb.recs["closet.outfit/luna-my-9"]; r.Fields["state"] != "draft" || r.Fields["source"] != "mine" {
		t.Errorf("欄位照原樣：%+v", r.Fields)
	}
}
