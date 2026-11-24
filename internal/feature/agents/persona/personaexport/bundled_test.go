package personaexport

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// bundledDir 建一個「跟著程式的公開卡」資料夾（PRS-25），放一張 kaguya.md（內容 text）。
func bundledDir(t *testing.T, text string) string {
	t.Helper()
	d := t.TempDir()
	writeFile(t, filepath.Join(d, "kaguya.md"), "---\nschema: 1\nname: 輝夜\n---\n"+text)
	return d
}

func importBundled(home, bundled string, data []byte, dry, force bool) (*ImportResult, error) {
	s := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	o := ImportOptions{Home: home, Store: s, DryRun: dry, Force: force, Now: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC), Bundled: bundled}
	return Import(context.Background(), o, bytes.NewReader(data), int64(len(data)))
}

// exportBundledKaguya：官方角色資料夾是空的新家，只靠跟著程式的卡匯出 kaguya。
func exportBundledKaguya(t *testing.T, bundled string) []byte {
	t.Helper()
	var buf bytes.Buffer
	s := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	if _, err := Export(context.Background(), ExportOptions{Home: t.TempDir(), Personas: []string{"kaguya"}, Store: s, Bundled: bundled}, &buf); err != nil {
		t.Fatalf("只有跟著程式的卡也要匯得出來：%v", err)
	}
	return buf.Bytes()
}

// TestExportBundledCard：匯出找官方卡走共用的入口（sdk.OfficialCardIn，PRS-25）——新裝的家官方角色資料夾是空的，
// 只有跟著程式的卡也匯得出來（卡放在 cards/）；沒給跟著程式的資料夾就只看官方位置，說找不到。
// Guards: PRS-25
func TestExportBundledCard(t *testing.T) {
	s := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	var buf bytes.Buffer
	if _, err := Export(context.Background(), ExportOptions{Home: t.TempDir(), Personas: []string{"kaguya"}, Store: s}, &buf); err == nil || !strings.Contains(err.Error(), "找不到角色「kaguya」的卡") {
		t.Fatalf("沒給跟著程式的資料夾＝只看官方位置，要說找不到：%v", err)
	}
	data := exportBundledKaguya(t, bundledDir(t, "程式帶的\n"))
	if names := zipNames(t, data); !slices.Contains(names, filesPrefix+"cards/kaguya.md") {
		t.Fatalf("卡要放在 cards/：%v", names)
	}
}

// TestImportBundledCardNotCopied：匯入的官方卡跟這一版程式帶的那張一樣時，不抄一份到官方位置（抄了之後換版會蓋住新版的卡）；
// 內容不一樣照「本機已經有、內容不同」不換，--force 才寫進官方位置；程式沒帶這位（舊版程式）照舊新增。
// Guards: PRS-25
func TestImportBundledCardNotCopied(t *testing.T) {
	data := exportBundledKaguya(t, bundledDir(t, "程式帶的\n"))
	card := func(home string) string { return filepath.Join(home, "personas", "kaguya.md") }

	b := t.TempDir()
	res, err := importBundled(b, bundledDir(t, "程式帶的\n"), data, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(card(b)) || slices.Contains(res.Added, "personas/kaguya.md") || res.Unchanged == 0 {
		t.Fatalf("跟程式帶的一樣：算一樣、不寫進官方位置：%+v", res)
	}
	if reg, err := os.ReadFile(filepath.Join(b, "settings", "persona-import.toml")); err == nil && strings.Contains(string(reg), "kaguya.md") {
		t.Fatalf("沒寫的檔不登記：\n%s", reg)
	}

	c, other := t.TempDir(), bundledDir(t, "另一版程式帶的\n")
	res, err = importBundled(c, other, data, true, false)
	if err != nil || !slices.Contains(res.Kept, "personas/kaguya.md") || len(res.Added) != 0 {
		t.Fatalf("跟程式帶的不一樣：預設不換（列在沒換的）：%+v %v", res, err)
	}
	if res, err = importBundled(c, other, data, false, true); err != nil || !slices.Contains(res.Added, "personas/kaguya.md") || !fileExists(card(c)) {
		t.Fatalf("--force 才寫進官方位置：%+v %v", res, err)
	}

	d := t.TempDir()
	if res, err = importBundled(d, "", data, false, false); err != nil || !slices.Contains(res.Added, "personas/kaguya.md") || !fileExists(card(d)) {
		t.Fatalf("程式沒帶這位：照舊新增：%+v %v", res, err)
	}
}

// TestImportRefusesMineSlugOfBundledCard：匯入「我的」角色時，這一版程式帶著同代號的公開卡（這個家沒有同代號的我的卡）也擋——
// 我的卡會蓋過程式帶的那張（PRS-27），匯進來會悄悄換掉這個家現在用的那位（坐她的秘書、記在她名下的紀錄跟著換人）；
// 白話說是程式帶的公開角色、要在原本的家改成 my_ 開頭；沒有同代號的照常。
// Guards: PRS-25
func TestImportRefusesMineSlugOfBundledCard(t *testing.T) {
	h, sh := homeA(t)
	data := exportTo(t, h, sh, "myhelper") // 我的角色
	bb := t.TempDir()
	writeFile(t, filepath.Join(bb, "myhelper.md"), "---\nschema: 1\nname: 程式帶的\n---\n")
	b := t.TempDir()
	_, err := importBundled(b, bb, data, true, false)
	if err == nil || !strings.Contains(err.Error(), "程式帶著的公開角色") || !strings.Contains(err.Error(), "my_myhelper") || strings.Contains(err.Error(), "personas/myhelper.md") {
		t.Fatalf("跟程式帶的卡同代號要擋、說清楚是程式帶的：%v", err)
	}
	if _, err := importBundled(b, "", data, true, false); err != nil {
		t.Fatalf("沒有同代號的卡就照常（預覽）：%v", err)
	}
}

// TestExportMineCardOverBundledCard：這個家有同代號的我的卡（舊的、不是 my_ 開頭）蓋過跟著程式的公開卡時（PRS-27），
// 匯出的是主人的那張（extra/mine/），跟角色庫用的同一張；跟著程式的那張不帶。
func TestExportMineCardOverBundledCard(t *testing.T) {
	h := t.TempDir()
	writeFile(t, filepath.Join(h, "personas", "mine", "kaguya.md"), "---\nschema: 1\nname: 我的輝夜\n---\n主人自己的\n")
	var buf bytes.Buffer
	s := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	if _, err := Export(context.Background(), ExportOptions{Home: h, Personas: []string{"kaguya"}, Store: s, Bundled: bundledDir(t, "程式帶的\n")}, &buf); err != nil {
		t.Fatal(err)
	}
	names := zipNames(t, buf.Bytes())
	if !slices.Contains(names, filesPrefix+"extra/mine/kaguya.md") || slices.Contains(names, filesPrefix+"cards/kaguya.md") {
		t.Fatalf("要匯出主人的那張：%v", names)
	}
}

// TestImportMineCardWhenHomeMineBeatsBundled：這個家已經有同代號的我的卡（蓋過跟著程式的公開卡，PRS-27）時，匯入「我的」那位是同一個位置，
// 照一般規則收（內容不同不蓋、--force 才蓋）；匯入官方卡（cards/）撞到這張我的卡照舊擋——我的卡是捷徑（壞掉的也算）一樣擋，
// 跟角色庫同一個判斷（sdk.HasMineCard），不然官方卡寫進官方位置就悄悄蓋過主人的那張。
func TestImportMineCardWhenHomeMineBeatsBundled(t *testing.T) {
	h, sh := homeA(t)
	data := exportTo(t, h, sh, "myhelper") // 我的角色
	bb := t.TempDir()
	writeFile(t, filepath.Join(bb, "myhelper.md"), "---\nschema: 1\nname: 程式帶的\n---\n")
	b := t.TempDir()
	mine := filepath.Join(b, "personas", "mine", "myhelper.md")
	writeFile(t, mine, "---\nschema: 1\nname: 這個家原本的小幫手\n---\n")
	res, err := importBundled(b, bb, data, false, false)
	if err != nil {
		t.Fatalf("這個家的 myhelper 是主人的（蓋過程式帶的），匯入「我的」那位要收：%v", err)
	}
	if !slices.Contains(res.Kept, "personas/mine/myhelper.md") || !strings.Contains(readFile(t, mine), "這個家原本的小幫手") {
		t.Fatalf("內容不同的照一般規則不蓋：%+v", res)
	}
	if res, err = importBundled(b, bb, data, false, true); err != nil || !slices.Contains(res.Replaced, "personas/mine/myhelper.md") || !strings.Contains(readFile(t, mine), "小幫手") || strings.Contains(readFile(t, mine), "原本的") {
		t.Fatalf("--force 才蓋：%+v %v", res, err)
	}

	official := exportBundledKaguya(t, bundledDir(t, "程式帶的\n"))
	c := t.TempDir()
	writeFile(t, filepath.Join(c, "personas", "mine", "kaguya.md"), "---\nschema: 1\nname: 我的輝夜\n---\n")
	if _, err := importBundled(c, bundledDir(t, "程式帶的\n"), official, true, false); err == nil || !strings.Contains(err.Error(), "personas/mine/kaguya.md") {
		t.Fatalf("匯入官方卡撞到這個家的我的卡照舊擋：%v", err)
	}
	link := t.TempDir()
	if err := os.MkdirAll(filepath.Join(link, "personas", "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(link, "不存在.md"), filepath.Join(link, "personas", "mine", "kaguya.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := importBundled(link, bundledDir(t, "程式帶的\n"), official, true, false); err == nil || !strings.Contains(err.Error(), "personas/mine/kaguya.md") {
		t.Fatalf("我的卡是捷徑也算這個家的那位，匯入官方卡要擋：%v", err)
	}
}
