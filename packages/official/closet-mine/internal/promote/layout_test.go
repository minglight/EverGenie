package promote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// TestMain 先清掉家的佈局變數：測試可能在秘書的 session 裡跑（核心會帶 EVERGENIE_STATE 等），其他測試都假設第 1 版的位置。
func TestMain(m *testing.M) {
	for _, k := range sdk.EnvPrefixes(sdk.EnvState, sdk.EnvMedia, sdk.EnvOfficialPersonas, sdk.EnvBundledPersonas) {
		os.Unsetenv(k)
	}
	os.Exit(m.Run())
}

// 第 2 版的家（DEV-145）：官方角色卡與官方圖在家外面的官方角色資料夾；升官方把圖寫到那裡（Move.To 是絕對路徑，檔名不改），
// 主人的圖仍在 <家>/personas/mine/avatars/ 改名成備份；退回的步驟把備份改回原名。
func TestPromoteLayoutV2(t *testing.T) {
	h, v := promoteHome(t)
	official := filepath.Join(t.TempDir(), "personas")
	if err := os.Rename(filepath.Join(h, "personas", "avatars"), filepath.Join(h, "official-avatars")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"avatars", ""} {
		if err := os.MkdirAll(filepath.Join(official, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(filepath.Join(h, "personas", "athena.md"), filepath.Join(official, "athena.md")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(sdk.EnvOfficialPersonas, official)
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.Local)
	res, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(official, "avatars", "full", "athena@my-12.png")); string(b) != "mine full/athena@my-12.png" {
		t.Fatalf("官方圖要寫到官方角色資料夾：%q", b)
	}
	if _, err := os.Stat(filepath.Join(h, "personas", "avatars")); !os.IsNotExist(err) {
		t.Fatal("第 2 版不該在家裡建 personas/avatars/")
	}
	if readRel(h, "personas/mine/avatars/athena@my-12.png.bak-20261107-promote") != "mine athena@my-12.png" {
		t.Fatal("主人的舊圖要在家裡改名成備份")
	}
	for _, m := range res.Files {
		if !filepath.IsAbs(m.To) || !strings.HasPrefix(m.To, official) || filepath.IsAbs(m.From) {
			t.Fatalf("Move：%+v", m)
		}
	}
	undo := strings.Join(res.Undo, "\n")
	if !strings.Contains(undo, "改回 "+filepath.Join("personas", "mine", "avatars", "full", "athena@my-12.png")) {
		t.Fatalf("退回要把備份改回 personas/mine/avatars/：%s", undo)
	}
	// 官方角色卡不在官方角色資料夾（只在家裡）就不算官方角色。
	if err := os.Rename(filepath.Join(official, "athena.md"), filepath.Join(h, "personas", "athena.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{Home: h, Target: "athena-my-13", NewID: "x", Vault: v, Now: now}); err == nil || !strings.Contains(err.Error(), "不是官方角色") {
		t.Fatalf("要拒絕：%v", err)
	}
}

// TestPromoteBundledCard：「是不是官方角色」走找官方卡的共用入口（sdk.OfficialCardPath，PRS-25）——官方角色資料夾沒有她的卡、
// 只有跟著程式的那張，也算官方角色；兩邊都沒有就拒絕。
func TestPromoteBundledCard(t *testing.T) {
	h, v := promoteHome(t)
	bundled := t.TempDir()
	if err := os.Rename(filepath.Join(h, "personas", "athena.md"), filepath.Join(bundled, "athena.md")); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.Local)
	if _, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v, Now: now, DryRun: true}); err == nil || !strings.Contains(err.Error(), "不是官方角色") {
		t.Fatalf("兩邊都沒有她的卡要拒絕：%v", err)
	}
	t.Setenv(sdk.EnvBundledPersonas, bundled)
	if _, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v, Now: now, DryRun: true}); err != nil {
		t.Fatalf("只有跟著程式的卡也算官方角色：%v", err)
	}
}

// TestPromoteRefusesMineCardOverBundled：主人自己有同代號的卡蓋過跟著程式的公開卡時（agents 的 PRS-27），那個代號是主人的，
// 不算官方角色，衣服不能升成官方。
func TestPromoteRefusesMineCardOverBundled(t *testing.T) {
	h, v := promoteHome(t)
	bundled := t.TempDir()
	if err := os.Rename(filepath.Join(h, "personas", "athena.md"), filepath.Join(bundled, "athena.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, "personas", "mine", "athena.md"), []byte("---\nname: 我的雅典娜\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(sdk.EnvBundledPersonas, bundled)
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.Local)
	if _, err := Run(context.Background(), Options{Home: h, Target: "athena-my-12", NewID: "seaside", Vault: v, Now: now, DryRun: true}); err == nil || !strings.Contains(err.Error(), "不是官方角色") {
		t.Fatalf("主人的卡蓋過程式帶的那張：不算官方角色，要拒絕：%v", err)
	}
}
