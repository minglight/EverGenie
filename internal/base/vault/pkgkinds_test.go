package vault

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// noteSpec 是測試用的套件資料種類 demo.note：seq 編號（N-<n>），date 與 score 選填、text 必填。
func noteSpec() Spec {
	return Spec{
		Kind: "demo.note", IDMode: "seq", IDPrefix: "N-",
		Path: func(id string) string { return PackagePath("demo", "note", id) },
		Fields: []Field{
			{Name: "date", Type: Date}, {Name: "text", Type: String, Required: true}, {Name: "score", Type: Number},
			{Name: "count", Type: Int}, {Name: "mood", Type: Enum, Enum: []string{"good", "bad"}},
		},
	}
}

func daySpec() Spec {
	return Spec{
		Kind: "demo.day", IDMode: "given",
		Path:   func(id string) string { return PackagePath("demo", "day", id) },
		Fields: []Field{{Name: "weight", Type: Number, Required: true}},
	}
}

func pkgPath(home, kind, id string) string {
	return filepath.Join(home, "vault", "pkg", "demo", kind, id+".md")
}

func putNote(t *testing.T, m *Module, fields map[string]any) Record {
	t.Helper()
	ctx := context.Background()
	id, err := m.NextID(ctx, "demo.note")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.Put(ctx, Record{Kind: "demo.note", ID: id, Fields: fields}, "owner(cli)", "")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// 套件註冊自己的種類之後：寫入走共通流程（驗證、路徑、變更紀錄）、NextID 發 prefix 編號、列得出來、封存搬到同種類的 archive/；
// 再整批換掉（傳空）就看不到了，換回來又回來。
// Guards: REQ-173
func TestPackageSchemaRegisterAndIndex(t *testing.T) {
	m, home := openStore(t)
	ctx := context.Background()
	m.SetPackageSchemas("demo", []Spec{noteSpec(), daySpec()})

	first := putNote(t, m, map[string]any{"text": "a", "date": "2026-10-25", "score": 3.5, "count": 3.0})
	second := putNote(t, m, map[string]any{"text": "b"})
	if first.ID != "N-1" || second.ID != "N-2" {
		t.Fatalf("編號：%s %s", first.ID, second.ID)
	}
	if _, err := os.Stat(pkgPath(home, "note", "N-1")); err != nil {
		t.Fatalf("檔案要在 vault/pkg/demo/note/：%v", err)
	}
	if first.Fields["id"] != "N-1" || first.Fields["by"] != "owner(cli)" {
		t.Fatalf("共通欄位：%v", first.Fields)
	}
	rows, err := m.List(ctx, "demo.note", Query{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("List：%v %v", rows, err)
	}
	// given 種類：id 自己給，格式不符回 INVALID。
	if _, err := m.Put(ctx, Record{Kind: "demo.day", ID: "2026-10-25", Fields: map[string]any{"weight": 70.5}}, "owner(cli)", ""); err != nil {
		t.Fatal(err)
	}
	// 底線也收：主人自己的角色代號是 my_<名字>，跟著角色走的紀錄編號會帶著它（DEV-154）。
	if _, err := m.Put(ctx, Record{Kind: "demo.day", ID: "my_kaguya-base", Fields: map[string]any{"weight": 1}}, "owner(cli)", ""); err != nil {
		t.Fatalf("編號帶底線要收：%v", err)
	}
	for _, bad := range []string{"Bad", "-x", "_x", "a b", "A1"} {
		if _, err := m.Put(ctx, Record{Kind: "demo.day", ID: bad, Fields: map[string]any{"weight": 1}}, "owner(cli)", ""); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("id %q 要被擋：%v", bad, err)
		}
	}
	// seq 種類不收別的編號格式。
	if _, err := m.Put(ctx, Record{Kind: "demo.note", ID: "X-1", Fields: map[string]any{"text": "x"}}, "owner(cli)", ""); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Errorf("seq 編號格式：%v", err)
	}
	// 欄位不符：缺必填、型別錯、enum 外，都帶欄位名。
	for field, fields := range map[string]map[string]any{
		"text":  {"score": 1},
		"score": {"text": "x", "score": "high"},
		"count": {"text": "x", "count": 1.5},
		"mood":  {"text": "x", "mood": "meh"},
		"date":  {"text": "x", "date": "2026/10/25"},
	} {
		_, err := m.Put(ctx, Record{Kind: "demo.note", ID: "N-9", Fields: fields}, "owner(cli)", "")
		var pe *platform.Err
		if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !asErr(err, &pe) || pe.Field != field {
			t.Errorf("欄位 %s 要被擋並帶欄位名：%v", field, err)
		}
	}
	// 封存：搬到同種類的 archive/，查得到、預設列表看不到。
	if err := m.ArchiveNow(ctx, "demo.note", "N-2", "owner(cli)"); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(home, "vault", "pkg", "demo", "note", "archive", "*", "N-2.md"))
	if len(matches) != 1 {
		t.Fatalf("封存位置：%v", matches)
	}
	if rows, _ := m.List(ctx, "demo.note", Query{}); len(rows) != 1 {
		t.Fatalf("封存後列表：%v", rows)
	}
	if rows, _ := m.List(ctx, "demo.note", Query{IncludeArchived: true}); len(rows) != 2 {
		t.Fatalf("含封存：%v", rows)
	}
	// 沒有自動封存：清道夫不動套件的資料。
	if rep, err := m.Sweep(ctx, m.d.Clock.Now().AddDate(5, 0, 0), false); err != nil {
		t.Fatal(err)
	} else {
		for _, it := range rep.Items {
			if strings.HasPrefix(it.Kind, "demo.") {
				t.Errorf("清道夫不該動套件的資料：%+v", it)
			}
		}
	}
	// 拿掉種類：資料還在檔案裡，但讀不到；換回來又回來；編號接著發。
	m.SetPackageSchemas("demo", nil)
	if _, err := m.Get(ctx, "demo.note", "N-1"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("拿掉後：%v", err)
	}
	m.SetPackageSchemas("demo", []Spec{noteSpec()})
	if rows, _ := m.List(ctx, "demo.note", Query{IncludeArchived: true}); len(rows) != 2 {
		t.Fatalf("換回來：%v", rows)
	}
	if r := putNote(t, m, map[string]any{"text": "c"}); r.ID != "N-3" {
		t.Fatalf("編號接著發：%s", r.ID)
	}
	// 只換一個套件的種類，不影響別的套件或核心種類。
	m.SetPackageSchemas("other", []Spec{{Kind: "other.x", IDMode: "given", Path: func(id string) string { return PackagePath("other", "x", id) }}})
	m.SetPackageSchemas("other", nil)
	if _, err := m.Get(ctx, "demo.note", "N-1"); err != nil {
		t.Fatalf("別的套件的更動不能影響 demo：%v", err)
	}
}

func asErr(err error, target **platform.Err) bool {
	pe, ok := err.(*platform.Err)
	if ok {
		*target = pe
	}
	return ok
}

// number 型別收整數與浮點數，不收字串、布林；int 收整數值（JSON 來的 3.0 也算）不收 3.5。
// Guards: REQ-173
func TestNumberField(t *testing.T) {
	f := Field{Name: "n", Type: Number}
	for _, ok := range []any{3, int64(3), 3.5, float32(2.5), uint8(1), 0, -1.25} {
		if !validField(f, ok, nil) {
			t.Errorf("number 要收 %#v", ok)
		}
	}
	for _, bad := range []any{"3", true, nil, []any{1}, map[string]any{}} {
		if bad != nil && validField(f, bad, nil) {
			t.Errorf("number 不收 %#v", bad)
		}
	}
	i := Field{Name: "i", Type: Int}
	for _, ok := range []any{3, 3.0, float64(-2)} {
		if !validField(i, ok, nil) {
			t.Errorf("int 要收 %#v", ok)
		}
	}
	if validField(i, 3.5, nil) {
		t.Error("int 不收 3.5")
	}
	// 寫進檔案再讀回來數字不變（整數留整數、小數留小數）。
	m, _ := openStore(t)
	m.SetPackageSchemas("demo", []Spec{noteSpec()})
	r := putNote(t, m, map[string]any{"text": "x", "score": 2.5, "count": 4})
	got, err := m.Get(context.Background(), "demo.note", r.ID)
	if err != nil || got.Fields["score"] != 2.5 || got.Fields["count"] != 4 {
		t.Fatalf("讀回：%v %v", got.Fields, err)
	}
}

// 沒註冊的套件種類（停用、解除安裝、被拒）：檔案留在原地、不進索引、doctor 的資料檢查與 Invalid 都不報錯；重新註冊就回來。
// Guards: REQ-173
func TestUnregisteredPackageDataKept(t *testing.T) {
	m, home := openStore(t)
	ctx := context.Background()
	m.SetPackageSchemas("demo", []Spec{noteSpec()})
	putNote(t, m, map[string]any{"text": "keep me"})
	path := pkgPath(home, "note", "N-1")

	m.SetPackageSchemas("demo", nil)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("資料要留在原地：%v", err)
	}
	// 另外放一個格式壞的檔案與一個別的種類的檔案，沒註冊時一樣不報錯。
	for rel, body := range map[string]string{"note/N-9.md": "不是 frontmatter", "other/x.md": "---\nid: x\n---\n"} {
		p := filepath.Join(home, "vault", "pkg", "demo", rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bad, err := m.InvalidRecords(ctx)
	if err != nil || len(bad) != 0 {
		t.Fatalf("沒註冊的資料不該被報錯：%v %v", bad, err)
	}
	if len(m.Invalid()) != 0 {
		t.Fatalf("Invalid：%v", m.Invalid())
	}
	// 沒註冊就不能寫。
	if _, err := m.Put(ctx, Record{Kind: "demo.note", ID: "N-5", Fields: map[string]any{"text": "x"}}, "owner(cli)", ""); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒註冊不能寫：%v", err)
	}
	// 重新啟動（重新開啟 vault）時，已註冊的才進索引，沒註冊的略過。
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	m.SetPackageSchemas("demo", nil)
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	if len(m.Invalid()) != 0 {
		t.Fatalf("重開後 Invalid：%v", m.Invalid())
	}
	if _, err := m.Get(ctx, "demo.note", "N-1"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒註冊讀不到：%v", err)
	}
	// 重新註冊：舊資料回來、壞檔現在才被報錯，編號從已有的最大編號接著發。
	m.SetPackageSchemas("demo", []Spec{noteSpec()})
	if got, err := m.Get(ctx, "demo.note", "N-1"); err != nil || got.Body != "" || got.Fields["text"] != "keep me" {
		t.Fatalf("重新註冊：%v %v", got, err)
	}
	if id, err := m.NextID(ctx, "demo.note"); err != nil || id != "N-2" {
		t.Fatalf("接著發號：%s %v", id, err)
	}
	if len(m.Invalid()) != 1 || !strings.HasSuffix(m.Invalid()[0], "note/N-9.md") {
		t.Fatalf("註冊後壞檔才算：%v", m.Invalid())
	}
}

// given 種類呼叫 NextID 回 INVALID 並說明；沒註冊的種類照舊。
// Guards: REQ-173
func TestGivenKindHasNoNextID(t *testing.T) {
	m, _ := openStore(t)
	m.SetPackageSchemas("demo", []Spec{daySpec()})
	_, err := m.NextID(context.Background(), "demo.day")
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "這種資料的編號要自己給") {
		t.Fatalf("given 發號：%v", err)
	}
}

// 自己給編號的種類（例如用日期當編號）封存後，同一個編號可以再新增一筆：封存的留在 archive/ 不動、新的列得出來；
// 同一個月再封存一次不蓋掉前一份；重開後正本優先、封存檔不算壞檔（DEV-151）。
func TestGivenIDReusableAfterArchive(t *testing.T) {
	m, home := openStore(t)
	ctx := context.Background()
	m.SetPackageSchemas("demo", []Spec{daySpec()})
	put := func(w float64) Record {
		t.Helper()
		r, err := m.Put(ctx, Record{Kind: "demo.day", ID: "2026-10-23", Fields: map[string]any{"weight": w}}, "owner(cli)", "")
		if err != nil {
			t.Fatalf("新增 %v：%v", w, err)
		}
		return r
	}
	put(70)
	if _, err := m.Put(ctx, Record{Kind: "demo.day", ID: "2026-10-23", Fields: map[string]any{"weight": 71.0}}, "owner(cli)", ""); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "已經有資料") || strings.Contains(err.Error(), "資料已更新") {
		t.Fatalf("正本還在時重複新增要說編號已經有資料：%v", err)
	}
	if err := m.ArchiveNow(ctx, "demo.day", "2026-10-23", "owner(cli)"); err != nil {
		t.Fatal(err)
	}
	if got := put(71); got.Archived || got.Fields["weight"] != 71.0 {
		t.Fatalf("封存後再新增：%+v", got)
	}
	list, err := m.List(ctx, "demo.day", Query{})
	if err != nil || len(list) != 1 || list[0].Fields["weight"] != 71.0 {
		t.Fatalf("列表要看到新的一筆：%v %+v", err, list)
	}
	first := filepath.Join(home, "vault", "pkg", "demo", "day", "archive", "2026-10", "2026-10-23.md")
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("封存的那份要留著：%v", err)
	}
	// 同一個月再封存：放 2026-10-2，不蓋掉前一份。
	if err := m.ArchiveNow(ctx, "demo.day", "2026-10-23", "owner(cli)"); err != nil {
		t.Fatalf("同月第二次封存：%v", err)
	}
	second := filepath.Join(home, "vault", "pkg", "demo", "day", "archive", "2026-10-2", "2026-10-23.md")
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("第二份封存：%v", err)
	}
	put(72)

	// 重開：正本＋兩份封存，索引留正本，沒有壞檔。
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatalf("重開：%v", err)
	}
	m.SetPackageSchemas("demo", []Spec{daySpec()})
	got, err := m.Get(ctx, "demo.day", "2026-10-23")
	if err != nil || got.Archived || fmt.Sprint(got.Fields["weight"]) != "72" {
		t.Fatalf("重開後要拿到正本：%v %+v", err, got)
	}
	if bad := m.Invalid(); len(bad) != 0 {
		t.Fatalf("封存檔不該算壞檔：%v", bad)
	}

	// 修改只動正本；正本封存後修改回找不到，不改到 archive/ 裡那筆。
	if _, err := m.Patch(ctx, "demo.day", "2026-10-23", map[string]any{"weight": 73.0}, nil, "owner(cli)"); err != nil {
		t.Fatalf("修改正本：%v", err)
	}
	if err := m.ArchiveNow(ctx, "demo.day", "2026-10-23", "owner(cli)"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Patch(ctx, "demo.day", "2026-10-23", map[string]any{"weight": 99.0}, nil, "owner(cli)"); platform.CodeOf(err) != sdk.ErrorCodeNotFound || strings.Contains(err.Error(), "資料已更新") {
		t.Fatalf("修改封存的要回找不到：%v", err)
	}

	// 正本在外面被刪掉：被蓋過的封存檔接回索引。
	put(74)
	live := pkgPath(home, "day", "2026-10-23")
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	m.refreshPath(&watchState{log: slog.New(slog.DiscardHandler)}, live)
	if got, err := m.Get(ctx, "demo.day", "2026-10-23"); err != nil || !got.Archived {
		t.Fatalf("正本刪掉後要接回封存的：%v %+v", err, got)
	}
}

// 核心發號的種類（任務）正本與封存同一個 id 仍然是重複，開不起來（只有 given 種類可以重用編號）。
func TestCoreKindDuplicateStillConflicts(t *testing.T) {
	m, home := openStore(t)
	ctx := context.Background()
	putTask(t, m, "SEC-1")
	data, err := os.ReadFile(filepath.Join(home, "vault", "tasks", "SEC-1.md"))
	if err != nil {
		t.Fatal(err)
	}
	dup := filepath.Join(home, "vault", "tasks", "archive", "2026-09", "SEC-1.md")
	if err := os.MkdirAll(filepath.Dir(dup), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("任務重複要擋：%v", err)
	}
	if err := os.Remove(dup); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
}
