package persona

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personaexport"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// 看板的角色匯出／匯入（DEV-116 S3、PRS-17～19）：跟指令 `personas export`／`import` 同一份程式（personaexport），
// 這裡只管 API 的事：只有主人、檔案放在家的 runtime/、用完清掉、套件紀錄的寫入記在主人名下。
//
//   - 匯出：runtime/persona-export/<時間>-<8 碼>/<名字或代號>.hgpersona；看板 GET /persona-export/<資料夾>/<檔名> 下載一次就刪。
//   - 匯入：看板先把檔 POST 到 /api/web/persona-upload（存成 runtime/persona-import/uploads/<時間>-<8 碼>.hgpersona），
//     再呼叫 personas.import 帶那個名字；真的匯入完、或檔案不合格就刪。
//   - 放超過 transferTTL 沒人拿的（看了預覽就關掉、換了一個檔、下載沒成功），由定時工作 personas.transfer_sweep
//     （serve 啟動後與之後每 transferSweepEvery）清掉；每次匯出、匯入前也先清一次。
//   - 位置與名字格式跟看板共用 platform.PersonaUploadDir、PersonaExportDir、PersonaTransferToken。

const (
	transferTTL        = time.Hour
	transferSweepEvery = 10 * time.Minute
)

// transferMu 讓匯出、匯入一次一個（兩次匯入同時寫同一個家會互相蓋登記檔）。
var transferMu sync.Mutex

// ActorStore 是組裝接的 Store 可以換署名的版本：看板匯入寫的紀錄記在主人（owner(web)）名下，不是 system。
type ActorStore interface {
	As(by string) personapack.Store
}

// sweepTransfers 清掉放超過 transferTTL 的匯出檔與上傳檔（錯誤不管，下次再清）。
// 跟檔案的修改時間比，所以用牆上時鐘、不用模組的 Clock（測試的假時鐘會讓剛放的檔看起來很舊）。
func (m *Module) sweepTransfers() {
	now := time.Now()
	if m.home == "" {
		return
	}
	for _, dir := range []string{platform.PersonaExportDir(m.home), platform.PersonaUploadDir(m.home)} {
		items, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, it := range items {
			info, err := it.Info()
			if err != nil || now.Sub(info.ModTime()) < transferTTL {
				continue
			}
			_ = os.RemoveAll(filepath.Join(dir, it.Name()))
		}
	}
}

func (m *Module) transferStore(by string) personapack.Store {
	s := m.recordStore()
	if as, ok := s.(ActorStore); ok {
		return as.As(by)
	}
	return s
}

func ownerOnly(by *sdk.Actor, what string) (string, error) {
	who, err := platform.ActorName(by)
	if err != nil {
		return "", err
	}
	if !platform.IsOwnerOrSystem(who) {
		return "", platform.Forbidden("只有主人能" + what)
	}
	return who, nil
}

// Export 實作 personas.export。
func (s *service) Export(ctx context.Context, req *sdk.PersonasExportRequest) (*sdk.PersonasExportResponse, error) {
	if _, err := ownerOnly(req.By, "匯出角色"); err != nil {
		return nil, s.fail(err)
	}
	res, err := s.m.ExportForBoard(ctx, req.Personas)
	if err != nil {
		return nil, s.fail(err)
	}
	return res, nil
}

// Import 實作 personas.import。
func (s *service) Import(ctx context.Context, req *sdk.PersonasImportRequest) (*sdk.PersonasImportResponse, error) {
	who, err := ownerOnly(req.By, "匯入角色")
	if err != nil {
		return nil, s.fail(err)
	}
	res, err := s.m.ImportUpload(ctx, req.Upload, req.DryRun, req.Force, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return res, nil
}

// ExportForBoard 把角色匯出到 runtime/persona-export/<資料夾>/<檔名>，回下載網址。
func (m *Module) ExportForBoard(ctx context.Context, personas []string) (*sdk.PersonasExportResponse, error) {
	if m.home == "" {
		return nil, platform.Unavailable("角色庫還沒準備好")
	}
	if len(personas) == 0 {
		return nil, platform.Invalid("personas", "請至少選一位角色")
	}
	transferMu.Lock()
	defer transferMu.Unlock()
	m.sweepTransfers()
	now := m.d.Clock.Now()
	name := m.exportFilename(ctx, personas)
	token, err := platform.NewPersonaTransferToken(now)
	if err != nil {
		return nil, platform.Internal("內部錯誤")
	}
	dir := filepath.Join(platform.PersonaExportDir(m.home), token)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, platform.Unavailable("無法建立匯出的暫存資料夾")
	}
	res, _, err := personaexport.ExportToFile(ctx, personaexport.ExportOptions{Home: m.home, Personas: personas, Store: m.recordStore(), Now: now, Bundled: m.bundledDir()}, filepath.Join(dir, name))
	if err != nil {
		_ = os.RemoveAll(dir)
		// 選錯角色這類（INVALID）照原話；其他是寫暫存檔失敗，原話帶家的完整路徑與指令用語，看板換成白話（log 留原話）。
		if platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			if h, herr := platform.LoadHome(m.home); herr == nil {
				platform.NewLogger(h, "personas").Warn("看板的角色匯出失敗", "personas", personas, "error", err.Error())
			}
			return nil, platform.Unavailable("匯出檔沒做成（寫不進家裡的暫存資料夾），請再試一次")
		}
		return nil, err
	}
	return &sdk.PersonasExportResponse{
		Personas: res.Personas, Files: int32(res.Files), Records: int32(res.Records), Bytes: res.Bytes,
		Kinds: res.Kinds, Skipped: res.Skipped, Filename: name,
		Download: "/persona-export/" + token + "/" + url.PathEscape(name),
	}, nil
}

// exportFilename：一位＝她的名字（沒有或名字全是符號時用代號）；兩三位用「-」接起來；更多是「第一位等 N 位」。
// 檔名只留英數、`._-` 與中日韓文字（platform.ShareName），不會有 `/` 或 `.` 開頭。
func (m *Module) exportFilename(ctx context.Context, slugs []string) string {
	names := map[string]string{}
	if rows, err := m.Library(ctx); err == nil {
		for _, r := range rows {
			if r.Source != "package" && !r.Shadowed {
				names[r.Slug] = r.Name
			}
		}
	}
	var parts []string
	seen := map[string]bool{}
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		n := strings.Trim(platform.ShareName(names[slug]), "._-")
		if n == "" {
			n = strings.Trim(platform.ShareName(slug), "._-")
		}
		if n == "" {
			n = "persona"
		}
		parts = append(parts, n)
	}
	switch {
	case len(parts) == 0:
		return "persona" + personaexport.Ext
	case len(parts) > 3:
		return fmt.Sprintf("%s等%d位%s", parts[0], len(parts), personaexport.Ext)
	}
	return strings.Join(parts, "-") + personaexport.Ext
}

// sweepJob 是定時工作 personas.transfer_sweep。
func (m *Module) sweepJob(context.Context) error {
	transferMu.Lock()
	defer transferMu.Unlock()
	m.sweepTransfers()
	return nil
}

// ImportUpload 匯入（或預覽）看板上傳的檔。上限跟指令一樣（sdk.PersonaImportMaxBytes）；
// 真的匯入成功、或檔案不合格（INVALID）就刪掉上傳的檔；連不到套件資料這類可以再試的錯誤留著，讓主人再按一次。
func (m *Module) ImportUpload(ctx context.Context, upload string, dryRun, force bool, by string) (*sdk.PersonasImportResponse, error) {
	if m.home == "" {
		return nil, platform.Unavailable("角色庫還沒準備好")
	}
	if !platform.IsPersonaUploadName(upload) {
		return nil, platform.Invalid("upload", "上傳的檔名不對，請重新選檔")
	}
	transferMu.Lock()
	defer transferMu.Unlock()
	m.sweepTransfers()
	now := m.d.Clock.Now()
	file := filepath.Join(platform.PersonaUploadDir(m.home), upload)
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() {
		return nil, platform.Invalid("upload", "找不到上傳的檔（放太久被清掉了？），請重新選檔")
	}
	if info.Size() > sdk.PersonaImportMaxBytes {
		_ = os.Remove(file)
		return nil, platform.Invalid("upload", fmt.Sprintf("匯出檔太大了（上限 %d GB）", sdk.PersonaImportMaxBytes>>30))
	}
	res, err := personaexport.ImportFile(ctx, personaexport.ImportOptions{Home: m.home, Store: m.transferStore(by), DryRun: dryRun, Force: force, Now: now, Bundled: m.bundledDir()}, file)
	if err != nil {
		if platform.CodeOf(err) == sdk.ErrorCodeInvalid {
			_ = os.Remove(file)
		}
		return nil, err
	}
	if !dryRun {
		_ = os.Remove(file)
	}
	return res.SDK(), nil
}
