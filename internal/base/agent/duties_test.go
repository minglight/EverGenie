package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// treeHashes 回資料夾底下每個檔的雜湊（相對路徑 → sha256）。
func treeHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	must(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(root, path)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	}))
	return out
}

func sameTree(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// 官方職責跟著程式走（DEV-154 定案第 2 件的檢查）：家裡寫了附加規則，官方職責改版後，秘書拿到「新官方職責＋原附加規則」，
// 家裡 vault/agents/ 的檔一個都沒被寫。
func TestOfficialDutiesFollowProgram(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	official := "第一版官方職責"
	m.SetOfficialDuties(func(ref string) (string, bool) { return official, ref == "secretaries/code" })
	_, err := m.PatchAgent(ctx, "code", map[string]any{"duties_from": "secretaries/code"}, nil, "system")
	must(t, err)
	must(t, m.WriteAddendum(ctx, "code", "主人的附加規則", "owner(web)"))
	before := treeHashes(t, filepath.Join(h.Dir, "vault", "agents"))

	text, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	if !strings.Contains(text, "第一版官方職責") || !strings.Contains(text, "主人的附加規則") || strings.Contains(text, "## 職責\n\n官方職責\n") {
		t.Fatalf("要讀樣板的職責、不讀家裡那份：\n%s", text)
	}
	official = "第二版官方職責（改版了）"
	text, err = m.Instructions(ctx, "code", "code")
	must(t, err)
	if !strings.Contains(text, "第二版官方職責（改版了）") || strings.Contains(text, "第一版官方職責") || !strings.Contains(text, "主人的附加規則") {
		t.Fatalf("官方改版後要拿到新職責＋原附加規則：\n%s", text)
	}
	if after := treeHashes(t, filepath.Join(h.Dir, "vault", "agents")); !sameTree(before, after) {
		t.Fatal("官方職責改版不該寫家裡的檔")
	}

	// 官方職責的職位不能整份改寫（要客製寫附加規則）；主人自己建的職位照舊。
	if err := m.WriteDuties(ctx, "code", "整份蓋掉", "system"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("官方職責不該被改寫：%v", err)
	}
	must(t, m.WriteDuties(ctx, "life", "主人自己的職責", "system"))
	text, err = m.Instructions(ctx, "life", "life")
	must(t, err)
	if !strings.Contains(text, "主人自己的職責") {
		t.Fatalf("自建職位讀家裡的內文：\n%s", text)
	}

	// 樣板讀不到（套件被拿掉）：不擋開場，職責段寫一句白話。
	m.SetOfficialDuties(func(string) (string, bool) { return "", false })
	text, err = m.Instructions(ctx, "code", "code")
	must(t, err)
	if !strings.Contains(text, "讀不到它的樣板") || !strings.Contains(text, "主人的附加規則") {
		t.Fatalf("樣板讀不到要有白話說明：\n%s", text)
	}
}

func writeAgentFile(t *testing.T, home, id, name, text string) string {
	t.Helper()
	dir := filepath.Join(home, "vault", "agents", id)
	must(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, name)
	must(t, os.WriteFile(path, []byte(text), 0o600))
	return path
}

const agentHead = "---\nschema: 1\nupdated_at: \"2026-11-01T00:00:00+08:00\"\nby: system\nid: %ID%\ncreated_at: \"2026-10-01T00:00:00+08:00\"\nduty: 測試\npersona: athena\nenabled: true\nextras:\n    x:\n        cron: 0 1 * * *\n---\n"

func head(id string) string { return strings.ReplaceAll(agentHead, "%ID%", id) }

// 家的遷移：對得上官方樣板的職位改成讀樣板；主人寫在職責內文裡的字一個不少搬到附加規則；自建職位不動；能重跑。
func TestMigrateOfficialDuties(t *testing.T) {
	home := t.TempDir()
	known := map[string]bool{"## 負責什麼": true, "- 寫 code": true, "- 舊版樣板才有的一行": true}
	official := map[string]OfficialDutyTemplate{
		"code":   {Ref: "secretaries/code", Known: known},
		"system": {Ref: "secretaries/system", Known: known},
		"work":   {Ref: "secretaries/work", Known: known},
		"empty":  {Ref: "secretaries/empty", Known: known},
	}
	// code：跟樣板（舊版）一樣，只多了自動加的大標題 → 內文清空、附加規則不動。
	codeAgent := writeAgentFile(t, home, "code", "agent.md", head("code")+"\n# code 的職責\n\n## 負責什麼\n- 寫 code\n- 舊版樣板才有的一行\n")
	codeAdd := writeAgentFile(t, home, "code", "addendum.md", "---\nschema: 1\nupdated_at: \"2026-11-01T00:00:00+08:00\"\nby: owner(web)\nagent: code\ncreated_at: \"2026-11-01T00:00:00+08:00\"\n---\n原本的附加規則\n")
	// system：內文有主人的客製，已經有附加規則 → 客製接在後面。
	writeAgentFile(t, home, "system", "agent.md", head("system")+"## 負責什麼\n- 寫 code\n- 主人加的：只管這台電腦  \n\n## 界線\n- 不碰孩子的平板\n")
	sysAdd := writeAgentFile(t, home, "system", "addendum.md", "---\nschema: 1\nupdated_at: \"2026-11-01T00:00:00+08:00\"\nby: owner(web)\nagent: system\ncreated_at: \"2026-11-01T00:00:00+08:00\"\n---\n先講結論\n")
	// work：有客製、沒有附加規則檔 → 建一份。
	writeAgentFile(t, home, "work", "agent.md", head("work")+"- 寫 code\n- 週五下午不排會議\n")
	// empty：內文是空的 → 只標成官方。
	writeAgentFile(t, home, "empty", "agent.md", head("empty"))
	// art：主人自己建的職位（沒有樣板）→ 一個位元都不動。
	artAgent := writeAgentFile(t, home, "art", "agent.md", head("art")+"## 生圖\n- 主人自己寫的職責\n")
	artBefore, _ := os.ReadFile(artAgent)
	codeAddBefore, _ := os.ReadFile(codeAdd)

	must(t, MigrateOfficialDuties(home, official))

	read := func(path string) string { data, err := os.ReadFile(path); must(t, err); return string(data) }
	if got, want := read(codeAgent), head("code")[:len(head("code"))-4]+"duties_from: secretaries/code\n---\n"; got != want {
		t.Fatalf("code 的紀錄：\n%s\n要：\n%s", got, want)
	}
	if read(codeAdd) != string(codeAddBefore) {
		t.Fatal("code 沒有客製，附加規則不該動")
	}
	sys := read(sysAdd)
	for _, want := range []string{"先講結論\n\n" + MovedDutiesMark + "\n- 主人加的：只管這台電腦  \n\n## 界線\n- 不碰孩子的平板\n"} {
		if !strings.HasSuffix(sys, want) {
			t.Fatalf("system 的附加規則要原樣接上客製：\n%s", sys)
		}
	}
	work := read(filepath.Join(home, "vault", "agents", "work", "addendum.md"))
	if !strings.Contains(work, "agent: work\n") || !strings.HasSuffix(work, "---\n"+MovedDutiesMark+"\n- 週五下午不排會議\n") {
		t.Fatalf("work 要建一份附加規則：\n%s", work)
	}
	if !strings.HasSuffix(read(filepath.Join(home, "vault", "agents", "empty", "agent.md")), "duties_from: secretaries/empty\n---\n") {
		t.Fatal("空內文的職位只標成官方")
	}
	if _, err := os.Stat(filepath.Join(home, "vault", "agents", "empty", "addendum.md")); !os.IsNotExist(err) {
		t.Fatal("沒有客製不該建附加規則")
	}
	if read(artAgent) != string(artBefore) {
		t.Fatal("主人自己建的職位不該動")
	}
	if got := MovedDuties(home); len(got) != 2 || got[0] != "system" || got[1] != "work" {
		t.Fatalf("報告要列出被搬過的職位：%v", got)
	}

	// 能重跑：第二次什麼都不變。
	before := treeHashes(t, home)
	must(t, MigrateOfficialDuties(home, official))
	if !sameTree(before, treeHashes(t, home)) {
		t.Fatal("重跑不該再改")
	}
	// 跑到一半（附加規則接好了、紀錄還沒標）再跑：不會接兩次。
	writeAgentFile(t, home, "work", "agent.md", head("work")+"- 寫 code\n- 週五下午不排會議\n")
	must(t, MigrateOfficialDuties(home, official))
	if strings.Count(read(filepath.Join(home, "vault", "agents", "work", "addendum.md")), "週五下午不排會議") != 1 {
		t.Fatal("中斷後重跑不該把客製接兩次")
	}
}

// 官方職責的樣板讀不到（套件被拿掉）：健康檢查出黃字，不是紅字（不擋部署）。
func TestOfficialDutiesMissingWarns(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	_, err := m.PatchAgent(ctx, "code", map[string]any{"duties_from": "gone/code"}, nil, "system")
	must(t, err)
	m.SetOfficialDuties(func(string) (string, bool) { return "", false })
	problems, warnings := m.check(ctx)
	if strings.Contains(strings.Join(problems, "\n"), "gone/code") || !strings.Contains(strings.Join(warnings, "\n"), "官方職責「gone/code」讀不到") {
		t.Fatalf("要是黃字：紅 %v 黃 %v", problems, warnings)
	}
}
