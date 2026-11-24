package personaexport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// photoKinds 是 closetKinds 加上照官方衣櫃宣告的照片紀錄（DEV-160）：file 指著檔、隱藏版不跟著走（export_only）。
func photoKinds() []KindInfo {
	str := func(n string, req bool) sdk.DataFieldDecl {
		return sdk.DataFieldDecl{Name: n, Type: "string", Required: req}
	}
	return append(closetKinds(), KindInfo{Kind: "closet.photo", Decl: sdk.DataKindDecl{
		Kind: "photo", IDs: "given", Export: sdk.DataExportByPersona, PersonaField: "persona", PersonaSource: "source",
		ExportOnly: map[string][]string{"hidden": {"false"}}, FileField: "file",
		Fields: []sdk.DataFieldDecl{
			str("persona", true), str("outfit", true), str("type", true), {Name: "order", Type: "int", Required: true},
			{Name: "hidden", Type: "bool", Required: true}, {Name: "source", Type: "enum", Enum: []string{"official", "mine"}, Required: true}, str("file", true),
		},
	}})
}

func photoOf(persona, outfit, file, source string, hidden bool) map[string]any {
	return map[string]any{"persona": persona, "outfit": outfit, "type": "half", "order": 1, "hidden": hidden, "source": source, "file": file}
}

// 匯出照照片紀錄帶圖（DEV-160 S2）：紀錄指著的檔才帶（mine 的放 extra/mine/），加上角色本身的頭像；沒有紀錄指著的不帶；
// 隱藏版的紀錄與它的圖都不帶，只裝隱藏版的資料夾的紀錄（closet.variant）也就不跟著走。匯入的家照紀錄放回原位。
func TestExportCarriesFilesByPhotoRecords(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	sa.put(t, "closet.photo", "luna-base--half1", photoOf("luna", "luna-base", "avatars/luna.png", "official", false))
	sa.put(t, "closet.photo", "luna-sea--half1", photoOf("luna", "luna-sea", "avatars/luna@sea.png", "official", false))
	sa.put(t, "closet.photo", "luna-sea--full1", photoOf("luna", "luna-sea", "avatars/full/luna@sea.png", "official", false))
	sa.put(t, "closet.photo", "luna-my-3--half1", photoOf("luna", "luna-my-3", "avatars/luna@my-3.png", "mine", false))
	sa.put(t, "closet.photo", "luna-sea--half1-h", photoOf("luna", "luna-sea", "avatars/hidden/luna@sea.png", "official", true))
	// 隱藏版的照片放在會整個帶走的衣服資料夾、名字也看不出來：照紀錄的 hidden 一樣不帶。
	writeFile(t, filepath.Join(a, "personas", "wardrobe", "luna", "plain.png"), "hidden-in-wardrobe")
	sa.put(t, "closet.photo", "luna-sea--half2-h", photoOf("luna", "luna-sea", "wardrobe/luna/plain.png", "official", true))
	sa.put(t, "closet.photo", "athena-base--half1", photoOf("athena", "athena-base", "avatars/athena.png", "official", false))
	var buf bytes.Buffer
	res, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"}, Store: sa}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var media []string
	for _, n := range zipNames(t, buf.Bytes()) {
		if strings.Contains(n, "avatars/") {
			media = append(media, strings.TrimPrefix(n, filesPrefix))
		}
	}
	if got := strings.Join(media, ","); got != "avatars/full/luna@sea.png,avatars/luna.mp4,avatars/luna.png,avatars/luna@sea.png,extra/mine/avatars/luna@my-3.png" {
		t.Fatalf("匯出檔裡的圖：%s", got)
	}
	var keys []string
	for _, r := range manifestOf(t, buf.Bytes()).Records {
		keys = append(keys, r.Key())
	}
	sort.Strings(keys)
	if got := strings.Join(keys, ","); got != "closet.outfit/luna-base,closet.outfit/luna-my-3,closet.outfit/luna-sea,closet.photo/luna-base--half1,closet.photo/luna-my-3--half1,closet.photo/luna-sea--full1,closet.photo/luna-sea--half1" {
		t.Fatalf("匯出的紀錄：%s", got)
	}
	if !strings.Contains(strings.Join(res.Skipped, "\n"), "closet.photo：2 筆不跟著角色走") {
		t.Errorf("要列出隱藏版沒帶：%v", res.Skipped)
	}
	if slices.Contains(zipNames(t, buf.Bytes()), filesPrefix+"wardrobe/luna/plain.png") ||
		!strings.Contains(strings.Join(res.Skipped, "\n"), "wardrobe/luna/plain.png：是不跟著角色走的紀錄 closet.photo/luna-sea--half2-h 的圖，不帶") {
		t.Errorf("衣服資料夾裡的隱藏版照片不帶：%v", res.Skipped)
	}

	b := t.TempDir()
	sb := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	if _, err := importBytes(b, sb, buf.Bytes(), false, false); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"avatars/luna@sea.png", "avatars/full/luna@sea.png", "mine/avatars/luna@my-3.png"} {
		if !fileExists(filepath.Join(b, "personas", filepath.FromSlash(f))) {
			t.Errorf("匯入後要有 %s", f)
		}
	}
	if r := sb.recs["closet.photo/luna-my-3--half1"]; r.Fields["file"] != "avatars/luna@my-3.png" || r.Fields["source"] != "mine" {
		t.Errorf("照片紀錄照原樣：%+v", r.Fields)
	}
	if fileExists(filepath.Join(b, "personas", "avatars", "hidden")) {
		t.Error("隱藏版的圖不跟著走")
	}
}

// 手改過的匯出檔帶了隱藏版的照片紀錄：匯入也不收（套件宣告這種紀錄不跟著角色走）。
func TestImportSkipsHiddenPhotoRecords(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	sa.put(t, "closet.photo", "luna-base--half1", photoOf("luna", "luna-base", "avatars/luna.png", "official", false))
	data := exportTo(t, a, sa, "luna")
	m := manifestOf(t, data)
	m.Records = append(m.Records, Record{Kind: "closet.photo", ID: "luna-base--half1-h", Fields: photoOf("luna", "luna-base", "avatars/luna.png", "official", true)})
	body, _ := json.Marshal(&m)
	data = rewriteManifest(t, data, body)
	sb := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	res, err := importBytes(t.TempDir(), sb, data, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sb.recs["closet.photo/luna-base--half1-h"]; ok || !strings.Contains(strings.Join(res.RecordsSkipped, "\n"), "closet.photo/luna-base--half1-h：這個家的 closet 套件說這種紀錄不跟著角色走") {
		t.Fatalf("隱藏版的照片紀錄不收：%v", res.RecordsSkipped)
	}
	if _, ok := sb.recs["closet.photo/luna-base--half1"]; !ok {
		t.Error("一般版照收")
	}
}

// rewriteManifest 把匯出檔的 manifest.json 換成 body，其他檔照原樣。
func rewriteManifest(t *testing.T, data, body []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		w, _ := zw.Create(f.Name)
		if f.Name == manifestName {
			_, _ = w.Write(body)
			continue
		}
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		_, _ = w.Write(b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// 檔名看不出是誰的檔（衣櫃新上傳的 photos/<照片編號>.<ext>、升官方後的 avatars/photos/…，DEV-160 S3）照帶著它的紀錄認：
// 匯出記在 file_records、照帶；隱藏版照舊不帶。匯入照這個家的宣告驗那筆紀錄指著這個檔；對不上整個不收；紀錄沒收的檔也不收。
func TestExportCarriesPhotosFolderByRecord(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	writeFile(t, filepath.Join(a, "personas", "mine", "photos", "luna-my-3--u1.png"), "u1")
	writeFile(t, filepath.Join(a, "personas", "mine", "photos", "luna-my-3--u2.png"), "u2-hidden")
	writeFile(t, filepath.Join(a, "personas", "avatars", "photos", "luna-sea--u5.png"), "u5")
	sa.put(t, "closet.photo", "luna-my-3--u1", photoOf("luna", "luna-my-3", "photos/luna-my-3--u1.png", "mine", false))
	sa.put(t, "closet.photo", "luna-my-3--u2", photoOf("luna", "luna-my-3", "photos/luna-my-3--u2.png", "mine", true))
	sa.put(t, "closet.photo", "luna-sea--u5", photoOf("luna", "luna-sea", "avatars/photos/luna-sea--u5.png", "official", false))
	data := exportTo(t, a, sa, "luna")
	names := zipNames(t, data)
	for _, n := range []string{"extra/mine/photos/luna-my-3--u1.png", "avatars/photos/luna-sea--u5.png"} {
		if !slices.Contains(names, filesPrefix+n) {
			t.Fatalf("要帶 %s：%v", n, names)
		}
	}
	if slices.Contains(names, filesPrefix+"extra/mine/photos/luna-my-3--u2.png") {
		t.Fatal("隱藏版的照片不帶")
	}
	m := manifestOf(t, data)
	if m.FileRecords["extra/mine/photos/luna-my-3--u1.png"] != "closet.photo/luna-my-3--u1" || m.FileRecords["avatars/photos/luna-sea--u5.png"] != "closet.photo/luna-sea--u5" || len(m.FileRecords) != 2 {
		t.Fatalf("file_records：%v", m.FileRecords)
	}

	b := t.TempDir()
	sb := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	if _, err := importBytes(b, sb, data, false, false); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"mine/photos/luna-my-3--u1.png", "avatars/photos/luna-sea--u5.png"} {
		if !fileExists(filepath.Join(b, "personas", filepath.FromSlash(f))) {
			t.Errorf("匯入後要有 %s", f)
		}
	}
	if r := sb.recs["closet.photo/luna-my-3--u1"]; r.Fields["file"] != "photos/luna-my-3--u1.png" {
		t.Errorf("照片紀錄照原樣：%+v", r.Fields)
	}

	// 手改：檔掛到別的紀錄（那筆指著別的檔）→ 整個不收；拿掉 file_records → 認不出是誰的、整個不收。
	for name, change := range map[string]func(*Manifest){
		"不是紀錄": func(m *Manifest) {
			m.FileRecords["extra/mine/photos/luna-my-3--u1.png"] = "closet.photo/luna-sea--u5"
		},
		"不屬於檔案裡": func(m *Manifest) { delete(m.FileRecords, "extra/mine/photos/luna-my-3--u1.png") },
		"沒帶的檔":   func(m *Manifest) { m.FileRecords["extra/mine/photos/nope.png"] = "closet.photo/luna-sea--u5" },
	} {
		m := manifestOf(t, data)
		change(&m)
		body, _ := json.Marshal(&m)
		c := t.TempDir()
		if _, err := importBytes(c, &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}, rewriteManifest(t, data, body), false, false); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s：要整個不收：%v", name, err)
		}
		if fileExists(filepath.Join(c, "personas")) {
			t.Fatalf("%s：什麼都不寫", name)
		}
	}

	// 這個家沒有照片紀錄（closet 是舊版）：紀錄略過，檔名看不出是誰的檔也不收（記在略過的原因後面）；名字認得出來的照收。
	d := t.TempDir()
	res, err := importBytes(d, &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}, data, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(d, "personas", "mine", "photos", "luna-my-3--u1.png")) || fileExists(filepath.Join(d, "personas", "avatars", "photos")) {
		t.Fatal("紀錄沒收的檔不收")
	}
	if !strings.Contains(strings.Join(res.RecordsSkipped, "\n"), "closet.photo/luna-my-3--u1：closet 套件沒裝或停用（它指著的 personas/mine/photos/luna-my-3--u1.png 也不收）") {
		t.Fatalf("略過的原因：%v", res.RecordsSkipped)
	}
	if !fileExists(filepath.Join(d, "personas", "avatars", "luna.png")) {
		t.Error("名字認得出來的照收")
	}
}

// moveEntry 把匯出檔裡的一個檔搬到另一個邏輯路徑（manifest 的 files、file_records 跟著改，紀錄的 file 照 change 改），重包一份。
func moveEntry(t *testing.T, data []byte, from, to string, change func(*Manifest)) []byte {
	t.Helper()
	m := manifestOf(t, data)
	for i := range m.Files {
		if m.Files[i].Path == from {
			m.Files[i].Path = to
		}
	}
	if k, ok := m.FileRecords[from]; ok {
		delete(m.FileRecords, from)
		m.FileRecords[to] = k
	}
	if change != nil {
		change(&m)
	}
	body, _ := json.Marshal(&m)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		name := f.Name
		if name == filesPrefix+from {
			name = filesPrefix + to
		}
		w, _ := zw.Create(name)
		if f.Name == manifestName {
			_, _ = w.Write(body)
			continue
		}
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		_, _ = w.Write(b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pointTo 把匯出檔裡一筆紀錄的 file 改掉。
func pointTo(key, file string) func(*Manifest) {
	return func(m *Manifest) {
		for i := range m.Records {
			if m.Records[i].Key() == key {
				m.Records[i].Fields["file"] = file
			}
		}
	}
}

// 靠 file_records 認的檔不能拿來夾帶別的角色的東西（DEV-160 S3 審查、DEV-160 審查 M1／M6）：只收角色的圖的位置（頭像、照片、衣服資料夾）；
// 不能是這個家別筆紀錄的檔（--force 也不行）；檔名是收件家另一位角色的，那筆紀錄與檔都不收；本機那筆改過、指著別的檔的，帶來的檔不寫、算本機的留著。
// 匯出時檔名是這個家別的角色的紀錄檔不帶，那筆紀錄也不帶（列在略過）。
func TestImportRecordOnlyFilesStayWithTheirPersona(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	writeFile(t, filepath.Join(a, "personas", "mine", "photos", "luna-my-3--u1.png"), "u1")
	sa.put(t, "closet.photo", "luna-my-3--u1", photoOf("luna", "luna-my-3", "photos/luna-my-3--u1.png", "mine", false))
	writeFile(t, filepath.Join(a, "personas", "mine", "avatars", "athena@old.png"), "改過代號留下的舊圖")
	sa.put(t, "closet.photo", "luna-my-3--u2", photoOf("luna", "luna-my-3", "avatars/athena@old.png", "mine", false))
	var buf bytes.Buffer
	res, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"}, Store: sa}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	skipped := strings.Join(res.Skipped, "\n")
	if slices.Contains(zipNames(t, data), filesPrefix+"extra/mine/avatars/athena@old.png") ||
		!strings.Contains(skipped, "extra/mine/avatars/athena@old.png：檔名是別的角色（athena）的，不帶") ||
		!strings.Contains(skipped, "closet.photo/luna-my-3--u2：指著的 personas/mine/avatars/athena@old.png 沒帶") {
		t.Fatalf("檔名是這個家別的角色的不帶，紀錄也不帶：%v", res.Skipped)
	}
	if slices.ContainsFunc(manifestOf(t, data).Records, func(r Record) bool { return r.Key() == "closet.photo/luna-my-3--u2" }) {
		t.Fatal("檔沒帶的紀錄不匯出")
	}
	const from = "extra/mine/photos/luna-my-3--u1.png"

	// 手改成不是角色的圖的位置：整個不收。
	c := t.TempDir()
	bad := moveEntry(t, data, from, "extra/mine/athena@sea.png", pointTo("closet.photo/luna-my-3--u1", "athena@sea.png"))
	if _, err := importBytes(c, &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}, bad, false, true); err == nil || !strings.Contains(err.Error(), "不屬於檔案裡寫的角色") {
		t.Fatalf("不在角色的圖的位置的檔要整個不收：%v", err)
	}
	if fileExists(filepath.Join(c, "personas")) {
		t.Fatal("什麼都不寫")
	}

	// 手改成收件家另一位角色（athena）名字的檔：那筆紀錄與檔都不收，athena 的圖不動。
	e := t.TempDir()
	writeFile(t, filepath.Join(e, "personas", "athena.md"), "---\nschema: 1\nname: 雅典娜\n---\n")
	athenaPic := filepath.Join(e, "personas", "mine", "avatars", "athena@sea.png")
	writeFile(t, athenaPic, "athena 的圖")
	se := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	bad = moveEntry(t, data, from, "extra/mine/avatars/athena@sea.png", pointTo("closet.photo/luna-my-3--u1", "avatars/athena@sea.png"))
	foreign, err := importBytes(e, se, bad, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := se.recs["closet.photo/luna-my-3--u1"]; ok || readFile(t, athenaPic) != "athena 的圖" ||
		!strings.Contains(strings.Join(foreign.RecordsSkipped, "\n"), "closet.photo/luna-my-3--u1：指著的 personas/mine/avatars/athena@sea.png 照檔名是這個家另一位角色（athena）的，不收") {
		t.Fatalf("檔名是收件家別的角色的不收：%v", foreign.RecordsSkipped)
	}

	// 手改成這個家別筆紀錄（athena 的照片）的檔：--force 也整個不收，那張照片不動。
	b := t.TempDir()
	sb := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	theirs := filepath.Join(b, "personas", "mine", "photos", "athena-base--u4.png")
	writeFile(t, theirs, "athena 的")
	sb.put(t, "closet.photo", "athena-base--u4", photoOf("athena", "athena-base", "photos/athena-base--u4.png", "mine", false))
	bad = moveEntry(t, data, from, "extra/mine/photos/athena-base--u4.png", pointTo("closet.photo/luna-my-3--u1", "photos/athena-base--u4.png"))
	if _, err := importBytes(b, sb, bad, false, true); err == nil || !strings.Contains(err.Error(), "是這個家紀錄 closet.photo/athena-base--u4 的檔") {
		t.Fatalf("別筆紀錄的檔要整個不收：%v", err)
	}
	if readFile(t, theirs) != "athena 的" {
		t.Fatal("別的角色的照片不能被蓋")
	}

	// 本機那筆改過、指著別的檔：紀錄留著，帶來的檔也不寫。
	d := t.TempDir()
	sd := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	sd.put(t, "closet.photo", "luna-my-3--u1", photoOf("luna", "luna-my-3", "photos/luna-my-3--u1.webp", "mine", false))
	got, err := importBytes(d, sd, data, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.RecordsKept, "closet.photo/luna-my-3--u1") || !slices.Contains(got.Kept, "personas/mine/photos/luna-my-3--u1.png") {
		t.Fatalf("本機改過的留著：%v %v", got.RecordsKept, got.Kept)
	}
	if fileExists(filepath.Join(d, "personas", "mine", "photos", "luna-my-3--u1.png")) {
		t.Fatal("本機紀錄沒指著的檔不寫")
	}
}

// archivedStore 是連封存的紀錄一起列得出來的 memStore（personapack.ArchivedLister）。
type archivedStore struct{ *memStore }

func (s archivedStore) ListWithArchived(_ context.Context, kind string) ([]sdk.Record, error) {
	var out []sdk.Record
	for _, r := range s.recs {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out, nil
}

// exportLuna 匯出 luna，回匯出檔、結果、匯出檔裡的檔（files/ 後面的路徑）。
func exportLuna(t *testing.T, home string, s Store) ([]byte, *ExportResult, []string) {
	t.Helper()
	var buf bytes.Buffer
	res, err := Export(context.Background(), ExportOptions{Home: home, Personas: []string{"luna"}, Store: s}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, n := range zipNames(t, buf.Bytes()) {
		names = append(names, strings.TrimPrefix(n, filesPrefix))
	}
	return buf.Bytes(), res, names
}

func recordKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var keys []string
	for _, r := range manifestOf(t, data).Records {
		keys = append(keys, r.Key())
	}
	return keys
}

// 紀錄只能讓檔更不帶（DEV-160 審查 M1）：同一個檔有一筆隱藏版、一筆一般版的紀錄指著，或封存了的隱藏版紀錄指著，檔都不帶；
// 一般版那筆的檔沒帶，紀錄也不帶。
func TestExportSkipsFileAlsoPointedByHiddenRecord(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	writeFile(t, filepath.Join(a, "personas", "mine", "photos", "h.png"), "HIDDEN")
	writeFile(t, filepath.Join(a, "personas", "mine", "photos", "old.png"), "ARCHIVED HIDDEN")
	sa.put(t, "closet.photo", "luna-my-3--u1", photoOf("luna", "luna-my-3", "photos/h.png", "mine", true))
	sa.put(t, "closet.photo", "luna-my-3--u9", photoOf("luna", "luna-my-3", "photos/h.png", "mine", false))
	sa.put(t, "closet.photo", "luna-my-3--u2", photoOf("luna", "luna-my-3", "photos/old.png", "mine", true))
	r := sa.recs["closet.photo/luna-my-3--u2"]
	r.Archived = true
	sa.recs["closet.photo/luna-my-3--u2"] = r
	sa.put(t, "closet.photo", "luna-my-3--u8", photoOf("luna", "luna-my-3", "photos/old.png", "mine", false))
	data, res, names := exportLuna(t, a, archivedStore{sa})
	for _, bad := range []string{"extra/mine/photos/h.png", "extra/mine/photos/old.png"} {
		if slices.Contains(names, bad) {
			t.Errorf("隱藏版的紀錄也指著的檔不帶：%s", bad)
		}
	}
	keys := recordKeys(t, data)
	for _, bad := range []string{"closet.photo/luna-my-3--u9", "closet.photo/luna-my-3--u8"} {
		if slices.Contains(keys, bad) {
			t.Errorf("檔沒帶的紀錄不匯出：%s", bad)
		}
	}
	skipped := strings.Join(res.Skipped, "\n")
	for _, want := range []string{
		"extra/mine/photos/h.png：是不跟著角色走的紀錄 closet.photo/luna-my-3--u1 的圖，不帶",
		"extra/mine/photos/old.png：是不跟著角色走的紀錄 closet.photo/luna-my-3--u2 的圖，不帶",
		"closet.photo/luna-my-3--u9：指著的 personas/mine/photos/h.png 沒帶",
	} {
		if !strings.Contains(skipped, want) {
			t.Errorf("少了「%s」：\n%s", want, skipped)
		}
	}
}

// 別的角色的紀錄指著的檔不跟著這位走（DEV-160 審查 M1）：luna 的一般版紀錄指著 myhelper 的隱藏版照片（自己的照片資料夾）、
// 指著不匯出的 athena 的照片（avatars/photos/），都不帶；收件的家也就不會被寫進這些檔。
func TestExportSkipsFilesOfOtherPersonasRecords(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	writeFile(t, filepath.Join(a, "personas", "mine", "photos", "myhelper-base--u1.png"), "MYHELPER HIDDEN")
	sa.put(t, "closet.photo", "myhelper-base--u1", photoOf("myhelper", "myhelper-base", "photos/myhelper-base--u1.png", "mine", true))
	sa.put(t, "closet.photo", "luna-my-3--u9", photoOf("luna", "luna-my-3", "photos/myhelper-base--u1.png", "mine", false))
	writeFile(t, filepath.Join(a, "personas", "avatars", "photos", "athena-x.png"), "ATHENA")
	sa.put(t, "closet.photo", "athena-base--u1", photoOf("athena", "athena-base", "avatars/photos/athena-x.png", "official", false))
	sa.put(t, "closet.photo", "luna-sea--u9", photoOf("luna", "luna-sea", "avatars/photos/athena-x.png", "official", false))
	data, res, names := exportLuna(t, a, sa)
	for _, bad := range []string{"extra/mine/photos/myhelper-base--u1.png", "avatars/photos/athena-x.png"} {
		if slices.Contains(names, bad) {
			t.Errorf("別的角色的照片不帶：%s", bad)
		}
	}
	skipped := strings.Join(res.Skipped, "\n")
	for _, want := range []string{
		"extra/mine/photos/myhelper-base--u1.png：是別的角色（myhelper）的紀錄 closet.photo/myhelper-base--u1 的圖，不帶",
		"avatars/photos/athena-x.png：是別的角色（athena）的紀錄 closet.photo/athena-base--u1 的圖，不帶",
	} {
		if !strings.Contains(skipped, want) {
			t.Errorf("少了「%s」：\n%s", want, skipped)
		}
	}
	b := t.TempDir()
	if _, err := importBytes(b, &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}, data, false, false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"avatars/photos/athena-x.png", "mine/photos/myhelper-base--u1.png"} {
		if fileExists(filepath.Join(b, "personas", filepath.FromSlash(bad))) {
			t.Errorf("收件的家不該有 %s", bad)
		}
	}
}

// 紀錄指著的檔照樣擋舊的隱藏版位置（DEV-160 審查 M3）：level/ 底下、名字看起來是隱藏版的資料夾（mine/avatars/adult/）、
// 跟著資料夾走的紀錄指的資料夾（avatars/hidden/、mine/avatars/secret/）、不是角色的圖的位置（mine/adult/）的檔，一般版的紀錄指著也不帶；
// 照紀錄帶圖時跟著資料夾走的紀錄（隱藏版的設定 closet.variant）也不匯出。
func TestExportRecordFilesStillBlockHiddenDirs(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	writeFile(t, filepath.Join(a, "personas", "mine", "adult", "luna@sea.png"), "ADULT")
	writeFile(t, filepath.Join(a, "personas", "mine", "avatars", "adult", "luna@sea.png"), "ADULT 2")
	writeFile(t, filepath.Join(a, "personas", "mine", "avatars", "secret", "luna@sea.png"), "SECRET")
	writeFile(t, filepath.Join(a, "personas", "avatars", "level", "hidden", "luna@sea.png"), "LEVEL")
	sa.put(t, "closet.photo", "luna-sea--x1", photoOf("luna", "luna-sea", "adult/luna@sea.png", "mine", false))
	sa.put(t, "closet.photo", "luna-sea--x2", photoOf("luna", "luna-sea", "avatars/level/hidden/luna@sea.png", "official", false))
	sa.put(t, "closet.photo", "luna-sea--x3", photoOf("luna", "luna-sea", "avatars/hidden/luna@sea.png", "official", false))
	sa.put(t, "closet.photo", "luna-sea--x4", photoOf("luna", "luna-sea", "avatars/secret/luna@sea.png", "mine", false))
	sa.put(t, "closet.photo", "luna-sea--x5", photoOf("luna", "luna-sea", "avatars/adult/luna@sea.png", "mine", false))
	sa.put(t, "closet.variant", "adult", map[string]any{"name": "隱藏版", "dir": "mine/adult", "button": true, "on": true})
	sa.put(t, "closet.variant", "secret", map[string]any{"name": "祕密", "dir": "mine/avatars/Secret/"})
	data, res, names := exportLuna(t, a, sa)
	for _, n := range names {
		if strings.Contains(n, "adult/") || strings.Contains(n, "level/") || strings.Contains(n, "hidden/") || strings.Contains(n, "secret/") {
			t.Errorf("隱藏版位置的檔不帶：%s", n)
		}
	}
	for _, k := range recordKeys(t, data) {
		if strings.HasPrefix(k, "closet.variant/") || strings.HasPrefix(k, "closet.photo/luna-sea--x") {
			t.Errorf("不該匯出：%s", k)
		}
	}
	skipped := strings.Join(res.Skipped, "\n")
	for _, want := range []string{
		"closet.photo/luna-sea--x1：檔「extra/mine/adult/luna@sea.png」不在角色的圖的位置",
		"avatars/level/hidden/luna@sea.png：在看起來是隱藏版的資料夾裡，不帶",
		"avatars/hidden/luna@sea.png：在跟著資料夾走的紀錄指的資料夾 avatars/hidden/ 裡（例如隱藏版），不帶",
		"extra/mine/avatars/secret/luna@sea.png：在跟著資料夾走的紀錄指的資料夾 mine/avatars/secret/ 裡（例如隱藏版），不帶",
		"extra/mine/avatars/adult/luna@sea.png：在看起來是隱藏版的資料夾裡，不帶",
		"personas/mine/adult：跟著資料夾走的紀錄指的資料夾（例如隱藏版），照紀錄帶圖時紀錄與裡面的圖都不匯出",
	} {
		if !strings.Contains(skipped, want) {
			t.Errorf("少了「%s」：\n%s", want, skipped)
		}
	}
}

// macOS 的檔名不分大小寫（DEV-160 審查 M1）：隱藏版的紀錄寫 Plain.png、檔是 plain.png，照樣是隱藏版的檔，不帶。
func TestExportBlocksHiddenFileWithDifferentCase(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	writeFile(t, filepath.Join(a, "personas", "wardrobe", "luna", "plain.png"), "hidden-in-wardrobe")
	sa.put(t, "closet.photo", "luna-base--half1", photoOf("luna", "luna-base", "avatars/luna.png", "official", false))
	sa.put(t, "closet.photo", "luna-sea--half2-h", photoOf("luna", "luna-sea", "wardrobe/luna/Plain.png", "official", true))
	_, res, names := exportLuna(t, a, sa)
	if slices.Contains(names, "wardrobe/luna/plain.png") {
		t.Fatal("大小寫不同也是隱藏版的檔")
	}
	if !strings.Contains(strings.Join(res.Skipped, "\n"), "wardrobe/luna/plain.png：是不跟著角色走的紀錄 closet.photo/luna-sea--half2-h 的圖，不帶") {
		t.Errorf("要說為什麼沒帶：%v", res.Skipped)
	}
}

// 這個家有照片紀錄的種類、但這位角色一筆都還沒有（舊家還沒登記，DEV-160 審查 S7）：照檔名找圖，名字看起來是隱藏版的照樣不帶。
func TestExportFallsBackToNamesWithoutPhotoRecords(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	writeFile(t, filepath.Join(a, "personas", "avatars", "luna@adult.png"), "ADULT NAME")
	sa.put(t, "closet.photo", "athena-base--half1", photoOf("athena", "athena-base", "avatars/athena.png", "official", false))
	_, _, names := exportLuna(t, a, sa)
	var media []string
	for _, n := range names {
		if strings.Contains(n, "avatars/") {
			media = append(media, n)
		}
	}
	sort.Strings(media)
	if got := strings.Join(media, ","); got != "avatars/full/luna@sea.png,avatars/luna.mp4,avatars/luna.png,avatars/luna@sea.png,avatars/luna@sea~2.webp,extra/mine/avatars/luna@my-3.png" {
		t.Fatalf("照檔名找的圖：%s", got)
	}
}

// 照紀錄帶圖時，頭像資料夾裡沒有紀錄指著的圖不帶、列出來（DEV-160 審查 S7，跟建包一樣）。
func TestExportListsMediaWithoutRecords(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	sa.put(t, "closet.photo", "luna-base--half1", photoOf("luna", "luna-base", "avatars/luna.png", "official", false))
	sa.put(t, "closet.photo", "luna-sea--half1", photoOf("luna", "luna-sea", "avatars/luna@sea.png", "official", false))
	_, res, names := exportLuna(t, a, sa)
	if slices.Contains(names, "avatars/luna@sea~2.webp") || slices.Contains(names, "extra/mine/avatars/luna@my-3.png") {
		t.Fatalf("沒有紀錄指著的不帶：%v", names)
	}
	skipped := strings.Join(res.Skipped, "\n")
	for _, want := range []string{
		"avatars/luna@sea~2.webp：沒有紀錄指著這個檔（照紀錄帶圖），不帶",
		"extra/mine/avatars/luna@my-3.png：沒有紀錄指著這個檔（照紀錄帶圖），不帶",
		"avatars/full/luna@sea.png：沒有紀錄指著這個檔（照紀錄帶圖），不帶",
	} {
		if !strings.Contains(skipped, want) {
			t.Errorf("少了「%s」：\n%s", want, skipped)
		}
	}
	if strings.Contains(skipped, "avatars/luna@sea.png：") || strings.Contains(skipped, "luna.locked.png") {
		t.Errorf("帶了的、製作用的不列：\n%s", skipped)
	}
}

// 改過代號的角色（home tidy 把 myhelper 改成 my_myhelper，檔名不改，DEV-160 審查 M6）：照片紀錄指著的舊檔名的圖照帶（記在 file_records），
// 收件的家照紀錄放回原位；指不到檔的紀錄不匯出、列出來。
func TestExportAfterRecodeCarriesRecordFiles(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	mine := filepath.Join(a, "personas", "mine")
	if err := os.Rename(filepath.Join(mine, "myhelper.md"), filepath.Join(mine, "my_myhelper.md")); err != nil {
		t.Fatal(err)
	}
	sa.put(t, "closet.photo", "my_myhelper-base--half1", photoOf("my_myhelper", "my_myhelper-base", "avatars/myhelper.png", "mine", false))
	sa.put(t, "closet.photo", "my_myhelper-base--full1", photoOf("my_myhelper", "my_myhelper-base", "avatars/full/myhelper.png", "mine", false))
	sa.put(t, "closet.photo", "my_myhelper-base--half2", photoOf("my_myhelper", "my_myhelper-base", "avatars/myhelper@gone.png", "mine", false))
	var buf bytes.Buffer
	res, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"my_myhelper"}, Store: sa}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	names := zipNames(t, data)
	for _, want := range []string{"extra/mine/avatars/myhelper.png", "extra/mine/avatars/full/myhelper.png"} {
		if !slices.Contains(names, filesPrefix+want) {
			t.Errorf("改過代號，紀錄指著的舊檔名照帶：%s（%v）", want, names)
		}
	}
	m := manifestOf(t, data)
	if m.FileRecords["extra/mine/avatars/myhelper.png"] != "closet.photo/my_myhelper-base--half1" || m.FileRecords["extra/mine/avatars/full/myhelper.png"] != "closet.photo/my_myhelper-base--full1" {
		t.Errorf("file_records：%v", m.FileRecords)
	}
	keys := recordKeys(t, data)
	if !slices.Contains(keys, "closet.photo/my_myhelper-base--half1") || slices.Contains(keys, "closet.photo/my_myhelper-base--half2") {
		t.Errorf("匯出的紀錄：%v", keys)
	}
	if !strings.Contains(strings.Join(res.Skipped, "\n"), "closet.photo/my_myhelper-base--half2：指著的 personas/mine/avatars/myhelper@gone.png 沒帶") {
		t.Errorf("指不到檔的紀錄要列出來：%v", res.Skipped)
	}
	b := t.TempDir()
	sb := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	if _, err := importBytes(b, sb, data, false, false); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(b, "personas", "mine", "avatars", "myhelper.png")) || sb.recs["closet.photo/my_myhelper-base--half1"].Fields["file"] != "avatars/myhelper.png" {
		t.Error("收件的家照紀錄放回原位")
	}
}

// 手改的匯出檔多一筆沒帶檔的紀錄（DEV-160 審查 M1）：指著收件家別筆紀錄（主人的隱藏版）的檔、或收件家已經有的檔，那筆不收、列出來，
// 檔不動；其他照收。
func TestImportSkipsRecordPointingAtLocalFile(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	sa.put(t, "closet.photo", "luna-base--half1", photoOf("luna", "luna-base", "avatars/luna.png", "official", false))
	data := exportTo(t, a, sa, "luna")
	m := manifestOf(t, data)
	m.Records = append(m.Records,
		Record{Kind: "closet.photo", ID: "luna-base--x9", Fields: photoOf("luna", "luna-base", "photos/owner-hidden.png", "mine", false)},
		Record{Kind: "closet.photo", ID: "luna-base--x8", Fields: photoOf("luna", "luna-base", "photos/owner-plain.png", "mine", false)})
	body, _ := json.Marshal(&m)
	data = rewriteManifest(t, data, body)
	b := t.TempDir()
	hidden := filepath.Join(b, "personas", "mine", "photos", "owner-hidden.png")
	writeFile(t, hidden, "OWNER HIDDEN")
	writeFile(t, filepath.Join(b, "personas", "mine", "photos", "owner-plain.png"), "OWNER PLAIN")
	sb := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	sb.put(t, "closet.photo", "my_x-base--h1", photoOf("my_x", "my_x-base", "photos/owner-hidden.png", "mine", true))
	res, err := importBytes(b, sb, data, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"closet.photo/luna-base--x9", "closet.photo/luna-base--x8"} {
		if _, ok := sb.recs[k]; ok {
			t.Errorf("指著收件家的檔的紀錄不收：%s", k)
		}
	}
	skipped := strings.Join(res.RecordsSkipped, "\n")
	for _, want := range []string{
		"closet.photo/luna-base--x9：指著的 personas/mine/photos/owner-hidden.png 是別的角色（my_x）的紀錄 closet.photo/my_x-base--h1 的圖，不收",
		"closet.photo/luna-base--x8：指著這個家已經有的 personas/mine/photos/owner-plain.png（檔案裡沒帶這個檔），不收",
	} {
		if !strings.Contains(skipped, want) {
			t.Errorf("少了「%s」：\n%s", want, skipped)
		}
	}
	if _, ok := sb.recs["closet.photo/luna-base--half1"]; !ok || readFile(t, hidden) != "OWNER HIDDEN" {
		t.Error("其他照收、檔不動")
	}
}

// unreadableStore 是有套件讀不到（停用、被拒）的 memStore（personapack.UnreadableLister）。
type unreadableStore struct {
	*memStore
	bad []personapack.UnreadablePackage
}

func (s unreadableStore) Unreadable(context.Context) ([]personapack.UnreadablePackage, error) {
	return s.bad, nil
}

// 照紀錄決定帶哪些圖的套件（宣告 file_field）停用或讀不懂時不匯出（DEV-160 審查 M2）：讀不到照片紀錄就不知道哪些圖是隱藏版，
// 照檔名找會把只靠紀錄標成隱藏版的圖帶出去。沒有這種宣告的套件停用不影響。
func TestExportRefusesWhenPhotoPackageUnreadable(t *testing.T) {
	a, sa := homeA(t)
	for name, bad := range map[string]personapack.UnreadablePackage{
		"closet（停用中；closet.photo）":   {ID: "closet", Why: "停用中", Exports: []string{"closet.outfit", "closet.photo"}, Files: []string{"closet.photo"}},
		"mine-looks（被拒：讀不懂，讀不懂它的設定）": {ID: "mine-looks", Why: "被拒：讀不懂", Unknown: true},
	} {
		var buf bytes.Buffer
		_, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"}, Store: unreadableStore{sa, []personapack.UnreadablePackage{bad}}}, &buf)
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "沒有匯出") {
			t.Errorf("%s：%v", name, err)
		}
	}
	var buf bytes.Buffer
	if _, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"},
		Store: unreadableStore{sa, []personapack.UnreadablePackage{{ID: "notes", Why: "停用中", Exports: []string{"notes.page"}}}}}, &buf); err != nil {
		t.Errorf("沒有照紀錄帶圖的套件停用：照常匯出：%v", err)
	}
}

// 手改的匯出檔帶一個假的檔放在收件家已經有的位置（收件家那個檔沒有紀錄，例如紀錄刪了），讓自己的紀錄指著它：
// 收件家的檔不蓋（本機的留著），指著它的那筆紀錄也不收；之後收件家匯出這位角色，自己的那個檔也不會跟著出去。
func TestImportDropsRecordWhoseFileStaysLocal(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	writeFile(t, filepath.Join(a, "personas", "mine", "photos", "luna-my-3--u1.png"), "sender dummy")
	sa.put(t, "closet.photo", "luna-my-3--u1", photoOf("luna", "luna-my-3", "photos/luna-my-3--u1.png", "mine", false))
	data := exportTo(t, a, sa, "luna")
	crafted := moveEntry(t, data, "extra/mine/photos/luna-my-3--u1.png", "extra/mine/photos/secret.png", pointTo("closet.photo/luna-my-3--u1", "photos/secret.png"))

	b := t.TempDir()
	writeFile(t, filepath.Join(b, "personas", "luna.md"), "---\nschema: 1\nname: 露娜\n---\n")
	secret := filepath.Join(b, "personas", "mine", "photos", "secret.png")
	writeFile(t, secret, "RECEIVER PRIVATE")
	sb := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	res, err := importBytes(b, sb, crafted, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, secret) != "RECEIVER PRIVATE" || !slices.Contains(res.Kept, "personas/mine/photos/secret.png") {
		t.Fatalf("收件家的檔要留著、列在 kept：%v", res.Kept)
	}
	if _, ok := sb.recs["closet.photo/luna-my-3--u1"]; ok || slices.Contains(res.RecordsAdded, "closet.photo/luna-my-3--u1") {
		t.Fatalf("指著留著的檔的紀錄不能收：%v", res.RecordsAdded)
	}
	want := "closet.photo/luna-my-3--u1：指著的 personas/mine/photos/secret.png 這個家已經有、內容不一樣（沒換），紀錄也不收"
	if !slices.Contains(res.RecordsSkipped, want) {
		t.Fatalf("少了「%s」：%v", want, res.RecordsSkipped)
	}
	var buf bytes.Buffer
	if _, err := Export(context.Background(), ExportOptions{Home: b, Personas: []string{"luna"}, Store: sb}, &buf); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(zipNames(t, buf.Bytes()), filesPrefix+"extra/mine/photos/secret.png") {
		t.Fatal("收件家自己的檔被匯出了")
	}
}

// 一般的匯出檔帶 avatars/luna@sea.png 與指著它的一般照片紀錄，收件家同一個位置是自己的隱藏版（隱藏版的紀錄指著）：
// 檔不寫、--force 也不蓋，指著它的紀錄也不收，兩件都列出原因。
func TestImportNeverTouchesReceiverHiddenPhoto(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	sa.put(t, "closet.photo", "luna-sea--half1", photoOf("luna", "luna-sea", "avatars/luna@sea.png", "official", false))
	data := exportTo(t, a, sa, "luna")
	for _, force := range []bool{false, true} {
		b := t.TempDir()
		writeFile(t, filepath.Join(b, "personas", "luna.md"), "---\nschema: 1\nname: 露娜\n---\n")
		hidden := filepath.Join(b, "personas", "avatars", "luna@sea.png")
		writeFile(t, hidden, "RECEIVER HIDDEN")
		sb := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
		sb.put(t, "closet.photo", "luna-sea--half1-h", photoOf("luna", "luna-sea", "avatars/luna@sea.png", "official", true))
		res, err := importBytes(b, sb, data, false, force)
		if err != nil {
			t.Fatal(err)
		}
		if readFile(t, hidden) != "RECEIVER HIDDEN" {
			t.Fatalf("force=%v：收件家的隱藏版被蓋掉了", force)
		}
		if _, ok := sb.recs["closet.photo/luna-sea--half1"]; ok {
			t.Fatalf("force=%v：一般的照片紀錄指著收件家的隱藏版", force)
		}
		for _, want := range []string{
			"closet.photo/luna-sea--half1：指著的 personas/avatars/luna@sea.png 是不跟著角色走的紀錄 closet.photo/luna-sea--half1-h 的圖，不收",
			"personas/avatars/luna@sea.png：是不跟著角色走的紀錄 closet.photo/luna-sea--half1-h 的圖，帶來的檔不寫（不蓋這個家的檔）",
		} {
			if !slices.Contains(res.RecordsSkipped, want) {
				t.Fatalf("force=%v 少了「%s」：%v", force, want, res.RecordsSkipped)
			}
		}
	}
}

// 紀錄指著頭像資料夾裡的說明檔、設定（不是圖或影片）：匯出不帶、列原因；手改的匯出檔硬塞（file_records 指著）整個不收。
func TestExportCarriesOnlyMediaByRecord(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	writeFile(t, filepath.Join(a, "personas", "avatars", "notes.txt"), "PRIVATE NOTES")
	writeFile(t, filepath.Join(a, "personas", "avatars", "luna.live.json"), "{\"live\":1}")
	sa.put(t, "closet.photo", "luna-base--half1", photoOf("luna", "luna-base", "avatars/luna.png", "official", false))
	sa.put(t, "closet.photo", "luna-base--x1", photoOf("luna", "luna-base", "avatars/notes.txt", "official", false))
	sa.put(t, "closet.photo", "luna-base--x2", photoOf("luna", "luna-base", "avatars/luna.live.json", "official", false))
	data, res, names := exportLuna(t, a, sa)
	for _, n := range []string{"avatars/notes.txt", "avatars/luna.live.json"} {
		if slices.Contains(names, n) {
			t.Fatalf("%s 不是圖或影片，不能帶：%v", n, names)
		}
		if !slices.Contains(res.Skipped, n+"：紀錄指著的不是圖或影片，不帶") {
			t.Fatalf("少了 %s 的原因：%v", n, res.Skipped)
		}
	}
	// 手改：把頭像換成說明檔、file_records 指著那筆紀錄
	crafted := moveEntry(t, data, "avatars/luna.png", "avatars/notes.txt", func(m *Manifest) {
		if m.FileRecords == nil {
			m.FileRecords = map[string]string{}
		}
		m.FileRecords["avatars/notes.txt"] = "closet.photo/luna-base--half1"
		pointTo("closet.photo/luna-base--half1", "avatars/notes.txt")(m)
	})
	b := t.TempDir()
	if _, err := importBytes(b, &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}, crafted, false, false); err == nil || !strings.Contains(err.Error(), "不屬於檔案裡寫的角色") {
		t.Fatalf("要整個不收：%v", err)
	}
	if fileExists(filepath.Join(b, "personas", "avatars", "notes.txt")) {
		t.Fatal("說明檔寫進收件家了")
	}
}

// 紀錄的 file 跟照名字收的頭像只差大小寫（avatars/Luna.png 與 avatars/luna.png，macOS 是同一個檔）：照樣匯出，那筆紀錄跟著走，
// file_records 不會列一個匯出檔裡沒有的寫法。
func TestExportRecordFileDifferingOnlyInCase(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	sa.put(t, "closet.photo", "luna-base--half1", photoOf("luna", "luna-base", "avatars/Luna.png", "official", false))
	var buf bytes.Buffer
	res, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"}, Store: sa}, &buf)
	if err != nil {
		t.Fatalf("要能匯出：%v", err)
	}
	if res.Records == 0 || !slices.Contains(zipNames(t, buf.Bytes()), filesPrefix+"avatars/luna.png") {
		t.Fatalf("頭像與紀錄都要帶：%+v %v", res, zipNames(t, buf.Bytes()))
	}
	b := t.TempDir()
	sb := &memStore{kinds: photoKinds(), recs: map[string]sdk.Record{}}
	if _, err := importBytes(b, sb, buf.Bytes(), false, false); err != nil {
		t.Fatalf("要能匯入：%v", err)
	}
	if _, ok := sb.recs["closet.photo/luna-base--half1"]; !ok {
		t.Fatal("照片紀錄要匯入")
	}
}

// 家裡有一筆照片紀錄的檔服務讀不懂（vault 不收、列不出來；例如手改壞的隱藏版）：不知道它標著哪些圖不能帶，不匯出、說是哪個檔。
func TestExportRefusesWithUnreadablePhotoRecord(t *testing.T) {
	a, sa := homeA(t)
	sa.kinds = photoKinds()
	sa.put(t, "closet.photo", "luna-base--half1", photoOf("luna", "luna-base", "avatars/luna.png", "official", false))
	writeFile(t, filepath.Join(a, "vault", "pkg", "closet", "photo", "luna-sea--half1.md"), "---\nhidden: [壞掉\n---\n")
	var buf bytes.Buffer
	_, err := Export(context.Background(), ExportOptions{Home: a, Personas: []string{"luna"}, Store: sa}, &buf)
	if err == nil || !strings.Contains(err.Error(), "vault/pkg/closet/photo/luna-sea--half1.md") || !strings.Contains(err.Error(), "沒有匯出") {
		t.Fatalf("要拒絕並說是哪個檔：%v", err)
	}
}
