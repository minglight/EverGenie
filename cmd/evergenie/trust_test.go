package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

const trustFixture = `schema = 1

[[repo]]
path = "/tmp/hg-trust-repo"
push = false
writers = ["code"]

[deny]
actions = ["send_external"]
`

func writeTrustFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "settings"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings", "trust.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// snapshotFiles 記下家裡 runtime/ 以外每個檔的內容（用來確認只讀指令沒改到設定）。
func snapshotFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			if rel == "runtime" {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// settledFiles 等家裡（runtime 以外）的檔案連續 1 秒沒變才回傳快照。
func settledFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	prev, still := snapshotFiles(t, dir), 0
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
		cur := snapshotFiles(t, dir)
		if reflect.DeepEqual(cur, prev) {
			if still++; still >= 5 {
				return cur
			}
		} else {
			still = 0
		}
		prev = cur
	}
	t.Fatal("家裡的檔案 30 秒內一直在變")
	return nil
}

// Guards: REQ-84, REQ-110
func TestTrustScanReadOnly(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	writeTrustFile(t, dir, trustFixture)
	// fixture 的 R-2 日期在過去，啟動後約 1 秒會發一則到期通知（建立 M-1、再補送達紀錄）；
	// 負載高時它落在下面兩次快照之間，會被當成只讀指令寫的。先等它寫完、家靜下來才拍「之前」。
	waitUntil(t, "啟動時的到期通知", 30*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(dir, "vault/messages/M-1.md"))
		return err == nil
	})
	before := settledFiles(t, dir)

	show := hgOK(t, sock, "trust", "show")
	for _, want := range []string{"code", "最高", "內建預設", "life", "/tmp/hg-trust-repo", "send_external", "其他秘書：最高"} {
		if !strings.Contains(show, want) {
			t.Fatalf("trust show 缺 %q：\n%s", want, show)
		}
	}
	scan := hgOK(t, sock, "trust", "scan")
	if !strings.Contains(scan, "## repo") || !strings.Contains(scan, "共 ") {
		t.Fatalf("trust scan：\n%s", scan)
	}
	if out := hgOK(t, sock, "trust", "scan", "--scope", "秘書 code"); strings.Contains(out, "repo") {
		t.Fatalf("--scope 只留範圍符合的：\n%s", out)
	}
	if out := hgOK(t, sock, "trust", "diff"); !strings.Contains(out, "項正常") {
		t.Fatalf("trust diff：\n%s", out)
	}
	if out := hgOK(t, sock, "trust", "denied", "--days", "3"); !strings.Contains(out, "最近 3 天沒有被擋的動作") {
		t.Fatalf("trust denied：\n%s", out)
	}
	first := hgOK(t, sock, "trust", "snapshot")
	if !strings.Contains(first, "runtime/trust/snapshots/") {
		t.Fatalf("trust snapshot：\n%s", first)
	}
	if second := hgOK(t, sock, "trust", "snapshot"); !strings.Contains(second, "沒有變化") {
		t.Fatalf("沒改設定，第二份快照不該有差異：\n%s", second)
	}
	snaps, err := os.ReadDir(filepath.Join(dir, "runtime", "trust", "snapshots"))
	if err != nil || len(snaps) != 2 {
		t.Fatalf("快照要有兩份：%v %v", snaps, err)
	}

	after := snapshotFiles(t, dir)
	if len(after) != len(before) {
		var diff []string
		for p := range after {
			if _, ok := before[p]; !ok {
				diff = append(diff, "+"+p)
			}
		}
		for p := range before {
			if _, ok := after[p]; !ok {
				diff = append(diff, "-"+p)
			}
		}
		t.Fatalf("只讀指令多出或少了檔案：%d → %d %v", len(before), len(after), diff)
	}
	for p, b := range before {
		if !bytes.Equal(after[p], b) {
			t.Fatalf("只讀指令改到了 %s", p)
		}
	}
}

// Guards: REQ-84, REQ-158
func TestPermissionWizardMapping(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	writeTrustFile(t, dir, trustFixture)
	trust := func() string {
		b, err := os.ReadFile(filepath.Join(dir, "settings", "trust.toml"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// 秘書（有 EVERGENIE_AGENT）不能改權限，檔案不變。
	t.Setenv("EVERGENIE_AGENT", "code")
	if code, _, errOut := hg(t, sock, "", "setup", "permissions", "--choice", "all"); code != 1 || !strings.Contains(errOut, "只有主人") {
		t.Fatalf("秘書改權限要被擋：%d %q", code, errOut)
	}
	if trust() != trustFixture {
		t.Fatal("被擋時設定檔不可以變")
	}
	t.Setenv("EVERGENIE_AGENT", "")

	// 互動：印出三個白話選項，輸入 2 → standard。
	code, out, errOut := hg(t, sock, "2\n", "setup", "permissions")
	if code != 0 {
		t.Fatalf("setup permissions：%d %s", code, errOut)
	}
	for _, want := range []string{"1. 全部讓 AI 幫我做", "2. 做大事先問我", "3. 只讓 AI 看不讓它改", "已更新權限", "舊的設定備份在 runtime/backups/trust/"} {
		if !strings.Contains(out, want) {
			t.Fatalf("精靈輸出缺 %q：\n%s", want, out)
		}
	}
	if s := trust(); !strings.Contains(s, `default_level = "standard"`) || !strings.Contains(s, "/tmp/hg-trust-repo") || !strings.Contains(s, "send_external") {
		t.Fatalf("要寫 default_level 並保留其他欄位：\n%s", s)
	}

	for choice, want := range map[string]sdk.PermissionLevel{"all": sdk.PermissionLevelFull, "read": sdk.PermissionLevelReadOnly, "1": sdk.PermissionLevelFull, "3": sdk.PermissionLevelReadOnly} {
		hgOK(t, sock, "setup", "permissions", "--choice", choice)
		if s := trust(); !strings.Contains(s, `default_level = "`+string(want)+`"`) {
			t.Fatalf("選 %s 要寫 %s：\n%s", choice, want, s)
		}
	}
	if code, _, _ := hg(t, sock, "", "setup", "permissions", "--choice", "9"); code != 2 {
		t.Fatal("不認得的選項是用法錯誤")
	}

	// 單獨改一位，之後整個家的選項不會蓋掉它。
	hgOK(t, sock, "setup", "permissions", "--agent", "life", "--level", "full")
	out = hgOK(t, sock, "setup", "permissions", "--choice", "read")
	if !strings.Contains(out, "life 另外單獨設定過") {
		t.Fatalf("要提醒單獨設定過的秘書：\n%s", out)
	}
	if show := hgOK(t, sock, "trust", "show"); !strings.Contains(show, "life       最高（全部讓 AI 做）　（單獨設定）") || !strings.Contains(show, "code       唯讀") {
		t.Fatalf("trust show：\n%s", show)
	}
	hgOK(t, sock, "setup", "permissions", "--agent", "life", "--level", "clear")
	if strings.Contains(trust(), "[agent.life]") {
		t.Fatalf("clear 要拿掉單獨設定：\n%s", trust())
	}
}
