package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNightCLIRoundTrip：真的 serve＋fixture 家。第一次 add 照範本建檔；list／done／block／rm 的序號與搬段；
// 內容真的在 vault/agents/system/notes/night-queue.md；變更紀錄有記誰改的。
// Guards: REQ-19, REQ-111
func TestNightCLIRoundTrip(t *testing.T) {
	dir := doctorHome(t)
	_, sock := startDoctorAPI(t, dir)
	t.Setenv("EVERGENIE_HOME", dir)
	t.Setenv("EVERGENIE_AGENT", "")

	if out := hgOK(t, sock, "night", "--help"); !strings.Contains(out, "block <n>") || !strings.Contains(out, "rm <n>") {
		t.Fatalf("night --help：%q", out)
	}
	// 還沒有檔案：list 也能看（空的三段）。
	if out := hgOK(t, sock, "night", "list"); !strings.Contains(out, "待做（0）") || !strings.Contains(out, "要主人（0）") || !strings.Contains(out, "做完（0）") {
		t.Fatalf("空清單：%q", out)
	}
	path := filepath.Join(dir, "vault", "agents", "system", "notes", "night-queue.md")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("list 不該建檔")
	}

	hgOK(t, sock, "night", "add", "整理 A")
	hgOK(t, sock, "night", "add", "補測試 B")
	hgOK(t, sock, "night", "add", "寫文件 C")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "# 夜班清單") || !strings.Contains(text, "## 待做\n- [ ] 整理 A｜加入 ") || !strings.Contains(text, "## 要主人") || !strings.Contains(text, "## 做完（留最近 14 天）") {
		t.Fatalf("檔案內容不對：\n%s", text)
	}
	if !strings.Contains(text, "\n主人排好、要在") || !strings.Contains(text, "要主人決定") {
		t.Fatalf("說明一律寫「主人」，不寫人名：\n%s", text)
	}
	if out := hgOK(t, sock, "night", "list"); !strings.Contains(out, "1. 整理 A") || !strings.Contains(out, "3. 寫文件 C") {
		t.Fatalf("list：%q", out)
	}

	hgOK(t, sock, "night", "done", "1", "--result", "做完了，在 notes/a.md")
	hgOK(t, sock, "night", "block", "1", "--reason", "要 sudo")
	out := hgOK(t, sock, "night", "list")
	for _, want := range []string{"待做（1）", "1. 寫文件 C", "要主人（1）", "補測試 B｜加入", "卡住：要 sudo", "做完（1）", "整理 A｜加入", "結果：做完了，在 notes/a.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("list 少了 %q：\n%s", want, out)
		}
	}
	hgOK(t, sock, "night", "rm", "1")
	if out = hgOK(t, sock, "night", "list"); !strings.Contains(out, "待做（0）") {
		t.Fatalf("rm 之後：\n%s", out)
	}

	// 錯誤：序號超出、沒附結果、不認得的子指令。
	if code, _, errOut := hg(t, sock, "", "night", "done", "1", "--result", "x"); code != 1 || !strings.Contains(errOut, "空的") {
		t.Fatalf("待做是空的：%d %q", code, errOut)
	}
	if code, _, _ := hg(t, sock, "", "night", "add"); code != 2 {
		t.Fatalf("add 沒寫內容是用法錯誤：%d", code)
	}
	if code, _, _ := hg(t, sock, "", "night", "done", "x", "--result", "好"); code != 2 {
		t.Fatalf("序號不是數字是用法錯誤：%d", code)
	}
	if code, _, _ := hg(t, sock, "", "night", "frob"); code != 2 {
		t.Fatalf("不認得的子指令：%d", code)
	}

	// 變更紀錄有記誰改的（owner(cli)）。
	logs, _ := filepath.Glob(filepath.Join(dir, "runtime", "logs", "changes-*.jsonl"))
	var all strings.Builder
	for _, l := range logs {
		b, _ := os.ReadFile(l)
		all.Write(b)
	}
	if !strings.Contains(all.String(), `"by":"owner(cli)","id":"system/night-queue","kind":"note"`) {
		t.Fatalf("變更紀錄：\n%s", all.String())
	}

	// 秘書自己改：by 是它的職位 id。
	t.Setenv("EVERGENIE_AGENT", "system")
	hgOK(t, sock, "night", "add", "秘書放的")
	logs, _ = filepath.Glob(filepath.Join(dir, "runtime", "logs", "changes-*.jsonl"))
	all.Reset()
	for _, l := range logs {
		b, _ := os.ReadFile(l)
		all.Write(b)
	}
	if !strings.Contains(all.String(), `"by":"system","id":"system/night-queue","kind":"note"`) {
		t.Fatalf("秘書的變更紀錄：\n%s", all.String())
	}
}
