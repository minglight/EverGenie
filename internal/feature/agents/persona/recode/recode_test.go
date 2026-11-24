package recode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
)

// 測試只用 t.TempDir()：第 1 版的家（暫存在家裡的 runtime/）；要第 2 版就在 home.toml 明寫 [layout] state 到暫存資料夾，
// 絕不碰真的 ~/.evergenie/state。

var now = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("讀不到 %s：%v", p, err)
	}
	return string(data)
}

// snapshot 記下 root 底下每個檔（內容雜湊與權限）與資料夾；退回後要逐項相同。
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == root {
				return fs.SkipAll
			}
			return err
		}
		rel, _ := filepath.Rel(root, p)
		info, _ := d.Info()
		if d.Type()&fs.ModeSymlink != 0 {
			to, _ := os.Readlink(p)
			out[rel] = "link " + to
			return nil
		}
		if d.IsDir() {
			out[rel] = "dir " + info.Mode().Perm().String()
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		h := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(h[:]) + " " + info.Mode().Perm().String()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, what string, want, got map[string]string) {
	t.Helper()
	if reflect.DeepEqual(want, got) {
		return
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s：%s 不一樣（之前 %q，現在 %q）", what, k, v, got[k])
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s：多了 %s", what, k)
		}
	}
}

// 假的套件宣告：跟衣櫃一樣的寫法（組裝點平常從 package.toml 讀來）；photo 是照片紀錄（file 指著檔，DEV-160）。
var testKinds = []Kind{
	{Kind: "wardrobe.outfit", Field: "persona", ID: "prefix", Prefixed: []string{"from"}, Source: "source"},
	{Kind: "wardrobe.rotation", ID: "exact"},
	{Kind: "wardrobe.note", Field: "persona", Prefixed: []string{"record"}},
	{Kind: "wardrobe.photo", Field: "persona", ID: "prefix", Prefixed: []string{"outfit", "of"}, Source: "source", File: "file"},
}

// photoRec 是一筆照片紀錄的檔（vault/pkg/wardrobe/photo/<id>.md）。
func photoRec(id, persona, outfit, source, file string) string {
	return "---\nid: " + id + "\npersona: " + persona + "\noutfit: " + outfit + "\nsource: " + source + "\nfile: " + file + "\n---\n"
}

// baseTOML 是能通過 platform.LoadHome 的最小設定（不含 [web]）。
const baseTOML = `[user]
name = "h"
timezone = "Asia/Taipei"

[slots]
default = "life"

[slots.life]
label = "生活"
remind = "08:30"
check = "18:00"

[accounts]
default = "main"

[accounts.main]
engine = "test-engine"
`

const homeTOML = baseTOML + `
[web]
port = 18850
app_icon = "mika"   # 看板圖示

[defaults]
persona = 'mikako'
`

const webTOML = "\n[web]\nport = 18850\n"

// mineHome 是第 1 版的家：兩位自己的角色 mika、mikako（代號一個是另一個的開頭，測邊界），一位官方 luna。
func mineHome(t *testing.T) string {
	home := filepath.Join(t.TempDir(), "home")
	write(t, home, map[string]string{
		"home.toml":                                             homeTOML,
		"personas/luna.md":                                      "官方",
		"personas/avatars/luna.png":                             "z",
		"personas/mine/mika.md":                                 "---\nname: Mika\n---\n我是 mika\n",
		"personas/mine/mikako.md":                               "---\nname: Mikako\n---\n",
		"personas/mine/_style.md":                               "說話層",
		"personas/mine/mika.md.bak-20260101-old":                "舊卡",
		"personas/mine/avatars/mika.png":                        "y1",
		"personas/mine/avatars/mika@full.png":                   "y2",
		"personas/mine/avatars/mika~casual.png":                 "y3",
		"personas/mine/avatars/full/mika+n.png":                 "y4",
		"personas/mine/avatars/mika2.png":                       "不是 mika 的",
		"personas/mine/avatars/mikako.png":                      "k1",
		"personas/mine/avatars/mikako~x.png":                    "k2",
		"personas/mine/adult/mika.png":                          "a1",
		"vault/agents/life/agent.md":                            "---\nid: life\npersona: mika\n---\n職責\n",
		"vault/agents/health/agent.md":                          "---\nid: health\npersona: mikako # 註解\n---\n",
		"vault/agents/work/agent.md":                            "---\nid: work\npersona: luna\n---\n",
		"vault/pkg/wardrobe/outfit/mika-casual.md":              "---\nid: mika-casual\npersona: mika\nfrom: mika-base\nsource: mine\ntitle: \"Casual\"\n---\n衣服 mika\n",
		"vault/pkg/wardrobe/outfit/mikako-x.md":                 "---\nid: mikako-x\npersona: mikako\nsource: mine\n---\n",
		"vault/pkg/wardrobe/outfit/luna-a.md":                   "---\nid: luna-a\npersona: luna\nfrom: mika-base\nsource: official\n---\n",
		"vault/pkg/wardrobe/outfit/archive/2026-10/mika-old.md": "---\nid: mika-old\npersona: mika\n---\n",
		"vault/pkg/wardrobe/rotation/mika.md":                   "---\nid: mika\nlist:\n  - a\n  - b\n---\n",
		"vault/pkg/wardrobe/note/CN-1.md":                       "---\nid: CN-1\npersona: \"mika\"\nrecord: 'mika-casual'\n---\n",
		"vault/pkg/wardrobe/photo/mika-casual--half1.md":        photoRec("mika-casual--half1", "mika", "mika-casual", "mine", "avatars/mika~casual.png"),
		"vault/pkg/wardrobe/photo/mika-base--half1.md":          photoRec("mika-base--half1", "mika", "mika-base", "mine", "avatars/mika.png"),
		"vault/pkg/wardrobe/photo/mika-base--full1.md":          photoRec("mika-base--full1", "mika", "mika-base", "mine", "avatars/full/mika+n.png"),
		"vault/pkg/wardrobe/photo/mika-base--half1-h.md":        photoRec("mika-base--half1-h", "mika", "mika-base", "mine", "adult/mika.png"),
		"vault/pkg/wardrobe/photo/mikako-x--half1.md":           photoRec("mikako-x--half1", "mikako", "mikako-x", "mine", "avatars/mikako~x.png"),
		"vault/notes/today.md":                                  "今天跟 mika 聊天\nmikako 也在\n",
		"settings/packages.toml":                                "[claims.life]\npersona = \"mika\"\n\n[claims.work]\npersona = \"luna\"\n",
		"settings/persona-import.toml":                          "schema = 1\n\n[files]\n\"personas/mine/mika.md\" = \"abc\"\n\"personas/mine/avatars/mika.png\" = \"def\"\n\n[records]\n\"wardrobe.outfit/mika-casual\" = \"123\"\n",
	})
	return home
}

func TestTidyRenamesMineAndReverts(t *testing.T) {
	home := mineHome(t)
	before := snapshot(t, home)
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(td.Problems) > 0 {
		t.Fatalf("不該有問題：%v", td.Problems)
	}
	sameTree(t, "預演不動檔", before, snapshot(t, home))
	if got := len(td.Mine); got != 2 {
		t.Fatalf("要改兩位自己的角色，得到 %v", td.Mine)
	}
	text := td.Text(false)
	for _, want := range []string{"mika → my_mika", "mikako → my_mikako", "luna", "預演", "mika.md.bak-20260101-old", "vault/notes/today.md:1"} {
		if !strings.Contains(text, want) {
			t.Errorf("預演要有 %q：\n%s", want, text)
		}
	}
	if err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now); err != nil {
		t.Fatal(err)
	}

	// 卡：內容原樣、改名。
	if got := read(t, filepath.Join(home, "personas/mine/my_mika.md")); got != "---\nname: Mika\n---\n我是 mika\n" {
		t.Errorf("卡內容要原樣：%q", got)
	}
	// 圖一律不改名（DEV-160：檔名沒有意思）：照片紀錄的角色、衣服、編號換成新代號，file 照舊指著原檔。
	for _, rel := range []string{"avatars/mika.png", "avatars/mika@full.png", "avatars/mika~casual.png", "avatars/full/mika+n.png",
		"avatars/mika2.png", "avatars/mikako.png", "avatars/mikako~x.png", "adult/mika.png", "mika.md.bak-20260101-old", "_style.md"} {
		if _, err := os.Stat(filepath.Join(home, "personas/mine", rel)); err != nil {
			t.Errorf("要有 personas/mine/%s", rel)
		}
	}
	for _, rel := range []string{"mika.md", "avatars/my_mika.png", "avatars/my_mikako~x.png", "adult/my_mika.png"} {
		if _, err := os.Stat(filepath.Join(home, "personas/mine", rel)); err == nil {
			t.Errorf("不該有 personas/mine/%s", rel)
		}
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/photo/my_mika-casual--half1.md")); got != photoRec("my_mika-casual--half1", "my_mika", "my_mika-casual", "mine", "avatars/mika~casual.png") {
		t.Errorf("照片紀錄：%q", got)
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/photo/my_mikako-x--half1.md")); !strings.Contains(got, "file: avatars/mikako~x.png\n") {
		t.Errorf("mikako 的照片紀錄：%q", got)
	}
	// 沒有照片紀錄指著、檔名是舊代號開頭的列出來（不動）；紀錄的 file 那一行不算「還寫著舊代號」。
	if !strings.Contains(text, "personas/mine/avatars/mika@full.png　（檔名是舊代號 mika 開頭、沒有照片紀錄指著") || strings.Contains(text, "personas/mine/avatars/mika.png　（") {
		t.Errorf("沒有紀錄指著的圖要列出來、有紀錄的不列：\n%s", text)
	}
	if strings.Contains(text, "wardrobe/photo/") {
		t.Errorf("照片紀錄的 file 不算還寫著舊代號：\n%s", text)
	}
	// 名冊：只改那一行，註解留著。
	if got := read(t, filepath.Join(home, "vault/agents/health/agent.md")); got != "---\nid: health\npersona: my_mikako # 註解\n---\n" {
		t.Errorf("名冊：%q", got)
	}
	if got := read(t, filepath.Join(home, "vault/agents/work/agent.md")); !strings.Contains(got, "persona: luna") {
		t.Errorf("官方的不動：%q", got)
	}
	// 套件紀錄：編號（檔名與 id）、persona、帶前綴的欄位；引號照原樣；source 不動（自己的角色本來就是 mine）。
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/outfit/my_mika-casual.md")); got != "---\nid: my_mika-casual\npersona: my_mika\nfrom: my_mika-base\nsource: mine\ntitle: \"Casual\"\n---\n衣服 mika\n" {
		t.Errorf("衣服紀錄：%q", got)
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/outfit/my_mikako-x.md")); !strings.Contains(got, "id: my_mikako-x\npersona: my_mikako\n") {
		t.Errorf("mikako 的衣服：%q", got)
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/outfit/luna-a.md")); !strings.Contains(got, "from: my_mika-base") || !strings.Contains(got, "persona: luna") {
		t.Errorf("別人的紀錄帶前綴的欄位也要跟著改：%q", got)
	}
	if _, err := os.Stat(filepath.Join(home, "vault/pkg/wardrobe/outfit/archive/2026-10/mika-old.md")); err != nil {
		t.Error("封存的不動")
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/rotation/my_mika.md")); !strings.HasPrefix(got, "---\nid: my_mika\nlist:") {
		t.Errorf("輪替：%q", got)
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/note/CN-1.md")); got != "---\nid: CN-1\npersona: \"my_mika\"\nrecord: 'my_mika-casual'\n---\n" {
		t.Errorf("便條：%q", got)
	}
	// 設定：只改值，註解、引號、排列留著。
	if got := read(t, filepath.Join(home, "home.toml")); got != strings.NewReplacer(`"mika"   #`, `"my_mika"   #`, `'mikako'`, `'my_mikako'`).Replace(homeTOML) {
		t.Errorf("home.toml：%q", got)
	}
	if got := read(t, filepath.Join(home, "settings/packages.toml")); got != "[claims.life]\npersona = \"my_mika\"\n\n[claims.work]\npersona = \"luna\"\n" {
		t.Errorf("packages.toml：%q", got)
	}
	if got := read(t, filepath.Join(home, "settings/persona-import.toml")); !strings.Contains(got, "\"personas/mine/my_mika.md\" = \"abc\"") ||
		!strings.Contains(got, "\"personas/mine/avatars/mika.png\" = \"def\"") || !strings.Contains(got, "\"wardrobe.outfit/my_mika-casual\" = \"123\"") {
		t.Errorf("匯入登記：%q", got)
	}
	if _, err := os.Stat(TidyJournal(home)); err != nil {
		t.Fatal("要有紀錄檔")
	}

	// 再預演一次：沒有要做的。
	again, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Steps) != 0 || len(again.Mine) != 0 {
		t.Errorf("整理完再預演要是空的：%d 步 %v", len(again.Steps), again.Mine)
	}

	// 退回：逐檔相同（紀錄檔、原文、這次建的資料夾都拿掉）。
	rp, err := PlanRevert(TidyJournal(home))
	if err != nil || len(rp.Problems) > 0 {
		t.Fatalf("退回預演：%v %v", err, rp)
	}
	if err := Revert(TidyJournal(home)); err != nil {
		t.Fatal(err)
	}
	sameTree(t, "退回後", before, snapshot(t, home))
}

func TestTidyRefusesExistingDestination(t *testing.T) {
	home := mineHome(t)
	write(t, home, map[string]string{"vault/pkg/wardrobe/outfit/my_mika-casual.md": "---\nid: my_mika-casual\n---\n"})
	before := snapshot(t, home)
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(td.Problems) == 0 || !strings.Contains(strings.Join(td.Problems, "\n"), "my_mika-casual.md 已經有了") {
		t.Fatalf("目的檔已經有了要整個不做：%v", td.Problems)
	}
	if err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now); err == nil {
		t.Fatal("有問題要拒絕套用")
	}
	sameTree(t, "拒絕後", before, snapshot(t, home))
}

func TestTidyRollsBackMidway(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, home string, td *Tidy)
	}{
		{"第幾步前失敗", func(t *testing.T, home string, td *Tidy) {
			failAt = len(td.Steps) - 2 // 改名都做了、內容改了一些
			t.Cleanup(func() { failAt = 0 })
		}},
		{"改名失敗", func(t *testing.T, home string, td *Tidy) {
			renameFile = func(from, to string) error {
				if strings.HasSuffix(to, "my_mikako-x--half1.md") {
					return errors.New("磁碟壞了")
				}
				return os.Rename(from, to)
			}
			t.Cleanup(func() { renameFile = os.Rename })
		}},
		{"寫檔失敗", func(t *testing.T, home string, td *Tidy) {
			// 最後一步改內容之前，那個檔被換掉（模擬做到一半有人動了檔）：前面做過的都要退回。
			last := td.Steps[len(td.Steps)-1]
			renameFile = func(from, to string) error {
				if err := os.Rename(from, to); err != nil {
					return err
				}
				if strings.HasSuffix(to, "my_mikako-x--half1.md") {
					return os.WriteFile(last.Path, []byte("有人動了"), 0o644)
				}
				return nil
			}
			t.Cleanup(func() { renameFile = os.Rename })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := mineHome(t)
			td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
			if err != nil || len(td.Problems) > 0 {
				t.Fatal(err, td.Problems)
			}
			before := snapshot(t, home)
			tc.setup(t, home, td)
			err = td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now)
			if err == nil || !strings.Contains(err.Error(), "全部退回") {
				t.Fatalf("要失敗並自動退回：%v", err)
			}
			want := before
			if tc.name == "寫檔失敗" {
				// 被別人換掉的那個檔留著別人的內容（不蓋），其他都要回到之前。
				last := td.Steps[len(td.Steps)-1]
				rel, _ := filepath.Rel(home, last.Path)
				want = copyMap(before)
				data, _ := os.ReadFile(last.Path)
				h := sha256.Sum256(data)
				want[rel] = hex.EncodeToString(h[:]) + " " + strings.SplitN(before[rel], " ", 2)[1]
			}
			sameTree(t, "自動退回後", want, snapshot(t, home))
		})
	}
}

// 做到一半程式當掉（沒機會自動退回）：紀錄檔留著，--revert 要能退回，而且家跟之前逐檔相同。
// 用 panic 模擬當掉：apply 的自動退回只在回錯誤時做，panic 時不做，跟程式被關掉一樣。
func TestRevertAfterCrashMidway(t *testing.T) {
	for _, at := range []string{"第一次改名之後", "改名做到一半", "改名都做完、還沒改內容"} {
		t.Run(at, func(t *testing.T) {
			home := mineHome(t)
			td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
			if err != nil || len(td.Problems) > 0 {
				t.Fatal(err, td.Problems)
			}
			renames := 0
			for _, s := range td.Steps {
				if s.Op == OpRename {
					renames++
				}
			}
			crashAfter := map[string]int{"第一次改名之後": 1, "改名做到一半": renames / 2, "改名都做完、還沒改內容": renames}[at]
			before := snapshot(t, home)
			calls := 0
			renameFile = func(from, to string) error {
				err := os.Rename(from, to)
				if calls++; calls == crashAfter {
					panic("當掉")
				}
				return err
			}
			t.Cleanup(func() { renameFile = os.Rename })
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("要當掉")
					}
				}()
				_ = td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now)
			}()
			renameFile = os.Rename
			if !exists(TidyJournal(home)) {
				t.Fatal("當掉後紀錄檔要留著")
			}
			if at == "改名做到一半" {
				missing := false
				for _, s := range td.Steps {
					if s.Op == OpWrite && !exists(s.Path) {
						missing = true
					}
				}
				if !missing {
					t.Fatal("測試要涵蓋：要改內容的檔還在改名之前的位置")
				}
			}
			rp, err := PlanRevert(TidyJournal(home))
			if err != nil || len(rp.Problems) > 0 || rp.Done {
				t.Fatalf("當掉後要能退回：%v %v done=%v", err, rp.Problems, rp != nil && rp.Done)
			}
			if err := Revert(TidyJournal(home)); err != nil {
				t.Fatal(err)
			}
			sameTree(t, "當掉後退回", before, snapshot(t, home))
		})
	}
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestPreflightCatchesChangedFile(t *testing.T) {
	home := mineHome(t)
	td, _ := PlanTidy(home, testKinds, nil, TidyOptions{})
	write(t, home, map[string]string{"settings/packages.toml": "[claims.life]\npersona = \"mika\"\n# 新加的\n"})
	before := snapshot(t, home)
	err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now)
	if err == nil || !strings.Contains(err.Error(), "預演之後被改過") {
		t.Fatalf("預演之後被改過的檔要在開始前擋下：%v", err)
	}
	sameTree(t, "擋下後", before, snapshot(t, home))
}

func TestRevertRefusesChangedFile(t *testing.T) {
	home := mineHome(t)
	td, _ := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now); err != nil {
		t.Fatal(err)
	}
	write(t, home, map[string]string{"settings/packages.toml": "主人後來改了"})
	after := snapshot(t, home)
	rp, err := PlanRevert(TidyJournal(home))
	if err != nil || len(rp.Problems) == 0 {
		t.Fatalf("改過的檔要擋下退回：%v %v", err, rp)
	}
	if err := Revert(TidyJournal(home)); err == nil {
		t.Fatal("有問題要拒絕退回")
	}
	sameTree(t, "拒絕退回後", after, snapshot(t, home))
}

func TestTidyBlockedWhileServiceRuns(t *testing.T) {
	home := mineHome(t)
	write(t, home, map[string]string{"runtime/serve.pid": strconv.Itoa(os.Getpid())})
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(td.Problems, "\n"), "服務還在跑") {
		t.Fatalf("服務在跑要擋下：%v", td.Problems)
	}
}

func TestTidyRenameOptionsAndUndecided(t *testing.T) {
	home := mineHome(t)
	write(t, home, map[string]string{
		"personas/mine/Bad_Name.md": "x",
		"personas/mine/luna.md":     "跟官方同代號",
	})
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{Rename: map[string]string{"mika": "my_snow"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(td.Problems) > 0 {
		t.Fatal(td.Problems)
	}
	if !reflect.DeepEqual(td.Mine, []Code{{"mika", "my_snow"}, {"mikako", "my_mikako"}}) {
		t.Errorf("--rename 要照指定：%v", td.Mine)
	}
	if len(td.Undecided) != 2 {
		t.Errorf("名字不合規則、跟官方同代號的要留給人決定：%v", td.Undecided)
	}
	bad, _ := PlanTidy(home, testKinds, nil, TidyOptions{Rename: map[string]string{"mika": "snow", "nobody": "my_x"}})
	if p := strings.Join(bad.Problems, "\n"); !strings.Contains(p, "my_ 開頭") || !strings.Contains(p, "沒有 nobody.md") {
		t.Errorf("新代號不對、找不到的要擋下：%v", bad.Problems)
	}
	clash, _ := PlanTidy(home, testKinds, nil, TidyOptions{Rename: map[string]string{"mika": "my_mikako"}})
	if !strings.Contains(strings.Join(clash.Problems, "\n"), "都要改成 my_mikako") {
		t.Errorf("兩位改成同一個代號要擋下：%v", clash.Problems)
	}
}

// syncedRegistry 回一份真的角色包登記檔的內容：用真的建包（官方角色 slugs）與同步，在一個新的家同步出來（登記的是包裡的邏輯路徑 cards/<代號>.md…）。
func syncedRegistry(t *testing.T, slugs ...string) string {
	t.Helper()
	pub := filepath.Join(t.TempDir(), "pub")
	files := map[string]string{"home.toml": baseTOML + webTOML + "\n[admin]\npublic_personas = [\"" + strings.Join(slugs, "\", \"") + "\"]\n"}
	for _, s := range slugs {
		files["personas/"+s+".md"] = "---\nschema: 1\nname: " + s + "\n---\n官方\n"
		files["personas/avatars/"+s+".png"] = "png-" + s
	}
	write(t, pub, files)
	out := filepath.Join(t.TempDir(), "pack")
	r, err := personapack.Build(personapack.BuildOptions{Home: pub, Out: out, Version: "2026.11.14", NoRecords: true,
		OfflineFileKinds: func() ([]string, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	ptr := filepath.Join(t.TempDir(), "pack.toml")
	write(t, filepath.Dir(ptr), map[string]string{"pack.toml": "version = \"2026.11.14\"\nurl = \"file://" + out + "/m/2026.11.14.json\"\nsha256 = \"" + r.ManifestSHA256 + "\"\n"})
	user := filepath.Join(t.TempDir(), "user")
	write(t, user, map[string]string{"home.toml": baseTOML + webTOML})
	if res, err := personapack.Sync(context.Background(), personapack.SyncOptions{Home: user, PointerPath: ptr}); err != nil || res.Status != personapack.StatusSynced {
		t.Fatalf("同步：%v %+v", err, res)
	}
	return read(t, filepath.Join(user, "settings", "personas-pack.toml"))
}

func TestTidyAdoptMisplaced(t *testing.T) {
	home := mineHome(t)
	write(t, home, map[string]string{
		"settings/personas-pack.toml":                 syncedRegistry(t, "luna"),
		"personas/athena.md":                          "我的，放錯位置",
		"personas/avatars/athena.png":                 "a",
		"personas/avatars/athena~x.png":               "ax",
		"personas/avatars/full/athena.png":            "af",
		"personas/avatars/athena2.png":                "別人",
		"personas/wardrobe/athena/look.png":           "衣櫃",
		"vault/pkg/wardrobe/outfit/athena-x.md":       "---\nid: athena-x\npersona: athena\nsource: official\n---\n",
		"vault/pkg/wardrobe/photo/athena-x--half1.md": photoRec("athena-x--half1", "athena", "athena-x", "official", "avatars/athena.png"),
		"vault/pkg/wardrobe/photo/athena-x--full1.md": photoRec("athena-x--full1", "athena", "athena-x", "official", "avatars/full/athena.png"),
	})
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(td.Misplaced, []string{"athena"}) {
		t.Fatalf("有登記檔時只列不是官方的：%v", td.Misplaced)
	}
	if bad, _ := PlanTidy(home, testKinds, nil, TidyOptions{Adopt: []string{"luna"}}); !strings.Contains(strings.Join(bad.Problems, "\n"), "是官方角色") {
		t.Errorf("官方的不能收：%v", bad.Problems)
	}
	before := snapshot(t, home)
	td, err = PlanTidy(home, testKinds, nil, TidyOptions{Adopt: []string{"athena"}})
	if err != nil || len(td.Problems) > 0 {
		t.Fatal(err, td.Problems)
	}
	if err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now); err != nil {
		t.Fatal(err)
	}
	// 照片紀錄指著的圖搬進 personas/mine/ 的同一個相對路徑、檔名不改（DEV-160）；沒有紀錄指著的（athena~x）不搬、列出來。
	for _, rel := range []string{"personas/mine/my_athena.md", "personas/mine/avatars/athena.png", "personas/mine/avatars/full/athena.png",
		"personas/avatars/athena~x.png", "personas/avatars/athena2.png", "personas/wardrobe/athena/look.png"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("要有 %s", rel)
		}
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/photo/my_athena-x--half1.md")); got != photoRec("my_athena-x--half1", "my_athena", "my_athena-x", "mine", "avatars/athena.png") {
		t.Errorf("照片紀錄換成新代號、source 改成 mine、file 不變：%q", got)
	}
	if !strings.Contains(td.Text(true), "personas/avatars/athena~x.png　（athena 的東西放在官方位置，沒搬") {
		t.Errorf("沒有紀錄指著的圖要列出來：\n%s", td.Text(true))
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/outfit/my_athena-x.md")); got != "---\nid: my_athena-x\npersona: my_athena\nsource: mine\n---\n" {
		t.Errorf("收成自己的，衣服的圖跟著搬進 mine，source 要改成 mine：%q", got)
	}
	if !strings.Contains(td.Text(true), "personas/wardrobe/athena") {
		t.Errorf("官方位置沒搬的東西要列出來：\n%s", td.Text(true))
	}
	if err := Revert(TidyJournal(home)); err != nil {
		t.Fatal(err)
	}
	sameTree(t, "退回後", before, snapshot(t, home))
}

// 收成自己的時只搬紀錄指著的、放在 avatars/ 或 wardrobe/<舊代號>/ 底下的圖與影片：紀錄的 file 是資料，指著別的角色的卡、
// 別的角色的頭像與衣服資料夾、不是圖的檔、路上有符號連結的檔都不搬，列在「要人看的地方」。
func TestTidyAdoptMovesOnlyPersonaMedia(t *testing.T) {
	home := mineHome(t)
	outside := t.TempDir()
	write(t, outside, map[string]string{"leak.png": "外面的"})
	write(t, home, map[string]string{
		"settings/personas-pack.toml":                 syncedRegistry(t, "luna"),
		"personas/athena.md":                          "我的，放錯位置",
		"personas/avatars/athena.png":                 "a",
		"personas/wardrobe/athena/look.png":           "衣櫃",
		"personas/wardrobe/luna/day.png":              "露娜的衣服",
		"personas/catalog/luna.md":                    "露娜的目錄",
		"personas/avatars/notes.txt":                  "不是圖",
		"vault/pkg/wardrobe/outfit/athena-x.md":       "---\nid: athena-x\npersona: athena\nsource: official\n---\n",
		"vault/pkg/wardrobe/photo/athena-x--half1.md": photoRec("athena-x--half1", "athena", "athena-x", "official", "avatars/athena.png"),
		"vault/pkg/wardrobe/photo/athena-x--half2.md": photoRec("athena-x--half2", "athena", "athena-x", "official", "wardrobe/athena/look.png"),
		"vault/pkg/wardrobe/photo/athena-x--half3.md": photoRec("athena-x--half3", "athena", "athena-x", "official", "luna.md"),
		"vault/pkg/wardrobe/photo/athena-x--half4.md": photoRec("athena-x--half4", "athena", "athena-x", "official", "avatars/luna.png"),
		"vault/pkg/wardrobe/photo/athena-x--half5.md": photoRec("athena-x--half5", "athena", "athena-x", "official", "wardrobe/luna/day.png"),
		"vault/pkg/wardrobe/photo/athena-x--half6.md": photoRec("athena-x--half6", "athena", "athena-x", "official", "catalog/luna.md"),
		"vault/pkg/wardrobe/photo/athena-x--half7.md": photoRec("athena-x--half7", "athena", "athena-x", "official", "avatars/notes.txt"),
		"vault/pkg/wardrobe/photo/athena-x--half8.md": photoRec("athena-x--half8", "athena", "athena-x", "official", "avatars/link/leak.png"),
	})
	if err := os.Symlink(outside, filepath.Join(home, "personas", "avatars", "link")); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, home)
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{Adopt: []string{"athena"}})
	if err != nil || len(td.Problems) > 0 {
		t.Fatal(err, td.Problems)
	}
	if err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"personas/mine/avatars/athena.png", "personas/mine/wardrobe/athena/look.png"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("她自己的圖要搬進來：%s", rel)
		}
	}
	for _, rel := range []string{"personas/luna.md", "personas/avatars/luna.png", "personas/wardrobe/luna/day.png", "personas/catalog/luna.md", "personas/avatars/notes.txt"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("別的角色的、不是圖的檔不搬：%s 不見了", rel)
		}
	}
	for _, rel := range []string{"personas/mine/luna.md", "personas/mine/avatars/luna.png", "personas/mine/wardrobe/luna", "personas/mine/catalog", "personas/mine/avatars/notes.txt", "personas/mine/avatars/link"} {
		if _, err := os.Lstat(filepath.Join(home, rel)); err == nil {
			t.Errorf("不該搬進 personas/mine/：%s", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "leak.png")); err != nil {
		t.Error("符號連結另一頭的檔不能被搬走")
	}
	text := td.Text(true)
	for _, want := range []string{"personas/luna.md", "personas/avatars/luna.png", "personas/wardrobe/luna/day.png", "personas/avatars/notes.txt", "personas/avatars/link/leak.png"} {
		if !strings.Contains(text, want+"　（紀錄指著的檔") {
			t.Errorf("沒搬的要列出來：%s\n%s", want, text)
		}
	}
	if err := Revert(TidyJournal(home)); err != nil {
		t.Fatal(err)
	}
	sameTree(t, "退回後", before, snapshot(t, home))
}

// 收成自己的時，紀錄的 file 大小寫跟真的檔名不同（avatars/Luna.png，macOS 上就是露娜的頭像），或別的角色的紀錄也指著同一個檔：
// 那個檔不搬、列出原因；她自己的照搬。
func TestTidyAdoptLeavesFilesOtherPersonasClaim(t *testing.T) {
	home := mineHome(t)
	write(t, home, map[string]string{
		"settings/personas-pack.toml":                 syncedRegistry(t, "luna"),
		"personas/athena.md":                          "我的，放錯位置",
		"personas/avatars/athena.png":                 "a",
		"personas/avatars/photos/zx-u4.png":           "露娜的照片",
		"vault/pkg/wardrobe/outfit/athena-x.md":       "---\nid: athena-x\npersona: athena\nsource: official\n---\n",
		"vault/pkg/wardrobe/photo/athena-x--half1.md": photoRec("athena-x--half1", "athena", "athena-x", "official", "avatars/athena.png"),
		"vault/pkg/wardrobe/photo/athena-x--half4.md": photoRec("athena-x--half4", "athena", "athena-x", "official", "avatars/Luna.png"),
		"vault/pkg/wardrobe/photo/athena-x--half5.md": photoRec("athena-x--half5", "athena", "athena-x", "official", "avatars/photos/zx-u4.png"),
		"vault/pkg/wardrobe/photo/luna-b--half1.md":   photoRec("luna-b--half1", "luna", "luna-b", "official", "avatars/photos/zx-u4.png"),
	})
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{Adopt: []string{"athena"}})
	if err != nil || len(td.Problems) > 0 {
		t.Fatal(err, td.Problems)
	}
	if err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"personas/avatars/luna.png", "personas/avatars/photos/zx-u4.png"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("露娜的檔不能搬走：%s", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "personas", "mine", "avatars", "athena.png")); err != nil {
		t.Error("她自己的頭像要搬進 personas/mine/")
	}
	text := td.Text(true)
	for _, want := range []string{"personas/avatars/Luna.png　（紀錄指著的檔檔名是別的角色 luna 的", "personas/avatars/photos/zx-u4.png　（紀錄指著的檔也是別的角色（luna）的紀錄指著的"} {
		if !strings.Contains(text, want) {
			t.Errorf("少了「%s」\n%s", want, text)
		}
	}
}

func TestTidyMoveOfficial(t *testing.T) {
	root := t.TempDir()
	home, state := filepath.Join(root, "home"), filepath.Join(root, "state")
	write(t, home, map[string]string{
		"home.toml":                       "schema = 2\n" + baseTOML + webTOML + "\n[layout]\nversion = 2\nstate = \"" + state + "\"\nofficial_personas = \"home\"\n",
		"settings/personas-pack.toml":     syncedRegistry(t, "luna"),
		"personas/luna.md":                "官方",
		"personas/luna.md.bak-20260101-x": "官方備份",
		"personas/avatars/luna.png":       "z",
		"personas/wardrobe/luna/a.png":    "w",
		"personas/catalog/c.md":           "c",
		"personas/mine/my_mika.md":        "y",
		"vault/notes/where.md":            "圖在 " + home + "/personas/avatars/luna.png\n",
		"personas/mine/my_ai.md":          "參考 " + home + "/personas/luna.md\n",
		"packages/mine/garden/notes.md":   "圖 " + home + "/personas/avatars/luna.png\n",
	})
	before := snapshot(t, root)
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if td.OfficialMove || !strings.Contains(td.OfficialWhy, "--move-official") || len(td.Steps) != 0 {
		t.Fatalf("沒加 --move-official 只列出：%+v", td)
	}
	td, err = PlanTidy(home, testKinds, nil, TidyOptions{MoveOfficial: true})
	if err != nil || len(td.Problems) > 0 {
		t.Fatal(err, td.Problems)
	}
	sameTree(t, "預演不動檔", before, snapshot(t, root))
	if err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"personas/luna.md", "personas/luna.md.bak-20260101-x", "personas/avatars/luna.png", "personas/wardrobe/luna/a.png", "personas/catalog/c.md"} {
		if _, err := os.Stat(filepath.Join(state, p)); err != nil {
			t.Errorf("要搬到狀態資料夾：%s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "personas/mine/my_mika.md")); err != nil {
		t.Error("自己的角色留在家裡")
	}
	if got := read(t, filepath.Join(home, "home.toml")); strings.Contains(got, "official_personas") {
		t.Errorf("要拿掉 official_personas：%q", got)
	}
	if got := read(t, filepath.Join(home, "vault/notes/where.md")); got != "圖在 "+state+"/personas/avatars/luna.png\n" {
		t.Errorf("路徑要跟著改：%q", got)
	}
	if got := read(t, filepath.Join(home, "personas/mine/my_ai.md")); got != "參考 "+state+"/personas/luna.md\n" {
		t.Errorf("自己的卡裡的路徑也跟著改：%q", got)
	}
	if got := read(t, filepath.Join(home, "packages/mine/garden/notes.md")); !strings.Contains(got, home+"/personas/avatars") {
		t.Errorf("主人的套件不動：%q", got)
	}
	if !strings.Contains(td.Text(true), "packages/mine/garden/notes.md:1") {
		t.Errorf("主人的套件裡的路徑要列出來：\n%s", td.Text(true))
	}
	if err := Revert(TidyJournal(home)); err != nil {
		t.Fatal(err)
	}
	sameTree(t, "退回後", before, snapshot(t, root))

	// 有判斷不出來的卡就不搬；發行角色包的家不搬。
	write(t, home, map[string]string{"personas/stray.md": "?"})
	td, _ = PlanTidy(home, testKinds, nil, TidyOptions{MoveOfficial: true})
	if !strings.Contains(strings.Join(td.Problems, "\n"), "stray") {
		t.Errorf("判斷不出來的卡要先處理：%v", td.Problems)
	}
	// 主人確認是官方的（--official）：這一次當官方的一起搬；寫了官方位置沒有的代號就擋下。
	td, _ = PlanTidy(home, testKinds, nil, TidyOptions{MoveOfficial: true, Official: []string{"stray"}})
	if len(td.Problems) > 0 || !td.OfficialMove || len(td.Misplaced) > 0 || !strings.Contains(strings.Join(td.Official, " "), "personas/stray.md") {
		t.Errorf("確認是官方的就一起搬：problems=%v official=%v misplaced=%v", td.Problems, td.Official, td.Misplaced)
	}
	td, _ = PlanTidy(home, testKinds, nil, TidyOptions{Official: []string{"nobody"}})
	if !strings.Contains(strings.Join(td.Problems, "\n"), "--official nobody") {
		t.Errorf("官方位置沒有的代號要擋：%v", td.Problems)
	}
	_ = os.Remove(filepath.Join(home, "personas/stray.md"))
	write(t, home, map[string]string{"home.toml": "schema = 2\n" + baseTOML + webTOML + "\n[admin]\npublic_personas = [\"luna\"]\n\n[layout]\nversion = 2\nstate = \"" + state + "\"\nofficial_personas = \"home\"\n"})
	td, _ = PlanTidy(home, testKinds, nil, TidyOptions{MoveOfficial: true})
	if td.OfficialMove || !strings.Contains(td.OfficialWhy, "發行角色包") {
		t.Errorf("發行角色包的家不搬：%+v", td.OfficialWhy)
	}
}

func devHome(t *testing.T, env string) string {
	home := filepath.Join(t.TempDir(), "home")
	write(t, home, map[string]string{
		"home.toml":                                         baseTOML + webTOML + "\n[runtime]\nenv = \"" + env + "\"\n",
		"personas/luna.md":                                  "官方",
		"personas/mine/my_mika.md":                          "---\nname: Mika\nstatus: design\n---\n",
		"personas/mine/avatars/my_mika.png":                 "y",
		"personas/mine/avatars/my_mika~casual.png":          "c",
		"personas/mine/adult/my_mika.png":                   "a",
		"vault/agents/life/agent.md":                        "---\nid: life\npersona: my_mika\n---\n",
		"vault/pkg/wardrobe/outfit/my_mika-casual.md":       "---\nid: my_mika-casual\npersona: my_mika\nsource: mine\n---\n",
		"vault/pkg/wardrobe/rotation/my_mika.md":            "---\nid: my_mika\n---\n",
		"vault/pkg/wardrobe/note/CN-2.md":                   "---\nid: CN-2\npersona: my_mika\nrecord: my_mika-casual\n---\n",
		"vault/pkg/wardrobe/photo/my_mika-casual--half1.md": photoRec("my_mika-casual--half1", "my_mika", "my_mika-casual", "mine", "avatars/my_mika~casual.png"),
		"settings/persona-import.toml":                      "schema = 1\n\n[files]\n\"personas/mine/my_mika.md\" = \"abc\"\n\"personas/mine/avatars/my_mika.png\" = \"def\"\n\n[records]\n\"wardrobe.outfit/my_mika-casual\" = \"123\"\n",
	})
	return home
}

func TestPromote(t *testing.T) {
	home := devHome(t, "dev")
	before := snapshot(t, home)
	p, err := PlanPromote(home, "my_mika", "", testKinds, nil)
	if err != nil || len(p.Problems) > 0 {
		t.Fatal(err, p.Problems)
	}
	if !strings.Contains(p.Text(false), "展示間") || !strings.Contains(p.Text(false), "public_personas") {
		t.Errorf("要提醒衣服一套一套升、要加進 public_personas：\n%s", p.Text(false))
	}
	if err := p.Apply("personas promote", PromoteJournal(home, "my_mika"), PromoteOriginals(home, "my_mika"), now); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(home, "personas/mika.md")); got != "---\nname: Mika\nstatus: design\n---\n" {
		t.Errorf("卡（設計中）搬到官方位置、內容原樣：%q", got)
	}
	// 匯入登記：卡搬出 personas/mine/ 就不是匯入的自己的角色了（拿掉）；圖還在 mine、檔名不改；紀錄跟著改編號。
	if got := read(t, filepath.Join(home, "settings/persona-import.toml")); got != "schema = 1\n\n[files]\n\"personas/mine/avatars/my_mika.png\" = \"def\"\n\n[records]\n\"wardrobe.outfit/mika-casual\" = \"123\"\n" {
		t.Errorf("匯入登記：%q", got)
	}
	for _, rel := range []string{"personas/mine/avatars/my_mika.png", "personas/mine/avatars/my_mika~casual.png", "personas/mine/adult/my_mika.png"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("圖留在 mine、檔名不改：%s", rel)
		}
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/photo/mika-casual--half1.md")); got != photoRec("mika-casual--half1", "mika", "mika-casual", "mine", "avatars/my_mika~casual.png") {
		t.Errorf("照片紀錄跟著新代號、file 不變：%q", got)
	}
	if !strings.Contains(p.Text(false), "圖：1 張留在 personas/mine/，檔名不改") {
		t.Errorf("預演要說圖不改名：\n%s", p.Text(false))
	}
	if _, err := os.Stat(filepath.Join(home, "personas/mine/my_mika.md")); err == nil {
		t.Error("家裡不留重複的卡")
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/outfit/mika-casual.md")); got != "---\nid: mika-casual\npersona: mika\nsource: mine\n---\n" {
		t.Errorf("衣服紀錄跟著新代號、source 不動：%q", got)
	}
	if got := read(t, filepath.Join(home, "vault/agents/life/agent.md")); !strings.Contains(got, "persona: mika\n") {
		t.Errorf("名冊：%q", got)
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/note/CN-2.md")); !strings.Contains(got, "record: mika-casual") {
		t.Errorf("便條：%q", got)
	}
	if _, err := PlanPromote(home, "my_mika", "", testKinds, nil); err == nil {
		t.Error("升過了，卡已經不在 mine")
	}
	if err := Revert(PromoteJournal(home, "my_mika")); err != nil {
		t.Fatal(err)
	}
	sameTree(t, "退回後", before, snapshot(t, home))

	if p, _ := PlanPromote(home, "my_mika", "luna", testKinds, nil); !strings.Contains(strings.Join(p.Problems, "\n"), "官方位置已經有 luna.md") {
		t.Errorf("官方已有同代號要擋下：%v", p.Problems)
	}
	if _, err := PlanPromote(home, "my_mika", "Bad", testKinds, nil); err == nil {
		t.Error("--as 不合規則要擋下")
	}
}

func TestPromoteRefusedOutsideDevHome(t *testing.T) {
	home := devHome(t, "")
	before := snapshot(t, home)
	if _, err := PlanPromote(home, "my_mika", "", testKinds, nil); err == nil || !strings.Contains(err.Error(), "開發家") {
		t.Fatalf("不是開發家要拒絕：%v", err)
	}
	sameTree(t, "拒絕後", before, snapshot(t, home))
}

func TestUnreadablePackageWithRecordsBlocks(t *testing.T) {
	home := mineHome(t)
	issues := []PackageIssue{{Package: "wardrobe", Message: "套件 wardrobe 讀不起來"}, {Package: "empty", Message: "套件 empty 讀不起來"}}
	td, err := PlanTidy(home, testKinds, issues, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p := strings.Join(td.Problems, "\n")
	if !strings.Contains(p, "wardrobe") || strings.Contains(p, "empty") {
		t.Errorf("有紀錄的才擋：%v", td.Problems)
	}
}

func TestBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		ok         bool
	}{
		{"athena.png", "athena", true}, {"athena@2x.png", "athena", true}, {"athena~x.png", "athena", true}, {"athena+n.png", "athena", true},
		{"athena2.png", "athena", false}, {"athenas.png", "athena", false}, {"athena", "athena", false}, {"xathena.png", "athena", false},
	} {
		if _, ok := boundary(tc.name, tc.code); ok != tc.ok {
			t.Errorf("boundary(%q, %q) = %v", tc.name, tc.code, ok)
		}
	}
}

// 套件角色的代號是 <套件>.<角色>，套件名可以有「-」：garden-x.fairy-casual 不是 garden 的紀錄。
func TestPrefixOfSkipsPackagePersona(t *testing.T) {
	b := newBuilder(t.TempDir(), nil)
	b.byOld["garden"] = &Rename{Old: "garden", New: "my_garden"}
	if r, _ := b.prefixOf("garden-x.fairy-casual"); r != nil {
		t.Error("garden-x.fairy-casual 是套件角色的，不能改")
	}
	if r, n := b.prefixOf("garden-casual"); r == nil || n != "my_garden-casual" {
		t.Errorf("garden-casual 要改成 my_garden-casual：%q", n)
	}
}

// 檔頭沒寫 id 的舊紀錄：編號就是檔名，只改檔名（不擋下整個計畫、不補 id）；退回後逐檔相同。
// 沒宣告的套件紀錄，檔名是舊代號（或 <舊代號>-…）的要列出來、不改名。
func TestUndeclaredRecordNamesListed(t *testing.T) {
	home := mineHome(t)
	write(t, home, map[string]string{
		"vault/pkg/studio/profile/mika.md":   "---\nid: mika\n---\n",
		"vault/pkg/studio/draft/mika-a1.md":  "---\nid: mika-a1\n---\n",
		"vault/pkg/studio/profile/mikano.md": "---\nid: mikano\n---\n",
		"vault/pkg/studio/archive/x/mika.md": "---\nid: mika\n---\n",
	})
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range td.Notes {
		if strings.Contains(n.Why, "persona_id") {
			got = append(got, n.Path)
		}
	}
	sort.Strings(got)
	if want := []string{"vault/pkg/studio/draft/mika-a1.md", "vault/pkg/studio/profile/mika.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("列出來的：%v，要 %v", got, want)
	}
}

func TestRecordWithoutIDRenamesFileOnly(t *testing.T) {
	home := mineHome(t)
	write(t, home, map[string]string{
		"vault/pkg/wardrobe/outfit/mika-plain.md": "---\npersona: mika\ntitle: 舊的\n---\n",
		"vault/pkg/wardrobe/rotation/mikako.md":   "---\nlist: []\n---\n",
	})
	before := snapshot(t, home)
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil || len(td.Problems) > 0 {
		t.Fatal(err, td.Problems)
	}
	if err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/outfit/my_mika-plain.md")); got != "---\npersona: my_mika\ntitle: 舊的\n---\n" {
		t.Errorf("沒寫 id 的：改檔名與 persona，不補 id：%q", got)
	}
	if got := read(t, filepath.Join(home, "vault/pkg/wardrobe/rotation/my_mikako.md")); got != "---\nlist: []\n---\n" {
		t.Errorf("只改檔名、內容原樣：%q", got)
	}
	if err := Revert(TidyJournal(home)); err != nil {
		t.Fatal(err)
	}
	sameTree(t, "退回後", before, snapshot(t, home))
}

// 要改內容的檔是捷徑（symlink）：改了會把捷徑換成一般的檔，擋下請人看；只搬的（紀錄改檔名）可以，搬的是捷徑本身。
func TestSymlinkNotRewritten(t *testing.T) {
	home := mineHome(t)
	outside := filepath.Join(t.TempDir(), "agent.md")
	write(t, filepath.Dir(outside), map[string]string{"agent.md": "---\nid: life\npersona: mika\n---\n"})
	life := filepath.Join(home, "vault/agents/life/agent.md")
	if err := os.Remove(life); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, life); err != nil {
		t.Fatal(err)
	}
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(td.Problems, "\n"), "vault/agents/life/agent.md 是捷徑") {
		t.Fatalf("捷徑要擋下：%v", td.Problems)
	}

	home = mineHome(t)
	rot := filepath.Join(home, "vault/pkg/wardrobe/rotation/mika.md")
	target := filepath.Join(t.TempDir(), "rot.md")
	write(t, filepath.Dir(target), map[string]string{"rot.md": read(t, rot)})
	if err := os.Remove(rot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, rot); err != nil {
		t.Fatal(err)
	}
	if td, err = PlanTidy(home, testKinds, nil, TidyOptions{}); err != nil {
		t.Fatal(err)
	}
	// rotation 的檔頭有 id，要改內容：一樣擋下。
	if !strings.Contains(strings.Join(td.Problems, "\n"), "rotation/mika.md 是捷徑") {
		t.Fatalf("要改內容的捷徑紀錄要擋下：%v", td.Problems)
	}
}

// 收尾（--forget）：紀錄檔與原文改名成備份，之後可以再整理；做到一半當掉的不能收尾。
func TestForget(t *testing.T) {
	home := mineHome(t)
	td, err := PlanTidy(home, testKinds, nil, TidyOptions{})
	if err != nil || len(td.Problems) > 0 {
		t.Fatal(err, td.Problems)
	}
	if err := td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now); err != nil {
		t.Fatal(err)
	}
	afterApply := snapshot(t, home)
	fp, err := PlanForget(TidyJournal(home), now)
	if err != nil || len(fp.Problems) > 0 || len(fp.Moves) != 2 {
		t.Fatalf("預演收尾：%v %+v", err, fp)
	}
	sameTree(t, "預演不動", afterApply, snapshot(t, home))
	if _, err := Forget(TidyJournal(home), now); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{TidyJournal(home), TidyOriginals(home)} {
		if exists(p) {
			t.Errorf("收尾後 %s 要改名成備份", p)
		}
		if !exists(p + ".bak-" + now.Format("20060102") + "-done") {
			t.Errorf("要有備份 %s.bak-…-done", p)
		}
	}
	if len(platform.PendingRecodeJournals(home)) != 0 {
		t.Error("收尾後不算還沒收尾的紀錄")
	}
	if err := Revert(TidyJournal(home)); err == nil {
		t.Error("收尾後沒有紀錄可以退回")
	}

	// 做到一半當掉的：不能收尾（家是一半改過的樣子）。
	home = mineHome(t)
	td, _ = PlanTidy(home, testKinds, nil, TidyOptions{})
	renameFile = func(from, to string) error {
		err := os.Rename(from, to)
		panic(err)
	}
	t.Cleanup(func() { renameFile = os.Rename })
	func() {
		defer func() { _ = recover() }()
		_ = td.Apply("home tidy", TidyJournal(home), TidyOriginals(home), now)
	}()
	renameFile = os.Rename
	if _, err := Forget(TidyJournal(home), now); err == nil || !exists(TidyJournal(home)) {
		t.Fatalf("沒做完的不能收尾：%v", err)
	}
}
