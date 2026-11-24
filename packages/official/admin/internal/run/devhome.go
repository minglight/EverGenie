package run

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 開發家狀態（從核心 internal/api/dev.go 的 dev.status＋probeDevHome 搬過來，REQ-184、185、247、248、266）。

// versionDetailPrefix 是開發家 system.doctor 的 config.version 那一條 detail 的開頭（核心 doctor.VersionDetailPrefix（internal/feature/system/doctor））。
const versionDetailPrefix = "核心版本："

// DoctorFunc 問一個家的 system.doctor（只跑 config 群組）；sock 是那個家暫存資料夾的 sockets/api.sock（第 2 版的家在狀態資料夾）。
type DoctorFunc func(ctx context.Context, sock string) (*sdk.SystemDoctorResponse, error)

// DialDoctor 是真的 DoctorFunc：經 unix socket 打那個家的 API。
func DialDoctor(ctx context.Context, sock string) (*sdk.SystemDoctorResponse, error) {
	return sdk.NewSystemClient(sdk.Dial(sock)).Doctor(ctx, &sdk.SystemDoctorRequest{Groups: []sdk.DoctorGroup{sdk.DoctorGroupConfig}})
}

// ProbeDevHome 看本機的開發家開了沒：timeout 內 doctor 有回應就是開著，版本從 config.version 讀；
// 看板 port、網址讀它 home.toml 的 [runtime]（網址只收 http／https）。
func ProbeDevHome(ctx context.Context, dir string, doctor DoctorFunc, timeout time.Duration) DevHome {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return DevHome{State: DevHomeUnset, Detail: "還沒設定開發家：在 home.toml 加一段 [admin]，寫 dev_home = \"<開發家的資料夾完整路徑>\""}
	}
	st := DevHome{Path: dir}
	if !filepath.IsAbs(dir) {
		st.State = DevHomeStopped
		st.Detail = "home.toml 的 [admin] dev_home 要寫完整路徑（從 / 開頭）"
		return st
	}
	runtime := filepath.Join(dir, "runtime") // 讀不到 home.toml 就當第 1 版
	if peek, err := PeekHome(dir); err == nil {
		runtime = peek.Runtime
		st.BoardPort = peek.BoardPort
		if u := peek.URL; strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
			st.URL = u
		}
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := doctor(pctx, filepath.Join(runtime, "sockets", "api.sock"))
	if err != nil || res == nil {
		st.State = DevHomeStopped
		st.Detail = "開發家沒有開（它的 api.sock 在 1 秒內沒有回應）"
		return st
	}
	st.State = DevHomeRunning
	for _, c := range res.Checks {
		if c.ID == "config.version" {
			st.Version = strings.TrimPrefix(c.Detail, versionDetailPrefix)
		}
	}
	st.Detail = "開發家開著"
	if st.Version == "" {
		st.Detail += "（它的核心比較舊，不會回報版本）"
	}
	return st
}

// ComputeDevHome 探測本機開發家。
func ComputeDevHome(ctx context.Context, cfg Config, doctor DoctorFunc, timeout time.Duration) (DevHome, bool) {
	return ProbeDevHome(ctx, cfg.DevHome, doctor, timeout), true
}
