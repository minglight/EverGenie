package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 建工坊家與搬家腳本（GOAL-2 S3、S4：scripts/workshop-init.sh、scripts/split-workshop.py）的整套情境（scripts/workshop-test.sh，`make workshop-test`）：
// 假的日常家＋假的 EverGenie，不編譯、不起服務：init 的設定與複製、重跑不改、預演不寫、DEV 單轉任務單的狀態與必填欄位、
// GOAL-2 → DG-1 原文不動、記憶不蓋、日常的 vault/admin 搬去備份、第二次 --apply 沒有變動、不認得的狀態停下來。
func TestWorkshopScripts(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("沒有 python3")
	}
	for _, name := range []string{"workshop-init.sh", "workshop-test.sh"} {
		script, _ := filepath.Abs(filepath.Join("..", "..", "scripts", name))
		if out, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
			t.Fatalf("%s 語法不對：%v\n%s", name, err, out)
		}
	}
	script, _ := filepath.Abs("../../scripts/workshop-test.sh")
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "失敗 0") {
		t.Fatalf("workshop-test.sh 沒有全過：%v\n%s", err, out)
	}
}
