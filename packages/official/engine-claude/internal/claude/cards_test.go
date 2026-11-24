package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

func cardReq(dir string, dry bool, cards ...sdk.AgentCard) *sdk.EngineWriteAgentCardsRequest {
	return &sdk.EngineWriteAgentCardsRequest{Account: "main", EngineConfig: map[string]any{"config_dir": dir}, UserName: "Amy", Cards: cards, DryRun: dry}
}

func actions(r *sdk.EngineWriteAgentCardsResponse) map[string]string {
	out := map[string]string{}
	for _, f := range r.Files {
		out[f.Agent] = f.Action
	}
	return out
}

// TestAgentsLinkDryRun：預演不寫任何檔、不建資料夾；真的寫之後內容有 frontmatter、記號與職責；再跑一次是 same。
// Guards: REQ-161
func TestAgentsLinkDryRun(t *testing.T) {
	dir := t.TempDir()
	e := &Engine{}
	code := sdk.AgentCard{ID: "code", Name: "雅典娜", Aliases: []string{"Athena"}, Duty: "寫 code\n和 review", Body: "# 職責\n讀 code。\n\n附加規則"}
	r, err := e.WriteAgentCards(context.Background(), cardReq(dir, true, code))
	if err != nil || actions(r)["code"] != "create" {
		t.Fatalf("預演：%+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agents")); !os.IsNotExist(err) {
		t.Fatalf("預演不該建資料夾：%v", err)
	}
	if r, err = e.WriteAgentCards(context.Background(), cardReq(dir, false, code)); err != nil || actions(r)["code"] != "create" {
		t.Fatalf("寫入：%+v %v", r, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "agents", "code.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: code\n", "「雅典娜」「Athena」", "當 Amy 說", "寫 code 和 review", cardMarker, "附加規則"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("檔案少了 %q：\n%s", want, data)
		}
	}
	if r, _ = e.WriteAgentCards(context.Background(), cardReq(dir, false, code)); actions(r)["code"] != "same" {
		t.Fatalf("再跑一次應該 same：%+v", r)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("不該在帳號資料夾動其他東西（settings.json 等）：%v", entries)
	}
}

// TestAgentsLinkKeepsForeignFiles：別人的檔案與連結不覆蓋；名冊上沒有的、自己產生的才移除。
// Guards: REQ-161
func TestAgentsLinkKeepsForeignFiles(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(target, []byte("舊系統的"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(agents, "life.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "work.md"), []byte("手寫的"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &Engine{}
	gone := sdk.AgentCard{ID: "gone", Name: "走了", Body: "x"}
	if _, err := e.WriteAgentCards(context.Background(), cardReq(dir, false, gone)); err != nil {
		t.Fatal(err)
	}
	r, err := e.WriteAgentCards(context.Background(), cardReq(dir, false, sdk.AgentCard{ID: "life", Name: "露娜", Body: "y"}, sdk.AgentCard{ID: "work", Name: "阿媞米斯", Body: "z"}))
	if err != nil {
		t.Fatal(err)
	}
	got := actions(r)
	if got["life"] != "skip" || got["work"] != "skip" || got["gone"] != "remove" {
		t.Fatalf("動作不對：%v", got)
	}
	if data, _ := os.ReadFile(target); string(data) != "舊系統的" {
		t.Fatalf("連結指到的檔案被改了：%q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(agents, "work.md")); string(data) != "手寫的" {
		t.Fatalf("手寫的檔案被覆蓋：%q", data)
	}
	if _, err := os.Stat(filepath.Join(agents, "gone.md")); !os.IsNotExist(err) {
		t.Fatalf("名冊上沒有的卡應移除：%v", err)
	}
	if r, _ := e.WriteAgentCards(context.Background(), &sdk.EngineWriteAgentCardsRequest{EngineConfig: map[string]any{}}); r.Note == "" {
		t.Fatal("沒有 config_dir 要說明原因")
	}
}

// 家設了 cards = false（同一台第二個家，秘書同名）：不寫、也不清帳號裡另一個家寫的卡。
func TestAgentCardsDisabledByHomeLeavesAccountUntouched(t *testing.T) {
	dir := t.TempDir()
	e := &Engine{}
	life := sdk.AgentCard{ID: "life", Name: "露娜", Body: "日常的家寫的"}
	if _, err := e.WriteAgentCards(context.Background(), cardReq(dir, false, life)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "agents", "life.md"))
	if err != nil {
		t.Fatal(err)
	}
	req := cardReq(dir, false, sdk.AgentCard{ID: "life", Name: "工坊的", Body: "工坊寫的"}, sdk.AgentCard{ID: "code", Name: "雅典娜", Body: "x"})
	req.EngineConfig["cards"] = false
	r, err := e.WriteAgentCards(context.Background(), req)
	if err != nil || r.Note == "" || len(r.Files) != 0 {
		t.Fatalf("cards = false 要略過並說明：%+v %v", r, err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "agents", "life.md")); string(after) != string(before) {
		t.Fatalf("另一個家寫的卡被改了：%q", after)
	}
	if _, err := os.Stat(filepath.Join(dir, "agents", "code.md")); !os.IsNotExist(err) {
		t.Fatal("cards = false 不該寫新卡")
	}
}
