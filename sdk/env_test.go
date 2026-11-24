package sdk

import (
	"os"
	"reflect"
	"testing"
)

// unsetenv 拿掉 name，測完還原（t.Setenv 先記下原本的值）。
func unsetenv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

// 新名字有設就用新名字（設成空字串也算有設）；沒設才讀改名前的舊名；不是 EVERGENIE_ 開頭的照原樣讀。
func TestGetenvFallsBackToLegacyName(t *testing.T) {
	const name, legacy = "EVERGENIE_ENVTEST", "HG_ENVTEST" // 讀舊名（legacy）
	unsetenv(t, name)
	unsetenv(t, legacy)
	if got := Getenv(name); got != "" {
		t.Fatalf("都沒設：%q", got)
	}
	t.Setenv(legacy, "/舊的")
	if got := Getenv(name); got != "/舊的" {
		t.Fatalf("只有舊名：%q，要讀舊名", got)
	}
	t.Setenv(name, "/新的")
	if got := Getenv(name); got != "/新的" {
		t.Fatalf("新舊都有：%q，要用新名字", got)
	}
	t.Setenv(name, "")
	if got := Getenv(name); got != "" {
		t.Fatalf("新名字設成空字串：%q，要當成有設（不讀舊名）", got)
	}
	t.Setenv("ENVTEST_OTHER", "x")
	if got := Getenv("ENVTEST_OTHER"); got != "x" {
		t.Fatalf("別的變數照原樣讀：%q", got)
	}
}

func TestEnvNameHelpers(t *testing.T) {
	if got := LegacyEnvName("EVERGENIE_HOME"); got != "HG_HOME" { // 讀舊名（legacy）
		t.Fatalf("LegacyEnvName：%q", got)
	}
	for _, n := range []string{"HOME", "EVERGENIE_", ""} {
		if got := LegacyEnvName(n); got != "" {
			t.Fatalf("LegacyEnvName(%q) 要空字串：%q", n, got)
		}
	}
	if got := CurrentEnvName("HG_SESSION"); got != "EVERGENIE_SESSION" { // 讀舊名（legacy）
		t.Fatalf("CurrentEnvName：%q", got)
	}
	if got := CurrentEnvName("PATH"); got != "PATH" {
		t.Fatalf("CurrentEnvName 別的名字要原樣：%q", got)
	}
	if !IsEnv("EVERGENIE_ENGINE_SOCKET", "EVERGENIE_ENGINE_SOCKET") || !IsEnv("HG_ENGINE_SOCKET", "EVERGENIE_ENGINE_SOCKET") || IsEnv("HG_ENGINE_SOCKETX", "EVERGENIE_ENGINE_SOCKET") { // 讀舊名（legacy）
		t.Fatal("IsEnv")
	}
	if !IsProductEnv("EVERGENIE_HOME") || !IsProductEnv("HG_HOME") || IsProductEnv("HOME") { // 讀舊名（legacy）
		t.Fatal("IsProductEnv")
	}
	got := EnvPrefixes("EVERGENIE_HOME=", "EVERGENIE_PACKAGE_", "PATH=")
	want := []string{"EVERGENIE_HOME=", "EVERGENIE_PACKAGE_", "PATH=", "HG_HOME=", "HG_PACKAGE_"} // 讀舊名（legacy）
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EnvPrefixes：%q，要 %q", got, want)
	}
}

// 家的佈局變數也走 Getenv：搬家前開著的 session 只有舊名，照樣找得到狀態資料夾。
func TestLayoutReadsLegacyEnv(t *testing.T) {
	unsetenv(t, EnvState)
	t.Setenv(LegacyEnvName(EnvState), "/state")
	if got := RuntimeDir("/home", "x"); got != "/state/runtime/x" {
		t.Fatalf("RuntimeDir：%q", got)
	}
}
