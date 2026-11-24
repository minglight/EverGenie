package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func boardJSON(t *testing.T, h http.Handler, path, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func boardPersonaUpload(t *testing.T, h http.Handler, file []byte) string {
	t.Helper()
	up := httptest.NewRequest(http.MethodPost, "/api/web/persona-upload", bytes.NewReader(file))
	up.Header.Set("X-Requested-With", "evergenie")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, up)
	var got struct{ Name string }
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Name == "" {
		t.Fatalf("上傳：%d %s", w.Code, w.Body.String())
	}
	return got.Name
}

func listLen(v any) int {
	xs, _ := v.([]any)
	return len(xs)
}

// 看板的匯出→匯入來回（DEV-116 S3、PRS-17～19、WEB-151）：A 經看板 personas.export 做檔、GET 下載一次就刪；
// B 把檔 POST 到 /api/web/persona-upload、personas.import 先 dry_run（什麼都不寫）再真的匯入（紀錄記在 owner(web) 名下、上傳的檔刪掉）。
// 回的摘要跟指令 --json 一樣；秘書經 API 呼叫被擋。
func TestPersonasExportImportThroughBoard(t *testing.T) {
	_, sockA, boardA := startFixtureWebWith(t, func(dir string) {
		dressPackage(t, dir)
		for _, f := range []string{"avatars/helper.png", "avatars/full/helper.png", "avatars/builder.png"} {
			putFile(t, filepath.Join(dir, "personas", filepath.FromSlash(f)), "圖 "+f)
		}
	})
	hgOK(t, sockA, "data", "add", "dress.outfit", "--id", "helper-sea", "--json", `{"persona":"helper","outfit":"sea","name":"海邊","cost":1,"state":"active","source":"official"}`)

	code, exp := boardJSON(t, boardA, "/api/personas/export", `{"personas":["helper"]}`)
	if code != http.StatusOK || exp["files"] != float64(3) || exp["records"] != float64(1) {
		t.Fatalf("export：%d %v", code, exp)
	}
	name, _ := exp["filename"].(string)
	download, _ := exp["download"].(string)
	if !strings.HasSuffix(name, ".hgpersona") || !strings.HasPrefix(download, "/persona-export/") {
		t.Fatalf("檔名與下載網址：%q %q", name, download)
	}
	w := httptest.NewRecorder()
	boardA.ServeHTTP(w, httptest.NewRequest(http.MethodGet, download, nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("下載：%d %v", w.Code, w.Header())
	}
	file := w.Body.Bytes()
	w = httptest.NewRecorder()
	boardA.ServeHTTP(w, httptest.NewRequest(http.MethodGet, download, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("下載一次就刪：%d", w.Code)
	}

	homeB, sockB, boardB := startFixtureWebWith(t, func(dir string) { dressPackage(t, dir) })
	_ = os.Remove(filepath.Join(homeB, "personas", "helper.md"))
	uploaded := boardPersonaUpload(t, boardB, file)
	// 看板存的位置就是角色庫找的位置（底層不 import 業務模組，兩邊各寫一次）。
	uploadedAt := filepath.Join(platform.PersonaUploadDir(homeB), uploaded)
	if !fileExists(uploadedAt) {
		t.Fatalf("上傳的檔不在 %s", uploadedAt)
	}

	code, dry := boardJSON(t, boardB, "/api/personas/import", `{"upload":"`+uploaded+`","dry_run":true}`)
	if code != http.StatusOK || dry["dry_run"] != true || listLen(dry["added"]) != 3 || listLen(dry["records_added"]) != 1 {
		t.Fatalf("dry_run：%d %v", code, dry)
	}
	if fileExists(filepath.Join(homeB, "personas", "helper.md")) || !fileExists(uploadedAt) {
		t.Fatal("dry_run 什麼都不寫、上傳的檔留著等確定")
	}
	if c, _, _ := hg(t, sockB, "", "data", "get", "dress.outfit", "helper-sea"); c == 0 {
		t.Fatal("dry_run 不寫紀錄")
	}

	code, imp := boardJSON(t, boardB, "/api/personas/import", `{"upload":"`+uploaded+`"}`)
	if code != http.StatusOK || imp["dry_run"] != false || listLen(imp["added"]) != 3 {
		t.Fatalf("import：%d %v", code, imp)
	}
	if !fileExists(filepath.Join(homeB, "personas", "helper.md")) || fileExists(filepath.Join(homeB, "personas", "avatars", "builder.png")) {
		t.Fatal("她的卡要到、別的角色的圖不到")
	}
	if fileExists(uploadedAt) {
		t.Fatal("匯入完要刪掉上傳的檔")
	}
	rec := jsonLine(t, hgOK(t, sockB, "data", "get", "dress.outfit", "helper-sea", "--json"))
	if f, _ := rec["fields"].(map[string]any); f["name"] != "海邊" || f["by"] != "owner(web)" {
		t.Fatalf("紀錄經 vault 寫入、記在主人名下：%v", rec)
	}

	// 摘要跟指令的 --json 一模一樣（再匯入一次：全部「一樣」）。
	again := filepath.Join(t.TempDir(), "again.hgpersona")
	if err := os.WriteFile(again, file, 0o644); err != nil {
		t.Fatal(err)
	}
	cli := jsonLine(t, hgOK(t, sockB, "personas", "import", again, "--dry-run", "--json", "--home", homeB))
	uploaded = boardPersonaUpload(t, boardB, file)
	_, api := boardJSON(t, boardB, "/api/personas/import", `{"upload":"`+uploaded+`","dry_run":true}`)
	if !reflect.DeepEqual(cli, api) {
		t.Fatalf("指令 --json 與 API 不一樣：\n%v\n%v", cli, api)
	}

	// 秘書（不是主人）經 API 呼叫被擋：匯出、匯入都是 forbidden，上傳的檔不動。
	client := sdk.NewPersonasClient(sdk.Dial(sockB))
	ctx := context.Background()
	var se *sdk.Error
	if _, err := client.Export(ctx, &sdk.PersonasExportRequest{Personas: []string{"helper"}, By: &sdk.Actor{Name: "code"}}); !errors.As(err, &se) || se.Code != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書匯出：%v", err)
	}
	if _, err := client.Import(ctx, &sdk.PersonasImportRequest{Upload: uploaded, By: &sdk.Actor{Name: "code"}}); !errors.As(err, &se) || se.Code != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書匯入：%v", err)
	}
	if !fileExists(filepath.Join(platform.PersonaUploadDir(homeB), uploaded)) {
		t.Fatal("被擋的呼叫不動上傳的檔")
	}
}

// 核心這邊也擋太大的檔（跟上傳同一個上限，sdk.PersonaImportMaxBytes），並刪掉；壞掉的檔回白話、刪掉；名字不對、找不到也是白話（PRS-18）。
func TestPersonasImportUploadLimits(t *testing.T) {
	home, sock := startFixtureAPIWith(t, nil)
	client := sdk.NewPersonasClient(sdk.Dial(sock))
	ctx := context.Background()
	owner := &sdk.Actor{Name: "owner(web)"}
	dir := platform.PersonaUploadDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "20261104-120000-0a1b2c3d.hgpersona")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(sdk.PersonaImportMaxBytes + 1); err != nil { // 稀疏檔，不真的佔 2 GB
		t.Fatal(err)
	}
	_ = f.Close()
	var se *sdk.Error
	_, err = client.Import(ctx, &sdk.PersonasImportRequest{Upload: filepath.Base(big), DryRun: true, By: owner})
	if !errors.As(err, &se) || se.Code != sdk.ErrorCodeInvalid || !strings.Contains(se.Message, "太大") || fileExists(big) {
		t.Fatalf("太大：%v（檔還在：%v）", err, fileExists(big))
	}

	broken := filepath.Join(dir, "20261104-120001-0a1b2c3d.hgpersona")
	putFile(t, broken, "不是 zip")
	_, err = client.Import(ctx, &sdk.PersonasImportRequest{Upload: filepath.Base(broken), DryRun: true, By: owner})
	if !errors.As(err, &se) || se.Code != sdk.ErrorCodeInvalid || !strings.Contains(se.Message, "什麼都沒寫") || fileExists(broken) {
		t.Fatalf("壞掉的檔：%v", err)
	}
	for _, name := range []string{"../home.toml", "x.hgpersona", "20261104-120002-0a1b2c3d.hgpersona"} {
		if _, err := client.Import(ctx, &sdk.PersonasImportRequest{Upload: name, DryRun: true, By: owner}); !errors.As(err, &se) || se.Code != sdk.ErrorCodeInvalid || !strings.Contains(se.Message, "重新選檔") {
			t.Errorf("%s：%v", name, err)
		}
	}
	if _, err := client.Export(ctx, &sdk.PersonasExportRequest{By: owner}); !errors.As(err, &se) || se.Code != sdk.ErrorCodeInvalid {
		t.Errorf("沒選角色：%v", err)
	}
}
