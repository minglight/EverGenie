package update

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeEvergenie 寫一支假的 EverGenie：doctor 前 oldTimes 次回舊版本（舊程式還沒退場），之後回 newV；
// packages list 回 pkgs。家資料夾沒有 home.toml，所以 verifyUpgrade 不查看板。
func fakeEvergenie(t *testing.T, oldTimes int, newV, pkgs string) (home, exe string) {
	t.Helper()
	home = t.TempDir()
	exe = filepath.Join(t.TempDir(), "evergenie")
	script := `#!/bin/sh
cnt="` + home + `/doctor-calls"
case "$1" in
doctor)
  n=$(cat "$cnt" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" >"$cnt"
  v="` + newV + `"; [ "$n" -le ` + strconv.Itoa(oldTimes) + ` ] && v="old"
  printf '{"checks":[{"id":"core.version","ok":true,"detail":"核心版本：%s"}]}\n' "$v" ;;
packages)
  cat <<'PKGS'
` + pkgs + `
PKGS
  ;;
esac
`
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, exe
}

const okPackages = `{"id":"habits","state":"running","enabled":true}`

// 重開後舊程式還在回 doctor（版本是舊的）：等到新版本起來就過，不誤判成壞版。
func TestVerifyUpgradeWaitsForNewVersion(t *testing.T) {
	old := verifyWait
	verifyWait = 10 * time.Second
	t.Cleanup(func() { verifyWait = old })
	home, exe := fakeEvergenie(t, 2, "1.2.3", okPackages)
	if err := verifyUpgrade(context.Background(), home, exe, "1.2.3", nil); err != nil {
		t.Fatalf("新版晚兩次才起來也該過：%v", err)
	}
}

// 版本一直不對：時間到照樣回報版本不符（壞版不會被放過）。
func TestVerifyUpgradeVersionNeverMatches(t *testing.T) {
	old := verifyWait
	verifyWait = 1500 * time.Millisecond
	t.Cleanup(func() { verifyWait = old })
	home, exe := fakeEvergenie(t, 9, "1.2.3", okPackages)
	err := verifyUpgrade(context.Background(), home, exe, "1.2.3", nil)
	if err == nil || !strings.Contains(err.Error(), "應該是「1.2.3」") {
		t.Fatalf("版本一直是舊的要回報版本不符，拿到 %v", err)
	}
}

// 停用中的套件帶著原因不算載入失敗；啟用中的官方套件帶 error 照樣算。
func TestVerifyUpgradeIgnoresDisabledPackageErrors(t *testing.T) {
	old := verifyWait
	verifyWait = 2 * time.Second
	t.Cleanup(func() { verifyWait = old })
	disabled := okPackages + "\n" + `{"id":"closet-mine","state":"rejected","enabled":false,"error":"需要套件「closet」，但它已停用"}`
	home, exe := fakeEvergenie(t, 0, "1.2.3", disabled)
	if err := verifyUpgrade(context.Background(), home, exe, "1.2.3", nil); err != nil {
		t.Fatalf("停用的套件不該擋升級：%v", err)
	}
	// 主人自己的、別人分享的套件壞了不是這一版的錯：不擋升級，寫進升級報告（DEV-154）。
	mine := okPackages + "\n" + `{"id":"my-thing","tier":"mine","state":"failed","enabled":true,"error":"載入失敗：連不上"}` + "\n" + `{"id":"theirs","tier":"installed","state":"failed","enabled":true,"error":"載入失敗"}`
	home, exe = fakeEvergenie(t, 0, "1.2.3", mine)
	if err := verifyUpgrade(context.Background(), home, exe, "1.2.3", nil); err != nil {
		t.Fatalf("主人自己的套件壞了不該把升級退回：%v", err)
	}
	broken := okPackages + "\n" + `{"id":"closet","tier":"official","state":"failed","enabled":true,"error":"載入失敗：連不上"}`
	home, exe = fakeEvergenie(t, 0, "1.2.3", broken)
	if err := verifyUpgrade(context.Background(), home, exe, "1.2.3", nil); err == nil || !strings.Contains(err.Error(), "有套件載入失敗") {
		t.Fatalf("啟用中的套件載入失敗要擋，拿到 %v", err)
	}
}

// 升級前就有的紅字（主人資料本來的問題）不算這次升級的錯；內容變了或新出現的照樣擋（DEV-154）。
func TestVerifyUpgradeIgnoresKnownReds(t *testing.T) {
	old := verifyWait
	verifyWait = 2 * time.Second
	t.Cleanup(func() { verifyWait = old })
	home := t.TempDir()
	exe := filepath.Join(t.TempDir(), "evergenie")
	script := "#!/bin/sh\ncase \"$1\" in\ndoctor) printf '{\"checks\":[{\"id\":\"core.version\",\"ok\":true,\"detail\":\"核心版本：1.2.3\"},{\"id\":\"data.invalid\",\"severity\":\"error\",\"detail\":\"有 1 個檔讀不懂\"}]}\\n' ;;\npackages) echo '" + okPackages + "' ;;\nesac\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := verifyUpgrade(ctx, home, exe, "1.2.3", nil); err == nil || !strings.Contains(err.Error(), "有 1 個檔讀不懂") {
		t.Fatalf("沒有基準時紅字要擋：%v", err)
	}
	known := doctorReds(ctx, home, exe)
	if len(known) != 1 {
		t.Fatalf("基準：%v", known)
	}
	if err := verifyUpgrade(ctx, home, exe, "1.2.3", known); err != nil {
		t.Fatalf("升級前就有的紅字不該擋：%v", err)
	}
	if err := verifyUpgrade(ctx, home, exe, "1.2.3", map[string]bool{"data.invalid\x00有 50 個檔讀不懂": true}); err == nil {
		t.Fatal("內容不一樣就是新的問題，要擋")
	}
}
