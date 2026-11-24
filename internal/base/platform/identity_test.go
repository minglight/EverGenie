package platform

import (
	"errors"
	"testing"
)

func fixedHostname(t *testing.T, name string) {
	t.Helper()
	old := hostname
	hostname = func() (string, error) { return name, nil }
	t.Cleanup(func() { hostname = old })
}

// 沒寫身分：env＝prod、owner＝user.name 轉小寫、machine＝這台的短主機名；標籤是 <owner>-<machine>，沒有 @dev。
func TestHomeIdentityDefaults(t *testing.T) {
	fixedHostname(t, "My-Mini.local")
	h, err := LoadHome(writeIDsHome(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if h.Runtime.Env != EnvProd || h.IsDev() || h.Runtime.Sandbox {
		t.Fatalf("預設要是正式家、不是沙盒：%+v", h.Runtime)
	}
	if h.Runtime.Owner != "測試使用者" || h.Runtime.Machine != "my-mini" {
		t.Fatalf("owner／machine 預設：%q %q", h.Runtime.Owner, h.Runtime.Machine)
	}
	if got := h.Label(); got != "測試使用者-my-mini" {
		t.Fatalf("標籤：%q", got)
	}
}

// user.name 轉 owner：小寫、空白換成 -。
func TestIdentitySlugLowercasesAndJoinsWords(t *testing.T) {
	for in, want := range map[string]string{"Amy": "amy", " Amy Doe ": "amy-doe", "小明": "小明", "a@b": "ab"} {
		if got := identitySlug(in); got != want {
			t.Errorf("%q → %q，要 %q", in, got, want)
		}
	}
}

// 寫了 env = "dev" 的家：標籤加 @dev；只是標記，不會變成沙盒（推播、升級照常）。
func TestHomeIdentityDevLabel(t *testing.T) {
	fixedHostname(t, "x")
	h, err := LoadHome(writeIDsHome(t, "[runtime]\nenv=\"dev\"\nowner=\"amy\"\nmachine=\"home\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Label(); got != "amy-home@dev" || !h.IsDev() || h.Runtime.Sandbox {
		t.Fatalf("開發家：%q sandbox=%v", got, h.Runtime.Sandbox)
	}
	for _, k := range []string{"env", "owner", "machine", "sandbox", "remote_suffix"} {
		if _, ok := h.Runtime.Overrides[k]; ok {
			t.Fatalf("%s 是核心自己的欄位，不交給引擎：%v", k, h.Runtime.Overrides)
		}
	}
}

// 舊的 [runtime] dev = true 讀成 sandbox = true 加 env = "dev"（相容）；有明寫 env 時以明寫的為準。
func TestHomeLegacyDevMeansSandboxAndDevEnv(t *testing.T) {
	fixedHostname(t, "x")
	h, err := LoadHome(writeIDsHome(t, "[runtime]\ndev=true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !h.Runtime.Sandbox || h.Runtime.Env != EnvDev {
		t.Fatalf("dev = true 要變成 sandbox＋env=dev：%+v", h.Runtime)
	}
	h, err = LoadHome(writeIDsHome(t, "[runtime]\ndev=true\nenv=\"prod\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !h.Runtime.Sandbox || h.Runtime.Env != EnvProd {
		t.Fatalf("明寫 env 優先：%+v", h.Runtime)
	}
	h, err = LoadHome(writeIDsHome(t, "[runtime]\nsandbox=true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !h.Runtime.Sandbox || h.Runtime.Env != EnvProd {
		t.Fatalf("只寫 sandbox 不改 env：%+v", h.Runtime)
	}
}

// 已退休的 remote_suffix 讀到忽略：家照常載入、不交給引擎。
func TestHomeRemoteSuffixIgnored(t *testing.T) {
	h, err := LoadHome(writeIDsHome(t, "[runtime]\nremote_suffix=\"ws\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Runtime.Overrides["remote_suffix"]; ok {
		t.Fatalf("remote_suffix 不交給引擎：%v", h.Runtime.Overrides)
	}
}

// 身分欄位寫錯，家載不起來並指出欄位。
func TestHomeIdentityRejectsBadValues(t *testing.T) {
	for extra, field := range map[string]string{
		"[runtime]\nenv=\"staging\"\n":   "runtime.env",
		"[runtime]\nowner=\"amy lin\"\n": "runtime.owner",
		"[runtime]\nmachine=\"a@b\"\n":   "runtime.machine",
	} {
		_, err := LoadHome(writeIDsHome(t, extra))
		var e *Err
		if !errors.As(err, &e) || e.Field != field {
			t.Fatalf("%q：要指出 %s，得到 %v", extra, field, err)
		}
	}
}
