package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// 秘書的 Agent 定義檔（Q3，evergenie agents link）：Claude Code 的 Agent 工具可以點名叫出來的子 agent，
// 位置是 <config_dir>/agents/<職位 id>.md，frontmatter 有 name、description，內文是職責。
// 只動這個資料夾裡「帶我們記號」的檔案；別人的檔案（包括指到別處的連結）一律不覆蓋；絕不碰帳號的 settings.json（ECL-N5）。

// cardMarker 是檔案裡的記號：有它才算我們產生的，才會被更新或移除。
const cardMarker = "<!-- generated-by: evergenie agents link（自動產生，不要手改；要改職責請改秘書的職責或附加規則） -->"

var cardID = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// cardsOff：engine_config 的 cards = false 時不寫也不清這個帳號的 Agent 定義檔。同一台兩個家共用一個帳號資料夾時，
// 兩個家有同名的秘書會互相蓋掉 agents/<id>.md，所以第二個家（例：工坊）在 home.toml [runtime] 寫 cards = false（GOAL-2 W2）。
func cardsOff(cfg map[string]any) bool {
	v, ok := cfg["cards"].(bool)
	return ok && !v
}

// cardsDir 是這個帳號放 Agent 定義檔的資料夾；帳號沒寫 config_dir 回空字串。
func cardsDir(cfg map[string]any) string {
	dir, _ := cfg["config_dir"].(string)
	dir = expandHome(strings.TrimSpace(dir))
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	return filepath.Join(dir, "agents")
}

// oneLine 把多行文字壓成一行（description 是單行摘要）。
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func quoted(names []string) string {
	var b strings.Builder
	for _, n := range names {
		if n != "" {
			b.WriteString("「" + n + "」")
		}
	}
	return b.String()
}

// renderCard 產生一張卡的檔案內容。
func renderCard(c sdk.AgentCard, userName string) string {
	if userName == "" {
		userName = "使用者"
	}
	name := c.Name
	if name == "" {
		name = c.ID
	}
	desc := "職位 " + c.ID
	if duty := oneLine(c.Duty); duty != "" {
		desc += "：" + duty
	}
	desc += "。現在的角色叫「" + name + "」。當 " + userName + " 說" + quoted(append([]string{name}, c.Aliases...)) + "或要做這個職位的事時使用。"
	var b strings.Builder
	b.WriteString("---\nname: " + c.ID + "\ndescription: >-\n  " + desc + "\n---\n\n")
	b.WriteString(cardMarker + "\n\n")
	b.WriteString(strings.TrimRight(c.Body, "\n") + "\n")
	return b.String()
}

func cardSummary(content string) string {
	return fmt.Sprintf("%d 行", strings.Count(content, "\n"))
}

// WriteAgentCards 把名冊寫成這個帳號的 Agent 定義檔。DryRun 只回報會做什麼，不寫任何檔、不建資料夾。
func (e *Engine) WriteAgentCards(ctx context.Context, req *sdk.EngineWriteAgentCardsRequest) (*sdk.EngineWriteAgentCardsResponse, error) {
	if cardsOff(req.EngineConfig) {
		return &sdk.EngineWriteAgentCardsResponse{Note: "這個家設定 cards = false，不寫秘書卡片，略過"}, nil
	}
	dir := cardsDir(req.EngineConfig)
	if dir == "" {
		return &sdk.EngineWriteAgentCardsResponse{Note: "這個帳號沒有設定 engine_config.config_dir，略過"}, nil
	}
	out := &sdk.EngineWriteAgentCardsResponse{Files: []sdk.AgentCardFile{}}
	want := map[string]bool{}
	for _, c := range req.Cards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, c.ID+".md")
		row := sdk.AgentCardFile{Agent: c.ID, Path: path}
		if !cardID.MatchString(c.ID) {
			row.Action, row.Summary = "skip", "id 不是小寫英文、數字、減號，不能當檔名"
			out.Files = append(out.Files, row)
			continue
		}
		want[c.ID] = true
		content := renderCard(c, req.UserName)
		old, state := readCard(path)
		switch {
		case state == cardMissing:
			row.Action, row.Summary = "create", cardSummary(content)
		case state == cardForeign:
			row.Action, row.Summary = "skip", "已經有別人的檔案（或連結），不覆蓋"
			out.Files = append(out.Files, row)
			continue
		case old == content:
			row.Action, row.Summary = "same", cardSummary(content)
			out.Files = append(out.Files, row)
			continue
		default:
			row.Action, row.Summary = "update", cardSummary(content)
		}
		if !req.DryRun {
			if err := writeCard(dir, path, content); err != nil {
				return nil, err
			}
		}
		out.Files = append(out.Files, row)
	}
	// 以前產生、名冊上已經沒有的卡拿掉。
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "讀不到 " + dir + "：" + err.Error()}
	}
	for _, en := range entries {
		id, ok := strings.CutSuffix(en.Name(), ".md")
		if !ok || want[id] {
			continue
		}
		path := filepath.Join(dir, en.Name())
		if _, state := readCard(path); state != cardOurs {
			continue
		}
		out.Files = append(out.Files, sdk.AgentCardFile{Agent: id, Path: path, Action: "remove", Summary: "名冊上已經沒有這位"})
		if !req.DryRun {
			if err := os.Remove(path); err != nil {
				return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "移除不了 " + path + "：" + err.Error()}
			}
		}
	}
	return out, nil
}

type cardState int

const (
	cardMissing cardState = iota
	cardOurs              // 一般檔案，帶我們的記號
	cardForeign           // 連結、資料夾或沒有記號的檔案
)

// readCard 讀一個目標檔：連結一律算別人的（舊系統把 agents/<id>.md 連到別處），不跟著連結讀。
func readCard(path string) (string, cardState) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", cardMissing
	}
	if !fi.Mode().IsRegular() {
		return "", cardForeign
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), cardMarker) {
		return "", cardForeign
	}
	return string(data), cardOurs
}

// writeCard 先寫暫存檔再改名，中途失敗不會留下半個檔案。
func writeCard(dir, path, content string) error {
	fail := func(err error) error {
		return &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "寫不進 " + path + "：" + err.Error()}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(err)
	}
	tmp, err := os.CreateTemp(dir, ".hg-card-*")
	if err != nil {
		return fail(err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fail(err)
	}
	return nil
}
