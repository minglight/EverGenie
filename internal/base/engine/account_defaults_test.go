package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ENG-19：引擎印的帳號設定原樣收下（只收字串與布林、小寫英數底線的鍵）；引擎的子行程抓著輸出不放也不會讓 init 卡住。
func TestAccountDefaultsReadsEngineOutput(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("EVERGENIE_ENGINE_BIN_DIR", bin)
	write := func(script string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(bin, "evergenie-engine-fake"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("#!/bin/sh\n[ \"$1\" = account-defaults ] || exit 2\necho '{\"config_dir\":\"/x/acct\",\"default_login\":true}'\n")
	m, why := AccountDefaults(context.Background(), "fake")
	if why != "" || m["config_dir"] != "/x/acct" || m["default_login"] != true || len(m) != 2 {
		t.Fatalf("%v %q", m, why)
	}
	for _, bad := range []string{"echo '{}'", "echo '{\"n\":1}'", "echo '{\"_x\":\"a\"}'", "echo 'not json'"} {
		write("#!/bin/sh\n" + bad + "\n")
		if m, why := AccountDefaults(context.Background(), "fake"); m != nil || why == "" {
			t.Fatalf("%s：應該不收，拿到 %v", bad, m)
		}
	}
	// 子行程抓著 stdout 不放：等 WaitDelay 後就放棄，不會一直等
	write("#!/bin/sh\nsleep 30 &\necho '{\"config_dir\":\"/x\"}'\n")
	start := time.Now()
	AccountDefaults(context.Background(), "fake")
	if d := time.Since(start); d > accountDefaultsWaitDelay+5*time.Second {
		t.Fatalf("等了 %v", d)
	}
	if m, why := AccountDefaults(context.Background(), "no-such-engine"); m != nil || why == "" {
		t.Fatalf("找不到執行檔：%v %q", m, why)
	}
}
