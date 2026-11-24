package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// ServeConfig 是 `serve` 的設定，來自環境變數（engine 模組啟動引擎行程時設）。
type ServeConfig struct {
	Home         string // EVERGENIE_HOME
	APISocket    string // EVERGENIE_API_SOCKET
	EngineSocket string // EVERGENIE_ENGINE_SOCKET
	HookBin      string // 自己執行檔的路徑
	PluginDir    string // 本套件資料夾（官方技能的外掛，FindPluginDir）；空＝不帶 --plugin-dir
	Tmux         Tmux   // 空＝真的 tmux（固定位置的 ＞ 引擎旁邊附帶的 ＞ PATH 上的，ECL-65、ECL-63；socket 用各帳號 engine_config.tmux_socket）
	AliveEvery   time.Duration
}

// ServeConfigFromEnv 讀環境變數。EVERGENIE_ENGINE_SOCKET 沒給時用暫存資料夾的 sockets/engine-claude.sock
// （sdk.RuntimeDir：第 1 版 <家>/runtime/，第 2 版在狀態資料夾，DEV-145）。
func ServeConfigFromEnv(getenv func(string) string, hookBin string) (ServeConfig, error) {
	c := ServeConfig{Home: getenv("EVERGENIE_HOME"), APISocket: getenv("EVERGENIE_API_SOCKET"), EngineSocket: getenv("EVERGENIE_ENGINE_SOCKET"), HookBin: hookBin, PluginDir: FindPluginDir(getenv, hookBin)}
	if c.Home == "" {
		return c, invalid("EVERGENIE_HOME", "找不到家的位置（環境變數 EVERGENIE_HOME）")
	}
	if c.APISocket == "" {
		c.APISocket = sdk.RuntimeDir(c.Home, "sockets", "api.sock")
	}
	if c.EngineSocket == "" {
		c.EngineSocket = sdk.RuntimeDir(c.Home, "sockets", "engine-claude.sock")
	}
	return c, nil
}

// defaultTmux 是 serve 用的真 tmux：從自己的執行檔找跟著程式一起裝的（固定位置的 ＞ 旁邊的），沒有才找 PATH（ECL-65、ECL-63）。
// 小啟動程式印的那一行（ECL-65）寫進 <狀態>/engine-claude/out.log：核心開引擎時不收它的 stderr。
func defaultTmux(c ServeConfig) *ExecTmux {
	return &ExecTmux{Self: c.HookBin, ScriptDir: sdk.RuntimeDir(c.Home, "engine-claude", "launch"), Warn: appendLog(sdk.RuntimeDir(c.Home, "engine-claude", engineLogName))}
}

// engineLogName 是引擎自己的紀錄檔（在狀態資料夾的 engine-claude/ 底下）：只記「沒照預期做、但不擋事」的事與引擎自己動手的事（按掉擋住的選單、收掉舊視窗），一行一件、前面是時間。
const engineLogName = "out.log"

// appendLog 回一個把一行話加到 path 結尾的函式；寫不進去就印到 stderr（不擋事）。
func appendLog(path string) func(string) {
	return func(line string) {
		msg := time.Now().Format(time.RFC3339) + " " + line + "\n"
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				_, werr := f.WriteString(msg)
				if cerr := f.Close(); werr == nil && cerr == nil {
					return
				}
			}
		}
		fmt.Fprint(os.Stderr, msg)
	}
}

// Serve 掛上 EngineAPI 直到 ctx 結束；每 30 秒檢查自己開的 session 還在不在。
func Serve(ctx context.Context, c ServeConfig) error {
	tm := c.Tmux
	if tm == nil {
		tm = defaultTmux(c)
	}
	engine := NewEngine(EngineDeps{
		Home: c.Home, HookBin: c.HookBin, APISocket: c.APISocket, PluginDir: c.PluginDir, Tmux: tm,
		Host: sdk.NewEngineHostClient(sdk.Dial(sdk.ResolveAPISocket(c.APISocket))),
		Log:  appendLog(sdk.RuntimeDir(c.Home, "engine-claude", engineLogName)),
	})
	every := c.AliveEvery
	if every <= 0 {
		every = AliveInterval
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(every)
		defer t.Stop()
		// 剛起來先很快看一次（電腦重開後核心記得的 session 都不在了，ECL-31），之後照 every。
		first := time.After(min(every, startupAliveDelay))
		for {
			select {
			case <-ctx.Done():
				return
			case <-first:
				engine.CheckAlive(ctx)
			case <-t.C:
				engine.CheckAlive(ctx)
			}
		}
	}()
	err := sdk.Serve(ctx, c.EngineSocket, map[string]any{"engine": engine})
	cancel()
	<-done
	engine.Close()
	return err
}
