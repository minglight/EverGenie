package persona

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 看板匯出、匯入放在 runtime/ 的暫存（PRS-18）：放超過一小時沒人拿的，由定時工作 personas.transfer_sweep（每 10 分鐘）
// 與每次匯出、匯入前清掉，剛放的不動（跟檔案修改時間比，用牆上時鐘；模組的假時鐘再怎麼走都不會清掉剛放的）。
func TestTransfersSweepOldOnly(t *testing.T) {
	_, lib, h, _ := fixture(t)
	setup, check := sweepCase(t, h.Dir)
	setup()
	var job func(context.Context) error
	for _, j := range lib.Jobs() {
		if j.Name == "personas.transfer_sweep" && j.Every == 10*time.Minute {
			job = j.Run
		}
	}
	if job == nil {
		t.Fatal("要登記 personas.transfer_sweep，每 10 分鐘")
	}
	must(t, job(context.Background()))
	check()

	setup()
	// 匯入一個找不到的檔：照樣先清。
	if _, err := lib.ImportUpload(context.Background(), "20261104-130000-0a1b2c3d.hgpersona", true, false, "owner(web)"); err == nil {
		t.Fatal("找不到的上傳檔要報錯")
	}
	check()
}

func sweepCase(t *testing.T, home string) (setup, check func()) {
	old := time.Now().Add(-2 * time.Hour)
	h := struct{ Dir string }{home}
	exportOld := filepath.Join(platform.PersonaExportDir(h.Dir), "20261104-100000-0a1b2c3d")
	exportNew := filepath.Join(platform.PersonaExportDir(h.Dir), "20261104-120000-0a1b2c3d")
	upOld := filepath.Join(platform.PersonaUploadDir(h.Dir), "20261104-100000-0a1b2c3d.hgpersona")
	upNew := filepath.Join(platform.PersonaUploadDir(h.Dir), "20261104-120000-0a1b2c3d.hgpersona")
	return func() {
			for _, d := range []string{exportOld, exportNew, platform.PersonaUploadDir(h.Dir)} {
				must(t, os.MkdirAll(d, 0o700))
			}
			for _, f := range []string{filepath.Join(exportOld, "a.hgpersona"), filepath.Join(exportNew, "a.hgpersona"), upOld, upNew} {
				must(t, os.WriteFile(f, []byte("x"), 0o600))
			}
			for _, p := range []string{exportOld, upOld} {
				must(t, os.Chtimes(p, old, old))
			}
		}, func() {
			t.Helper()
			for p, want := range map[string]bool{exportOld: false, upOld: false, exportNew: true, upNew: true} {
				if _, err := os.Stat(p); (err == nil) != want {
					t.Errorf("%s 存在＝%v，要 %v", p, err == nil, want)
				}
			}
		}
}

// 下載的檔名照角色的名字（一位）、「-」接起來（兩三位）、「第一位等 N 位」（更多）；名字只剩符號時用代號；不會有 `/` 或 `.` 開頭。
func TestExportFilename(t *testing.T) {
	_, lib, h, _ := fixture(t)
	must(t, os.WriteFile(filepath.Join(h.Dir, "personas", "dots.md"), []byte("---\nname: \"../..\"\n---\n人設\n"), 0o600))
	ctx := context.Background()
	for _, c := range []struct {
		slugs []string
		want  string
	}{
		{[]string{"athena"}, "Athena.hgpersona"},
		{[]string{"athena", "new", "athena"}, "Athena-New.hgpersona"},
		{[]string{"dots"}, "dots.hgpersona"},
		{[]string{"athena", "new", "dots", "nobody"}, "Athena等4位.hgpersona"},
		{[]string{" "}, "persona.hgpersona"},
	} {
		if got := lib.exportFilename(ctx, c.slugs); got != c.want {
			t.Errorf("%v → %q，要 %q", c.slugs, got, c.want)
		}
	}
}

// TestBoardTransferBundledCard：看板的匯出、匯入（ExportForBoard、ImportUpload）也接上跟著程式的公開卡（PRS-25）——
// 官方資料夾沒有、只有程式帶的那張也匯得出來；匯入跟程式帶的一樣的卡算「一樣」，不抄一份到官方位置。
// Guards: PRS-25
func TestBoardTransferBundledCard(t *testing.T) {
	_, lib, h, _ := fixture(t)
	ctx := context.Background()
	bundled := t.TempDir()
	must(t, os.WriteFile(filepath.Join(bundled, "kaguya.md"), []byte("+++\nname = \"輝夜\"\n+++\n程式帶的\n"), 0o444))
	lib.SetBundledCards(func(string) string { return bundled })

	res, err := lib.ExportForBoard(ctx, []string{"kaguya"})
	must(t, err)
	token, _, _ := strings.Cut(strings.TrimPrefix(res.Download, "/persona-export/"), "/")
	data, err := os.ReadFile(filepath.Join(platform.PersonaExportDir(h.Dir), token, res.Filename))
	must(t, err)
	upload := "20261113-120000-0a1b2c3d" + platform.PersonaTransferExt
	must(t, os.MkdirAll(platform.PersonaUploadDir(h.Dir), 0o700))
	must(t, os.WriteFile(filepath.Join(platform.PersonaUploadDir(h.Dir), upload), data, 0o600))

	imp, err := lib.ImportUpload(ctx, upload, false, false, "owner(web)")
	must(t, err)
	if slices.Contains(imp.Added, "personas/kaguya.md") || imp.Unchanged == 0 {
		t.Fatalf("跟程式帶的一樣：算一樣、不寫：%+v", imp)
	}
	if _, err := os.Stat(filepath.Join(platform.OfficialPersonasDir(h.Dir), "kaguya.md")); !os.IsNotExist(err) {
		t.Fatalf("官方位置不該多一份：%v", err)
	}
}
