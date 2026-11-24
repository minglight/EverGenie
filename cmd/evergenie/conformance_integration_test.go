//go:build integration

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/conformance"
	"github.com/minglight/evergenie/sdk"
)

// 這個測試跑真的 Claude（由 code 在本機跑，Codex 只要 go vet -tags integration 過）：
//
//	make build
//	EVERGENIE_IT_CONFIG_DIR=~/.claude-acme EVERGENIE_IT_MODEL=haiku go test -tags integration -run TestEngineClaudeConformance ./cmd/evergenie
//
// tmux socket 一律 hg-test（不准用 agents、hg），結束時 kill-server。
const integrationTmuxSocket = "hg-test"

// withStartDefaults 補上一致性案例沒有的設定：帳號、engine_config、核心位址、模型；
// 第一次進新資料夾的信任對話框由這裡經 AnswerBootPrompt 選信任（跟看板做的事一樣）。
type withStartDefaults struct {
	sdk.EngineAPI
	configDir, model, hostSocket string
}

func (w withStartDefaults) Start(ctx context.Context, r *sdk.EngineStartRequest) (*sdk.EngineStartResponse, error) {
	r.Account = "test"
	r.EngineConfig = map[string]any{"config_dir": w.configDir, "tmux_socket": integrationTmuxSocket, "isolate": true, "permission_mode": "auto"}
	r.Model = w.model
	r.HostEndpoint = w.hostSocket
	resp, err := w.EngineAPI.Start(ctx, r)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		list, err := w.EngineAPI.ListBootPrompts(ctx, &sdk.EngineListBootPromptsRequest{Session: r.Name})
		if err == nil && len(list.Prompts) > 0 {
			p := list.Prompts[0]
			_, err := w.EngineAPI.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: p.ID, Choice: p.Choices[0]})
			if err != nil {
				return nil, err
			}
			time.Sleep(3 * time.Second) // 信任之後 claude 才進主畫面
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	return resp, nil
}

func (w withStartDefaults) Ask(ctx context.Context, r *sdk.EngineAskRequest) (*sdk.EngineAskResponse, error) {
	r.Account = "test"
	r.EngineConfig = map[string]any{"config_dir": w.configDir}
	r.Model = w.model
	return w.EngineAPI.Ask(ctx, r)
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
	}
	t.Fatalf("socket 沒有出現：%s", path)
}

// Guards: REQ-75, REQ-76
func TestEngineClaudeConformance(t *testing.T) {
	configDir, model := integrationConfig(t)
	bin, err := filepath.Abs("../../bin/evergenie-engine-claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("找不到 %s，先跑 make build", bin)
	}
	home, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	apiSocket := filepath.Join(home, "runtime", "sockets", "api.sock")
	engineSocket := filepath.Join(home, "runtime", "sockets", "engine-claude.sock")

	host := conformance.NewHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = sdk.Serve(ctx, apiSocket, map[string]any{"engine_host": host}) }()
	waitForSocket(t, apiSocket)

	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = append(scrubbedEnv(), "EVERGENIE_HOME="+home, "EVERGENIE_API_SOCKET="+apiSocket, "EVERGENIE_ENGINE_SOCKET="+engineSocket)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// 先關測試用的 tmux server（裡面的 session 都是這個測試開的），再停引擎行程。
		_ = exec.Command("tmux", "-L", integrationTmuxSocket, "kill-server").Run()
		cancel()
		_ = cmd.Wait()
	})
	waitForSocket(t, engineSocket)

	engine := withStartDefaults{EngineAPI: sdk.NewEngineClient(sdk.Dial(engineSocket)), configDir: configDir, model: model, hostSocket: apiSocket}
	if _, err := engine.SetPermission(ctx, &sdk.EngineSetPermissionRequest{Account: "test", Policy: &sdk.PermissionPolicy{}}); err != nil {
		t.Fatal(err)
	}
	conformance.Run(t, engine, host.Events)
}
