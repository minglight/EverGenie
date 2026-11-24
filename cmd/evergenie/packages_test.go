package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// TestPackagesCommandsOnFixtureHome：真的 serve＋fixture 家。官方套件（引擎）照新規則被找到、由 engine 監督（pkg 不編譯不啟動）；
// install-go 沒有 --confirmed 不送出；秘書在主人同意後可以呼叫（原始碼版不下載）；go-status 唯讀、結束碼 0／3。
// Guards: REQ-91, REQ-171
func TestPackagesCommandsOnFixtureHome(t *testing.T) {
	official, err := filepath.Abs(filepath.Join("..", "..", "packages", "official"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", official)
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)

	help := hgOK(t, sock, "packages", "--help")
	for _, sub := range []string{"list", "get", "reload", "updates", "install-go", "--confirmed", "go-status"} {
		if !strings.Contains(help, sub) {
			t.Errorf("packages --help 少了 %s", sub)
		}
	}
	out := hgOK(t, sock, "packages", "get", "engine-claude")
	var p sdk.Package
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if p.Tier != sdk.TierOfficial || p.Kind != sdk.PackageKindProgram || p.State == sdk.PackageStateRejected || p.State == sdk.PackageStateBuilding {
		t.Fatalf("engine-claude：%+v", p)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime", "packages", "engine-claude")); !os.IsNotExist(err) {
		t.Fatal("引擎由 engine 監督，pkg 不能自己編譯或啟動它")
	}
	if list := hgOK(t, sock, "packages", "list"); !strings.Contains(list, `"id":"engine-claude"`) {
		t.Fatalf("packages list：%s", list)
	}

	t.Setenv("EVERGENIE_AGENT", "code")
	if code, _, errOut := hg(t, sock, "", "packages", "install-go"); code != 2 || !strings.Contains(errOut, "先問主人") {
		t.Fatalf("沒有 --confirmed 不能送：%d %q", code, errOut)
	}
	// 這裡跑的是原始碼版（測試執行檔不在 versions/<版本>/bin/）：已經有 Go 就只重編、不下載；沒有就說原始碼版要自己裝，絕對不下載。
	code, out, errOut := hg(t, sock, "", "packages", "install-go", "--confirmed")
	switch {
	case code == 0:
		var r sdk.PackageInstallGoResponse
		if err := json.Unmarshal([]byte(out), &r); err != nil || r.Command != "" || r.Version == "" {
			t.Fatalf("已經有 Go 就不下載：%s %v", out, err)
		}
	case !strings.Contains(errOut, "原始碼版"):
		t.Fatalf("install-go：%d %q", code, errOut)
	}
	// go-status：唯讀；能用結束碼 0、不能用 3，JSON 照樣印；原始碼版不能幫忙下載。
	code, out, _ = hg(t, sock, "", "packages", "go-status")
	var st sdk.PackageGoStatusResponse
	if err := json.Unmarshal([]byte(out), &st); err != nil || st.Installable || st.Need == "" || (code == 0) != st.Usable || (code != 0 && code != 3) {
		t.Fatalf("go-status：%d %s %v", code, out, err)
	}
}
