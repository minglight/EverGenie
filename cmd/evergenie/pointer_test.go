package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func pointerCheckout(t *testing.T, content string) string {
	t.Helper()
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(checkout, platform.PointerFile), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(checkout, "bin", "evergenie")
}

// 沒設 EVERGENIE_HOME：用執行檔所在 checkout 的指標檔找家。
func TestPointerFileSetsHomeWhenNoEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EVERGENIE_HOME", "")
	applyPointerHome(pointerCheckout(t, "home = \""+home+"\"\n"), &bytes.Buffer{})
	if got := sdk.Getenv("EVERGENIE_HOME"); got != home {
		t.Fatalf("EVERGENIE_HOME 要是指標檔的家：%q", got)
	}
}

// EVERGENIE_HOME 已經設了就不看指標檔（EVERGENIE_HOME 優先）。
func TestPointerFileLosesToEnv(t *testing.T) {
	t.Setenv("EVERGENIE_HOME", "/env/home")
	applyPointerHome(pointerCheckout(t, "home = \"/pointer/home\"\n"), &bytes.Buffer{})
	if got := sdk.Getenv("EVERGENIE_HOME"); got != "/env/home" {
		t.Fatalf("EVERGENIE_HOME 優先：%q", got)
	}
}

// 沒有指標檔：什麼都不設（各指令照原本的行為）；指標檔壞了：只警告、不設。
func TestPointerFileMissingOrBroken(t *testing.T) {
	t.Setenv("EVERGENIE_HOME", "")
	applyPointerHome(pointerCheckout(t, ""), &bytes.Buffer{})
	if got := sdk.Getenv("EVERGENIE_HOME"); got != "" {
		t.Fatalf("沒有指標檔不設：%q", got)
	}
	var errOut bytes.Buffer
	applyPointerHome(pointerCheckout(t, "nothing = 1\n"), &errOut)
	if got := sdk.Getenv("EVERGENIE_HOME"); got != "" || !strings.Contains(errOut.String(), platform.PointerFile) {
		t.Fatalf("壞掉的指標檔只警告：%q %q", got, errOut.String())
	}
}

// --home 旗標贏過 EVERGENIE_HOME（serve 也一樣）：給了 --home 就不看環境變數；沒給才用 EVERGENIE_HOME。
func TestServeHomeFlagBeatsEnv(t *testing.T) {
	t.Setenv("EVERGENIE_HOME", filepath.Join(t.TempDir(), "沒有這個家"))
	var out, errOut bytes.Buffer
	if code := serve([]string{"extra"}, &out, &errOut); code != 2 {
		t.Fatalf("多餘參數要回用法錯誤：%d", code)
	}
	// 沒給 --home：用 EVERGENIE_HOME 那個（不存在的）家，啟動失敗而不是用法錯誤。
	errOut.Reset()
	if code := serve(nil, &out, &errOut); code != 1 {
		t.Fatalf("沒給 --home 要用 EVERGENIE_HOME：%d %s", code, errOut.String())
	}
	t.Setenv("EVERGENIE_HOME", "")
	errOut.Reset()
	if code := serve(nil, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "evergenie.local.toml") {
		t.Fatalf("都沒有：用法錯誤並說明三種給法：%d %s", code, errOut.String())
	}
}
