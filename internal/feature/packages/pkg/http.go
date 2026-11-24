package pkg

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "package"，實作是下面的 service。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "package", Impl: &service{m: m}}}
}

// service 實作 sdk.PackageAPI：寫入（安裝、解除安裝、fork、停用、擴充點、裝 Go…）都要帶操作者，
// 直接呼叫本模組，把錯誤轉成 API 錯誤（原本在 internal/api/pkg.go）。
type service struct{ m *Module }

var _ sdk.PackageAPI = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && s.m.log != nil && !errors.Is(err, context.Canceled) {
		s.m.log.Error("request failed", "error", err)
	}
	return out
}

// call 是共用的轉接：（要的話）檢查操作者、呼叫模組、把錯誤換成 SDK 的錯誤。
func call[Req, Resp any](s *service, ctx context.Context, req *Req, who *sdk.Actor, needBy bool, f func(sdk.PackageAPI, context.Context, *Req) (*Resp, error)) (*Resp, error) {
	if needBy {
		if _, err := platform.ActorName(who); err != nil {
			return nil, s.fail(err)
		}
	}
	out, err := f(s.m, ctx, req)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) Hello(ctx context.Context, req *sdk.PackageHelloRequest) (*sdk.PackageHelloResponse, error) {
	return call(s, ctx, req, nil, false, sdk.PackageAPI.Hello)
}
func (s *service) List(ctx context.Context, req *sdk.PackageListRequest) (*sdk.PackageListResponse, error) {
	return call(s, ctx, req, nil, false, sdk.PackageAPI.List)
}
func (s *service) Get(ctx context.Context, req *sdk.PackageGetRequest) (*sdk.PackageGetResponse, error) {
	return call(s, ctx, req, nil, false, sdk.PackageAPI.Get)
}
func (s *service) Install(ctx context.Context, req *sdk.PackageInstallRequest) (*sdk.PackageInstallResponse, error) {
	return call(s, ctx, req, req.By, true, sdk.PackageAPI.Install)
}
func (s *service) Uninstall(ctx context.Context, req *sdk.PackageUninstallRequest) (*sdk.PackageUninstallResponse, error) {
	return call(s, ctx, req, req.By, true, sdk.PackageAPI.Uninstall)
}
func (s *service) Reload(ctx context.Context, req *sdk.PackageReloadRequest) (*sdk.PackageReloadResponse, error) {
	return call(s, ctx, req, nil, false, sdk.PackageAPI.Reload)
}
func (s *service) SetEnabled(ctx context.Context, req *sdk.PackageSetEnabledRequest) (*sdk.PackageSetEnabledResponse, error) {
	return call(s, ctx, req, req.By, true, sdk.PackageAPI.SetEnabled)
}

// ForkClosed 是「整包複製官方套件」關掉之後回的白話說明（DEV-154，作者 2026-11-12：第一版不開放整包複製，
// 那是最容易跟升級衝突的路）。要改官方套件的行為只有兩條路：擴充點，或自己做新套件。
const ForkClosed = "官方套件不能整包複製來改（複製之後每次升級都要自己合併）。想調整官方套件，請用它的擴充點（設定值、在職責後面加一段、頁面插一塊）；想要新的東西，用 evergenie packages new 做自己的套件"

// Fork：對外的入口已關（看板、CLI、套件都走這裡）。模組裡的 Fork 留著，只給已經複製過的家讀更新差異與測試用。
func (s *service) Fork(context.Context, *sdk.PackageForkRequest) (*sdk.PackageForkResponse, error) {
	return nil, s.fail(platform.Forbidden(ForkClosed))
}
func (s *service) New(ctx context.Context, req *sdk.PackageNewRequest) (*sdk.PackageNewResponse, error) {
	return call(s, ctx, req, req.By, true, sdk.PackageAPI.New)
}
func (s *service) ListUpdates(ctx context.Context, req *sdk.PackageListUpdatesRequest) (*sdk.PackageListUpdatesResponse, error) {
	return call(s, ctx, req, nil, false, sdk.PackageAPI.ListUpdates)
}
func (s *service) ListExtensions(ctx context.Context, req *sdk.PackageListExtensionsRequest) (*sdk.PackageListExtensionsResponse, error) {
	return call(s, ctx, req, nil, false, sdk.PackageAPI.ListExtensions)
}
func (s *service) SetExtension(ctx context.Context, req *sdk.PackageSetExtensionRequest) (*sdk.PackageSetExtensionResponse, error) {
	return call(s, ctx, req, req.By, true, sdk.PackageAPI.SetExtension)
}
func (s *service) InstallGo(ctx context.Context, req *sdk.PackageInstallGoRequest) (*sdk.PackageInstallGoResponse, error) {
	return call(s, ctx, req, req.By, true, sdk.PackageAPI.InstallGo)
}
func (s *service) GoStatus(ctx context.Context, req *sdk.PackageGoStatusRequest) (*sdk.PackageGoStatusResponse, error) {
	return call(s, ctx, req, nil, false, sdk.PackageAPI.GoStatus)
}
func (s *service) Sync(ctx context.Context, req *sdk.PackageSyncRequest) (*sdk.PackageSyncResponse, error) {
	// 預演不用操作者；套用要帶（是不是主人由 pkg 判斷）。
	return call(s, ctx, req, req.By, req.Apply, sdk.PackageAPI.Sync)
}

func (s *service) Preview(ctx context.Context, req *sdk.PackagePreviewRequest) (*sdk.PackagePreviewResponse, error) {
	return call(s, ctx, req, nil, false, sdk.PackageAPI.Preview)
}
func (s *service) Outdated(ctx context.Context, req *sdk.PackageOutdatedRequest) (*sdk.PackageOutdatedResponse, error) {
	return call(s, ctx, req, nil, false, sdk.PackageAPI.Outdated)
}
func (s *service) Upgrade(ctx context.Context, req *sdk.PackageUpgradeRequest) (*sdk.PackageUpgradeResponse, error) {
	return call(s, ctx, req, req.By, true, sdk.PackageAPI.Upgrade)
}
func (s *service) Export(ctx context.Context, req *sdk.PackageExportRequest) (*sdk.PackageExportResponse, error) {
	// 匯出會在這台電腦寫檔（資料夾或壓縮檔），要帶操作者。
	return call(s, ctx, req, req.By, true, sdk.PackageAPI.Export)
}
func (s *service) ClaimNotify(ctx context.Context, req *sdk.PackageClaimNotifyRequest) (*sdk.PackageClaimNotifyResponse, error) {
	// 秘書與主人都能請核心發認領通知；它只發通知，套用要等主人按「好」（SEC-302 S9）。
	return call(s, ctx, req, req.By, true, sdk.PackageAPI.ClaimNotify)
}
