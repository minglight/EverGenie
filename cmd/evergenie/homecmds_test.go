package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHomeRelayoutCommand(t *testing.T) {
	root := t.TempDir()
	home, state := filepath.Join(root, "h"), filepath.Join(root, "s")
	for rel, body := range map[string]string{
		"home.toml":           "[user]\nname = \"h\"\n",
		"runtime/share/a.png": "a",
		"runtime/logs/x.log":  "x",
		"secrets/push.toml":   "k",
		"personas/luna.md":    "z",
		"personas/mine/m.md":  "m",
	} {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.Local)
	var out, errw bytes.Buffer
	if code := homeRelayout(home, []string{"--state", state}, &out, &errw, now); code != 0 {
		t.Fatalf("預演 code=%d err=%s", code, errw.String())
	}
	for _, want := range []string{"暫存", "主人的檔案", "留在家裡", "personas/mine", "要真的搬請加 --apply"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("預演輸出要有 %q：\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(state); err == nil {
		t.Fatal("預演不該建狀態資料夾")
	}
	if code := homeRelayout(home, []string{"--state", "rel/dir"}, &out, &errw, now); code != 2 {
		t.Errorf("相對路徑的 --state 要是用法錯誤，code=%d", code)
	}
	out.Reset()
	if code := homeRelayout(home, []string{"--state", state, "--apply"}, &out, &errw, now); code != 0 {
		t.Fatalf("apply code=%d err=%s", code, errw.String())
	}
	if !strings.Contains(out.String(), "接下來") || !strings.Contains(out.String(), "evergenie service install --home "+home) {
		t.Errorf("搬完要提醒重裝服務：\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(state, "secrets", "push.toml")); err != nil {
		t.Fatal("密鑰要搬到狀態資料夾")
	}
	// 官方角色預設留在家裡（沒加 --move-personas），home.toml 寫 official_personas = "home"。
	if _, err := os.Stat(filepath.Join(home, "personas", "luna.md")); err != nil {
		t.Fatal("官方角色預設要留在家裡")
	}
	if data, _ := os.ReadFile(filepath.Join(home, "home.toml")); !strings.Contains(string(data), `official_personas = "home"`) {
		t.Errorf("home.toml 要寫 official_personas = \"home\"：%s", data)
	}
	// --revert 沒加 --apply 只預演：什麼都不動。
	if code := homeRelayout(home, []string{"--revert"}, &out, &errw, now); code != 0 || !strings.Contains(out.String(), "這是預演") {
		t.Fatalf("revert 預演 code=%d out=%s err=%s", code, out.String(), errw.String())
	}
	if _, err := os.Stat(filepath.Join(home, "settings", "relayout.json")); err != nil {
		t.Fatal("預演不能動紀錄")
	}
	if code := homeRelayout(home, []string{"--revert", "--apply"}, &out, &errw, now); code != 0 {
		t.Fatalf("revert code=%d err=%s", code, errw.String())
	}
	if data, _ := os.ReadFile(filepath.Join(home, "home.toml")); string(data) != "[user]\nname = \"h\"\n" {
		t.Errorf("退回後 home.toml 要跟原本一樣：%q", data)
	}
}
