package sdk

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// fakePackageHost 只實作 Hello；其他方法回 unavailable（ServePackage 只用 Hello）。
type fakePackageHost struct{ got *PackageHelloRequest }

func (f *fakePackageHost) Hello(_ context.Context, req *PackageHelloRequest) (*PackageHelloResponse, error) {
	f.got = req
	if req.API != "evergenie.v1" {
		return nil, &Error{Code: ErrorCodeInvalid, Message: "協定版本不對"}
	}
	return &PackageHelloResponse{Token: "tok", DataDir: "vault/pkg/" + req.ID, Granted: []string{"read:vault:task"}}, nil
}

var errFakeUnavailable = &Error{Code: ErrorCodeUnavailable, Message: "fake"}

func (*fakePackageHost) List(context.Context, *PackageListRequest) (*PackageListResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Get(context.Context, *PackageGetRequest) (*PackageGetResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Install(context.Context, *PackageInstallRequest) (*PackageInstallResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Uninstall(context.Context, *PackageUninstallRequest) (*PackageUninstallResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Reload(context.Context, *PackageReloadRequest) (*PackageReloadResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) SetEnabled(context.Context, *PackageSetEnabledRequest) (*PackageSetEnabledResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Fork(context.Context, *PackageForkRequest) (*PackageForkResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) ListUpdates(context.Context, *PackageListUpdatesRequest) (*PackageListUpdatesResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) New(context.Context, *PackageNewRequest) (*PackageNewResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Preview(context.Context, *PackagePreviewRequest) (*PackagePreviewResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Outdated(context.Context, *PackageOutdatedRequest) (*PackageOutdatedResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Upgrade(context.Context, *PackageUpgradeRequest) (*PackageUpgradeResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Export(context.Context, *PackageExportRequest) (*PackageExportResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) ClaimNotify(context.Context, *PackageClaimNotifyRequest) (*PackageClaimNotifyResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) ListExtensions(context.Context, *PackageListExtensionsRequest) (*PackageListExtensionsResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) SetExtension(context.Context, *PackageSetExtensionRequest) (*PackageSetExtensionResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) InstallGo(context.Context, *PackageInstallGoRequest) (*PackageInstallGoResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) GoStatus(context.Context, *PackageGoStatusRequest) (*PackageGoStatusResponse, error) {
	return nil, errFakeUnavailable
}
func (*fakePackageHost) Sync(context.Context, *PackageSyncRequest) (*PackageSyncResponse, error) {
	return nil, errFakeUnavailable
}

type recordProgram struct{ host *PackageHost }

func (r *recordProgram) Run(_ context.Context, h *PackageHost) error { r.host = h; return nil }

// TestServePackageHello：套件行程從環境變數讀到自己的身分，先報到，成功才進 Run；報到失敗不呼叫 Run。
// Guards: REQ-91
func TestServePackageHello(t *testing.T) {
	core := &fakePackageHost{}
	_, socket, _ := startServer(t, map[string]any{"package": core})
	t.Setenv("EVERGENIE_API_SOCKET", socket)
	t.Setenv("EVERGENIE_PACKAGE_ID", "my-habit")
	t.Setenv("EVERGENIE_PACKAGE_API", "evergenie.v1")
	t.Setenv("EVERGENIE_PACKAGE_VERSION", "0.1.0")
	t.Setenv("EVERGENIE_PACKAGE_DIR", "/tmp/packages/mine/my-habit")
	p := &recordProgram{}
	if err := ServePackage(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if core.got == nil || core.got.ID != "my-habit" || core.got.Version != "0.1.0" {
		t.Fatalf("Hello 收到 %+v", core.got)
	}
	h := p.host
	if h == nil || h.Token != "tok" || h.DataDir != "vault/pkg/my-habit" || h.Dir != "/tmp/packages/mine/my-habit" || len(h.Granted) != 1 || h.Client == nil {
		t.Fatalf("PackageHost = %+v", h)
	}

	t.Setenv("EVERGENIE_PACKAGE_API", "evergenie.v9")
	p = &recordProgram{}
	err := ServePackage(context.Background(), p)
	var se *Error
	if !errors.As(err, &se) || se.Code != ErrorCodeInvalid || p.host != nil {
		t.Fatalf("報到失敗要回錯誤、不進 Run：%v %+v", err, p.host)
	}

	t.Setenv("EVERGENIE_PACKAGE_ID", "")
	if err := ServePackage(context.Background(), &recordProgram{}); err == nil {
		t.Fatal("沒有 EVERGENIE_PACKAGE_ID 要失敗")
	}
}

// TestServePackageWaitsForCoreSocket：核心的 API socket 晚一點才開，套件照樣報到成功（DEV-8）；一直不開就在時限後回錯誤。
// Guards: REQ-257
func TestServePackageWaitsForCoreSocket(t *testing.T) {
	t.Setenv("EVERGENIE_PACKAGE_ID", "habits")
	t.Setenv("EVERGENIE_PACKAGE_API", "evergenie.v1")
	core := &fakePackageHost{}
	socket := shortSocket(t)
	t.Setenv("EVERGENIE_API_SOCKET", socket)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	time.AfterFunc(500*time.Millisecond, func() { served <- Serve(ctx, socket, map[string]any{"package": core}) })
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	p := &recordProgram{}
	if err := ServePackage(context.Background(), p); err != nil {
		t.Fatalf("socket 晚開也要報到成功：%v", err)
	}
	if p.host == nil || p.host.Token != "tok" {
		t.Fatalf("PackageHost = %+v", p.host)
	}

	old := helloRetryFor
	helloRetryFor = 300 * time.Millisecond
	t.Cleanup(func() { helloRetryFor = old })
	t.Setenv("EVERGENIE_API_SOCKET", filepath.Join(t.TempDir(), "never.sock"))
	start := time.Now()
	if err := ServePackage(context.Background(), &recordProgram{}); err == nil {
		t.Fatal("核心一直不在要回錯誤")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("重試要在時限內停：%v", time.Since(start))
	}
}
