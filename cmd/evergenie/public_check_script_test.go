package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 公開前檢查與匯出（DEV-158：scripts/public-check.sh、scripts/export-public.sh）的整套情境（scripts/public-check-test.sh，`make public-check-test`）：
// 暫存資料夾裡自己建檔案樹與 git repo、假 HOME、假的私人清單，不讀也不寫真的 ~/.evergenie-release/：每種規則都抓得到、輸出不印私人的字、
// 示範寫法放過、放行與排除清單的格式、匯出只抄該抄的再檢查一次。
func TestPublicCheckScript(t *testing.T) {
	for _, name := range []string{"public-check.sh", "export-public.sh", "public-check-test.sh"} {
		script, _ := filepath.Abs(filepath.Join("..", "..", "scripts", name))
		if out, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
			t.Fatalf("%s 語法不對：%v\n%s", name, err, out)
		}
	}
	script, _ := filepath.Abs("../../scripts/public-check-test.sh")
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "失敗 0") {
		t.Fatalf("public-check-test.sh 沒有全過：%v\n%s", err, out)
	}
}
