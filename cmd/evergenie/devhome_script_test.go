package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

// scripts/dev-home.sh init 建出來的開發家：[runtime] sandbox = true（不推播、不自動升級）、env = "dev"（看板與對話名標成開發家），
// 用新的寫法，不再寫舊的 dev = true；建出來的家 LoadHome 讀得起來。
func TestDevHomeScriptWritesSandboxAndDevEnv(t *testing.T) {
	script, _ := filepath.Abs("../../scripts/dev-home.sh")
	home := filepath.Join(t.TempDir(), "dev")
	cmd := exec.Command("bash", script, "init")
	cmd.Env = append(os.Environ(), "EVERGENIE_DEV_HOME="+home, "EVERGENIE_DEV_PORT=18999")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dev-home.sh init 失敗：%v\n%s", err, out)
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Runtime.Sandbox || !h.IsDev() || h.Runtime.LegacyDev || h.Web.Port != 18999 {
		t.Fatalf("開發家的 [runtime]：%+v", h.Runtime)
	}
}
