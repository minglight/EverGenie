package pkg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// buildTimeout 是一次 go build 最多等多久（逾時說「編太久停掉了」，buildSlow）；測試改短。
var buildTimeout = 5 * time.Minute

// errNoGo：找不到能用的 Go（*goMissing 包著它，帶白話原因；toolchain.go）。不自動裝（D-27）：套件標 failed、
// 帶補救代號 install_go，主人同意後才呼叫 InstallGo。
var errNoGo = errors.New("go not found")

func errMessage(err error) string {
	if e, ok := err.(*platform.Err); ok {
		return e.Message
	}
	return err.Error()
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func isExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// findPrebuilt 找官方套件跟著發版的執行檔：EVERGENIE_PACKAGE_BIN_DIR、EVERGENIE_ENGINE_BIN_DIR、核心執行檔旁邊（platform.ExecutableDirs）。
func findPrebuilt(name string) string {
	dirs := append([]string{sdk.Getenv("EVERGENIE_PACKAGE_BIN_DIR"), sdk.Getenv("EVERGENIE_ENGINE_BIN_DIR")}, platform.ExecutableDirs()...)
	for _, d := range dirs {
		if d != "" && isExecutable(filepath.Join(d, name)) {
			return filepath.Join(d, name)
		}
	}
	return ""
}

// ensureBinary 回傳要啟動的執行檔。官方套件優先用發版附的；其餘（mine、installed）用 go build 編到
// <家>/runtime/packages/<id>/bin/，套件 import 的 sdk 用這份程式旁邊附的（工作區 go.work，sdkwork.go，PKG-66）；
// 內容沒變（build.stamp 的指紋相同：套件的檔，加上 sdk 的位置與內容）就不重編。沒有 Go、沒有 sdk 回白話的錯誤。
func (m *Module) ensureBinary(ctx context.Context, id string, tier sdk.Tier, dir string, man *Manifest) (string, error) {
	name := man.Binary()
	var work *sdkWork
	if tier == sdk.TierOfficial {
		if p := findPrebuilt(name); p != "" {
			return p, nil
		}
	} else {
		w, err := m.sdkWorkFor(id, dir)
		if err != nil {
			return "", err
		}
		work = w
	}
	outDir := filepath.Join(m.runtimeDir(id), "bin")
	out := filepath.Join(outDir, name)
	stamp := filepath.Join(m.runtimeDir(id), "build.stamp")
	fp := fingerprint(dir)
	if work != nil {
		fp += "|" + work.stamp
	}
	prev, _ := os.ReadFile(stamp)
	if strings.TrimSpace(string(prev)) == fp && isExecutable(out) {
		return out, nil
	}
	// 上次編成功用的是另一份 sdk（升級、退回換了版本）：編不過而且提到 sdk 的名字時，才說是「跟這一版的工具組對不上」。
	sdkChanged := false
	if p := strings.TrimSpace(string(prev)); work != nil {
		if i := strings.Index(p, "|sdk="); i >= 0 {
			sdkChanged = p[i+1:] != work.stamp
		}
	}
	goBin, err := m.lookGo()
	if err != nil {
		var miss *goMissing
		if !errors.As(err, &miss) {
			miss = &goMissing{reason: goMsgNone}
		}
		return "", miss
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", platform.Unavailable("無法建立編譯輸出資料夾")
	}
	var env []string
	if work != nil {
		if err := work.write(); err != nil {
			return "", platform.Unavailable("無法寫編譯用的工作區檔案")
		}
		env = work.env()
	}
	bctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	// -buildvcs=false：套件資料夾在 git 裡（家放進 git、為了分享 git init）時 go 不去叫 git——乾淨的 Mac 上 git 是一叫就跳
	// 「安裝開發者工具」視窗的空殼，編譯會失敗；套件的執行檔也用不到版本控制的資訊。
	runDir := dir
	if work != nil {
		runDir = work.pkgDir // 解開符號連結的路徑：跟 go.work 寫的一樣（sdkWorkFor）
	}
	output, err := m.runner.Run(bctx, runDir, env, goBin, "build", "-buildvcs=false", "-o", out, "./"+filepath.ToSlash(man.Program.Entry))
	if err != nil {
		if bctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return "", buildSlow()
		}
		return "", buildFailure(string(output), work, sdkChanged)
	}
	if err := os.WriteFile(stamp, []byte(fp), 0o644); err != nil {
		return "", platform.Unavailable("無法記錄編譯結果")
	}
	return out, nil
}

// tail 取輸出的最後 n 行（編譯錯誤通常在最後），空輸出給一句通用的話。
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return "編譯沒有成功（沒有輸出訊息）"
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
