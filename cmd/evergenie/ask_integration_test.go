//go:build integration

package main

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 這個測試跑真的 Claude 一次（由 code 在本機跑；執行者只要 go vet -tags integration 過）：
//
//	make build
//	EVERGENIE_IT_CONFIG_DIR=~/.claude-acme EVERGENIE_IT_MODEL=haiku go test -tags integration -timeout 10m -run TestAskRealClaude -v ./cmd/evergenie
//
// 一次性對話（Ask）不開工具、不留 session：跑完帳號資料夾的 projects/ 底下不能多出任何檔案。

// projectFiles 列出帳號資料夾 projects/ 底下所有檔案（相對路徑，排序過）。
func projectFiles(t *testing.T, configDir string) []string {
	t.Helper()
	root := filepath.Join(configDir, "projects")
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// Guards: REQ-130
func TestAskRealClaude(t *testing.T) {
	configDir, model := integrationConfig(t)
	if strings.HasPrefix(configDir, "~/") {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, configDir[2:])
	}
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
	engineSocket := filepath.Join(home, "runtime", "sockets", "engine-claude.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = append(scrubbedEnv(), "EVERGENIE_HOME="+home, "EVERGENIE_API_SOCKET="+filepath.Join(home, "runtime", "sockets", "api.sock"), "EVERGENIE_ENGINE_SOCKET="+engineSocket)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	waitForSocket(t, engineSocket)

	before := projectFiles(t, configDir)
	engine := sdk.NewEngineClient(sdk.Dial(engineSocket))
	got, err := engine.Ask(ctx, &sdk.EngineAskRequest{
		Agent: "test", Cwd: t.TempDir(), Account: "test",
		EngineConfig:   map[string]any{"config_dir": configDir},
		Model:          model,
		Instructions:   "你叫測試員，回覆只說「收到」兩個字。",
		Message:        "哈囉",
		TimeoutSeconds: 180,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reply == "" || !strings.Contains(got.Reply, "收到") {
		t.Fatalf("回覆要非空而且含「收到」：%q", got.Reply)
	}
	t.Logf("回覆：%q（模型 %q）", got.Reply, got.Model)
	after := projectFiles(t, configDir)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("一次性對話不能留下 session 檔：\n前 %d 個檔、後 %d 個檔\n前：%v\n後：%v", len(before), len(after), before, after)
	}
}
