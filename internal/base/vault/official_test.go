package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// openOfficialStore 開一個「官方角色放家外」的家（第 2 版佈局、沒寫 official_personas）：回模組、家、狀態資料夾。
func openOfficialStore(t *testing.T) (*Module, string, string) {
	t.Helper()
	home, state := t.TempDir(), t.TempDir()
	cfg := "schema = 2\n" + storeConfig + "\n[layout]\nversion = 2\nstate = " + `"` + state + `"` + "\n"
	if err := platform.AtomicWrite(filepath.Join(home, "home.toml"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Taipei")
	m := New(Deps{Clock: platform.NewFakeClock(time.Date(2026, 11, 12, 12, 0, 0, 0, loc))})
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return m, home, state
}

// treeHash 回資料夾底下每個檔的相對路徑與內容雜湊（排序過，一行一個）。
func treeHash(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(root, path)
		lines = append(lines, rel+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// 官方層（DEV-154）：官方角色包帶來的紀錄寫在家外，家裡的檔一個都不變；讀的時候合起來、家裡的優先；
// 改官方層的一筆＝在家裡寫一份，官方之後換新也不蓋主人那份。
func TestOfficialLayerStaysOutOfHome(t *testing.T) {
	m, home, state := openOfficialStore(t)
	ctx := context.Background()
	m.SetPackageSchemas("demo", []Spec{noteSpec(), daySpec()})
	if !m.HasOfficialLayer() {
		t.Fatal("官方角色放家外的家要有官方層")
	}
	if _, err := m.Put(ctx, Record{Kind: "demo.day", ID: "mine-1", Fields: map[string]any{"weight": 1.0}}, "owner(cli)", ""); err != nil {
		t.Fatal(err)
	}
	before := treeHash(t, filepath.Join(home, "vault"))

	for id, w := range map[string]float64{"kaguya-base": 10, "kaguya-moon": 20} {
		if _, err := m.OfficialPut(ctx, Record{Kind: "demo.day", ID: id, Fields: map[string]any{"weight": w}}); err != nil {
			t.Fatalf("OfficialPut %s：%v", id, err)
		}
	}
	if got := treeHash(t, filepath.Join(home, "vault")); got != before {
		t.Fatalf("寫官方層不能動到家裡：\n%s\n---\n%s", before, got)
	}
	if _, err := os.Stat(filepath.Join(state, "personas", "records", "pkg", "demo", "day", "kaguya-base.md")); err != nil {
		t.Fatalf("官方層的檔要在家外：%v", err)
	}
	// 讀：合起來、照編號排。
	rows, err := m.List(ctx, "demo.day", Query{})
	if err != nil || len(rows) != 3 || rows[0].ID != "kaguya-base" || rows[1].ID != "kaguya-moon" || rows[2].ID != "mine-1" {
		t.Fatalf("List 要合起來：%v %v", rows, err)
	}
	got, err := m.Get(ctx, "demo.day", "kaguya-base")
	if err != nil || got.Fields["weight"] != 10.0 || got.Fields["by"] != OfficialBy {
		t.Fatalf("Get 官方層：%v %v", got.Fields, err)
	}
	// 新增同編號要擋；不帶對的 revision 也擋。
	if _, err := m.Put(ctx, Record{Kind: "demo.day", ID: "kaguya-base", Fields: map[string]any{"weight": 99.0}}, "owner(cli)", ""); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("官方層已有的編號不能當新增：%v", err)
	}
	if _, err := m.Put(ctx, Record{Kind: "demo.day", ID: "kaguya-base", Fields: map[string]any{"weight": 99.0}}, "owner(cli)", "wrong"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("revision 不對要擋：%v", err)
	}
	if err := m.Archive(ctx, "demo.day", "kaguya-base", "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("官方層的不能封存：%v", err)
	}
	// 主人改一筆：家裡多一份，官方層那個檔不動。
	officialFile := filepath.Join(state, "personas", "records", "pkg", "demo", "day", "kaguya-base.md")
	officialBefore, _ := os.ReadFile(officialFile)
	patched, err := m.Patch(ctx, "demo.day", "kaguya-base", map[string]any{"weight": 11.0}, nil, "owner(cli)")
	if err != nil || patched.Fields["weight"] != 11.0 {
		t.Fatalf("改官方層的一筆：%v %v", patched.Fields, err)
	}
	if _, err := os.Stat(pkgPath(home, "day", "kaguya-base")); err != nil {
		t.Fatalf("主人改過的要寫在家裡：%v", err)
	}
	if now, _ := os.ReadFile(officialFile); string(now) != string(officialBefore) {
		t.Fatal("官方層的檔被動到")
	}
	// 官方換新：主人那份不被蓋；沒改過的那筆跟著換。
	mineBefore := treeHash(t, filepath.Join(home, "vault"))
	for id, w := range map[string]float64{"kaguya-base": 12, "kaguya-moon": 22} {
		if _, err := m.OfficialPut(ctx, Record{Kind: "demo.day", ID: id, Fields: map[string]any{"weight": w}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := treeHash(t, filepath.Join(home, "vault")); got != mineBefore {
		t.Fatal("官方換新不能動到家裡")
	}
	if got, _ := m.Get(ctx, "demo.day", "kaguya-base"); got.Fields["weight"] != 11.0 {
		t.Fatalf("主人改過的留著：%v", got.Fields)
	}
	if got, _ := m.Get(ctx, "demo.day", "kaguya-moon"); got.Fields["weight"] != 22.0 {
		t.Fatalf("沒改過的跟著官方：%v", got.Fields)
	}
	if o, ok := m.OfficialGet("demo.day", "kaguya-base"); !ok || o.Fields["weight"] != 12.0 {
		t.Fatalf("OfficialGet 讀官方層本身：%v", o.Fields)
	}
	if rows, _ := m.List(ctx, "demo.day", Query{}); len(rows) != 3 {
		t.Fatalf("同編號不重複列：%v", rows)
	}
	// 官方層不收流水號的種類、不收格式不對的。
	if _, err := m.OfficialPut(ctx, Record{Kind: "demo.note", ID: "N-1", Fields: map[string]any{"text": "x"}}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("流水號的種類不放官方層：%v", err)
	}
	if _, err := m.OfficialPut(ctx, Record{Kind: "demo.day", ID: "x", Fields: map[string]any{}}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("缺必填要擋：%v", err)
	}
	// 重開：官方層還讀得到；套件停用就看不到，啟用又回來。
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(m.Stop(ctx))
	must(m.Open(home))
	m.SetPackageSchemas("demo", []Spec{noteSpec(), daySpec()})
	if rows, _ := m.List(ctx, "demo.day", Query{}); len(rows) != 3 {
		t.Fatalf("重開後：%v", rows)
	}
	m.SetPackageSchemas("demo", nil)
	if _, ok := m.OfficialGet("demo.day", "kaguya-moon"); ok {
		t.Fatal("套件停用後官方層的紀錄不該還在")
	}
	m.SetPackageSchemas("demo", []Spec{noteSpec(), daySpec()})
	if _, ok := m.OfficialGet("demo.day", "kaguya-moon"); !ok {
		t.Fatal("套件啟用後官方層的紀錄要回來")
	}
}

// 官方角色還放在家裡的家（現在的五個家）沒有官方層：行為跟以前一樣。
func TestNoOfficialLayerWhenPersonasAtHome(t *testing.T) {
	m, _ := openStore(t)
	m.SetPackageSchemas("demo", []Spec{daySpec()})
	if m.HasOfficialLayer() {
		t.Fatal("第 1 版佈局不該有官方層")
	}
	if _, err := m.OfficialPut(context.Background(), Record{Kind: "demo.day", ID: "a", Fields: map[string]any{"weight": 1.0}}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有官方層要白話擋下：%v", err)
	}
}

// 官方層寫一筆會發變更事件給訂閱的（新增是 created、換掉是 updated，署名 official），衣櫃等套件的程式照它馬上重算（DEV-157）；
// 家裡有同編號的（主人改過的那份）不發：大家看到的還是家裡那份，沒變。
// Guards: VLT-19
func TestOfficialPutNotifiesWatchers(t *testing.T) {
	m, _, _ := openOfficialStore(t)
	m.SetPackageSchemas("demo", []Spec{noteSpec(), daySpec()})
	ch, ctx := subscribe(t, m, "demo.day")
	if _, err := m.OfficialPut(ctx, Record{Kind: "demo.day", ID: "kaguya-base", Fields: map[string]any{"weight": 1.0}}); err != nil {
		t.Fatal(err)
	}
	c := receive(t, ctx, ch)
	if c.Type != sdk.ChangeTypeCreated || c.Record.ID != "kaguya-base" || c.By.Name != OfficialBy || c.Record.Revision == "" {
		t.Fatalf("新增：%+v %+v", c, c.Record)
	}
	if _, err := m.OfficialPut(ctx, Record{Kind: "demo.day", ID: "kaguya-base", Fields: map[string]any{"weight": 2.0}}); err != nil {
		t.Fatal(err)
	}
	if c := receive(t, ctx, ch); c.Type != sdk.ChangeTypeUpdated || c.Record.ID != "kaguya-base" {
		t.Fatalf("換掉：%+v", c)
	}
	// 主人在家裡改了一份（Put 自己會發事件）；之後官方層再換新：不發。
	if _, err := m.Patch(ctx, "demo.day", "kaguya-base", map[string]any{"weight": 9.0}, nil, "owner(cli)"); err != nil {
		t.Fatal(err)
	}
	if c := receive(t, ctx, ch); c.By.Name != "owner(cli)" {
		t.Fatalf("主人的修改：%+v", c)
	}
	if _, err := m.OfficialPut(ctx, Record{Kind: "demo.day", ID: "kaguya-base", Fields: map[string]any{"weight": 3.0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.OfficialPut(ctx, Record{Kind: "demo.day", ID: "kaguya-moon", Fields: map[string]any{"weight": 4.0}}); err != nil {
		t.Fatal(err)
	}
	if c := receive(t, ctx, ch); c.Record.ID != "kaguya-moon" {
		t.Fatalf("家裡有同編號的那筆不該發事件，下一個應該是 kaguya-moon：%+v", c.Record)
	}
}
