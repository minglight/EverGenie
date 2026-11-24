package agent

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

//go:embed rules/shared.md
var sharedRules string

// SetCoreFinder 接上「EverGenie repo 在哪」的函式（組裝時呼叫；registry 傳 update.FindCore 包起來的）。沒接或回空字串時，開發家的規則請秘書去問主人。
func (m *Module) SetCoreFinder(f func() string) { m.core = f }

func (m *Module) Instructions(ctx context.Context, id, sessionName string) (string, error) {
	return m.instructions(ctx, id, sessionName, nil)
}

// personaTrial 是測口吻時換掉的部分（TestInstructions）：座位 seat 暫時坐 slug 這位人設，
// 人設段用 body、說話層用 style（空＝照目前設定）、梗本段用 quips（nil＝照家裡的梗本；情境自己帶梗本時用，DEV-164）。
// 其他段（職責、附加規則、共用規則、記憶、名冊、現況）照常。
type personaTrial struct {
	slug, name, body, style string
	quips                   []string
}

func (m *Module) instructions(ctx context.Context, id, sessionName string, trial *personaTrial) (string, error) {
	r, err := m.d.Vault.Get(ctx, "agent", id)
	if err != nil {
		return "", err
	}
	slug, _ := r.Fields["persona"].(string)
	if trial != nil {
		slug = trial.slug
	}
	persona, err := m.persona(slug)
	if err != nil {
		return "", err
	}
	if trial != nil {
		persona.Body = trial.body
	}
	sections := []string{}
	add := func(name, body string) { sections = append(sections, "## "+name+"\n\n"+strings.TrimSpace(body)) }
	add("人設", persona.Body)
	_, style, _, err := m.src.Style()
	if err != nil {
		return "", err
	}
	if trial != nil && strings.TrimSpace(trial.style) != "" {
		style = trial.style
	}
	add("說話層", style)
	add("職責", m.DutiesOf(r))
	rows, err := m.List(ctx) // 兼任的職位與名冊共用
	if err != nil {
		return "", err
	}
	for _, v := range []struct{ kind, title string }{{"addendum", "附加規則"}, {"memory", "記憶"}} {
		if v.kind == "memory" {
			add("共用規則", strings.TrimSpace(sharedRules)+"\n\n"+m.codeRule()+"\n\n"+m.shareRule())
		}
		record, err := m.d.Vault.Get(ctx, v.kind, id)
		if err != nil && platform.CodeOf(err) != sdk.ErrorCodeNotFound {
			return "", err
		}
		add(v.title, record.Body)
		if v.kind != "addendum" {
			continue
		}
		if m.appends != nil {
			// 套件補充（DEV-154）：主人在「設定 → 套件 → 擴充點」寫的、或套件預設的那一段；沒有就不出這一節。
			var parts []string
			for _, p := range m.appends(id) {
				head := p.Source
				if t := oneLine(p.Title); t != "" {
					head += "：" + t
				}
				parts = append(parts, "（"+head+"）\n"+demoteHeadings(strings.TrimSpace(p.Text)))
			}
			if len(parts) > 0 {
				add("套件補充", strings.Join(parts, "\n\n"))
			}
		}
		// 兼任的職位（DEV-161，AGT-43）：同一位角色擔任的其他職位的專業背景；沒有就不出這一節。
		// 照座位原本坐的角色算（測口吻時也一樣，AGT-22 只換人設、說話層與名冊裡的名字）。
		side, err := m.sidePositions(ctx, r, rows)
		if err != nil {
			return "", err
		}
		if side != "" {
			add("兼任的職位", side)
		}
	}
	// 記憶核心之後：「主人」主題全文與自己的職位索引（DEV-161）。別的主題不預先載入，要用時 `evergenie notes show`。
	owner, err := m.OwnerTopic(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(owner) == "" {
		owner = "（還沒有寫。第一次知道主人的身分、家人、目標這類穩定的事實時，先問主人一句，再用 `evergenie notes add 主人 <一行>` 寫進去。）"
	}
	add("主人", demoteHeadings(owner)) // 從 profile 搬來的常有標題，不能自己開一節
	// 梗本（DEV-164）：全家共用的老梗，有長度上限、這週提過的先不帶（quips.go）。測口吻的情境可以自己帶一份。
	if trial != nil && trial.quips != nil {
		add("梗本", QuipSection(PickQuips(strings.Join(trial.quips, "\n"), nil)))
	} else {
		_, quips := m.Quips(ctx)
		add("梗本", quips)
	}
	index, err := m.NoteIndex(ctx, id)
	if err != nil {
		return "", err
	}
	add("職位索引", indexForPrompt(index))
	lines := []string{}
	for _, a := range rows {
		slug, _ := a.Fields["persona"].(string)
		p, err := m.persona(slug)
		if err != nil {
			return "", err
		}
		name, _ := a.Fields["display_name"].(string)
		if name == "" {
			name, _ = p.Fields["name"].(string)
		}
		if trial != nil && a.ID == id {
			name = trial.name // 名冊裡這個座位用被測人設的名字
		}
		duty, _ := a.Fields["duty"].(string)
		lines = append(lines, fmt.Sprintf("- %s：%s（%s）", a.ID, name, duty))
	}
	add("名冊", strings.Join(lines, "\n"))
	m.mu.Lock()
	snapshot, _, err := m.snapshot(ctx)
	m.mu.Unlock()
	if err != nil {
		return "", err
	}
	add("現況", snapshot)
	return strings.Join(sections, "\n\n---\n\n"), nil
}

// codeRule 告訴秘書這個家能不能改程式（法律第 1.1 條，DEV-93）：依 home.toml 的 [runtime] env 只放對應的一句。
// 開發家（env = "dev"）：任何秘書都可以直接在 EverGenie repo 開分支改核心與官方套件；正式家：只做客製（packages/mine），
// 要改官方或核心就說「這要到開發家改」。repo 位置由 SetCoreFinder 接進來的函式找（找不到就請秘書問主人）。讀不到 home.toml 當正式家（比較保守）。
func (m *Module) codeRule() string {
	if h, err := platform.LoadHome(m.home); err == nil && h.IsDev() {
		where := "（這個家找不到 repo 的位置，先問主人）"
		if m.core != nil {
			if dir := m.core(); dir != "" {
				where = "在 `" + dir + "`"
			}
		}
		return "這是開發家：任何秘書都可以改程式。主人叫你改的東西，不管是官方套件（`packages/official/`）、核心還是 `evergenie` repo，都直接改，不轉給別人；改的本來就是官方套件時改官方，不另外建套件。`evergenie` repo " + where + "，開分支改（照 `docs/branches.md`），改完跑 `make ci`；push 與刪除先問主人。"
	}
	return "這是正式家：只做客製，新套件建在 `packages/mine/`。不改官方套件（`packages/official/`）、核心與 `evergenie` repo；只想換官方套件的行為時走擴充點（`evergenie packages extensions <套件>` 看有哪些）；官方套件不能整包複製來改，擴充點做不到的就做一個自己的新套件，或說「這要到開發家改」，並把要改什麼講清楚。"
}

// shareRule 告訴秘書看板附件的位置：使用者在聊天附上的檔案存在家的附件資料夾（第 1 版 <家>/runtime/share/，第 2 版 <家>/media/share/），訊息裡是 /share/<名字> 連結（REQ-82）。
func (m *Module) shareRule() string {
	rule := "使用者訊息裡的 `/share/<名字>` 連結，檔案就在 `" + platform.SharesDir(m.home) + "/<名字>`，可以直接讀（圖片也可以看）。" +
		"任務單與待辦的附件（`evergenie tasks get`／`evergenie reminders get` 的 `attachments`）也是這種連結；要把檔案附上去，先 `evergenie share <完整路徑>`，再 `evergenie tasks set <單號> --add_attachments <它回的 /share/ 網址>`（待辦用 `evergenie reminders set <R-n> …`）。"

	return rule
}

func (m *Module) Context(ctx context.Context, sessionName string) (string, bool, error) {
	if sessionName == "" || filepath.Base(sessionName) != sessionName || strings.ContainsAny(sessionName, "/\\") || sessionName == "." || sessionName == ".." {
		return "", false, platform.Invalid("session", "對話名稱不正確")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	body, at, err := m.snapshot(ctx)
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(platform.RuntimeDir(m.home, "sessions", sessionName+".md"))
	if os.IsNotExist(err) {
		return body, true, nil
	}
	if err != nil {
		return "", false, platform.Unavailable("無法讀取對話狀態")
	}
	r, _, err := vault.Decode(data)
	if err != nil {
		return "", false, err
	}
	seen, _ := r.Fields["now_seen_at"].(string)
	seenAt, _ := time.Parse(time.RFC3339Nano, seen)
	if !at.After(seenAt) {
		return "", false, nil
	}
	return body, true, nil
}

// snapshot persists its generation time, so fake clocks and restarts use the same age.
func (m *Module) snapshot(ctx context.Context) (string, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, err
	}
	path := platform.RuntimeDir(m.home, "now.md")
	now := m.d.Clock.Now().In(m.loc)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", time.Time{}, platform.Unavailable("無法讀取現況快照")
	}
	if err == nil {
		r, _, err := vault.Decode(data)
		if err != nil {
			return "", time.Time{}, err
		}
		value, _ := r.Fields["updated_at"].(string)
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return "", time.Time{}, platform.Invalid("updated_at", "現況快照時間不正確")
		}
		if now.Sub(at) <= 30*time.Minute {
			return strings.TrimSpace(r.Body), at, nil
		}
	}
	lines := []string{"時間：" + now.Format(time.RFC3339)}
	files, err := filepath.Glob(platform.RuntimeDir(m.home, "sessions", "*.md"))
	if err != nil {
		return "", time.Time{}, err
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", time.Time{}, platform.Unavailable("無法讀取對話狀態")
		}
		r, _, err := vault.Decode(data)
		if err != nil {
			return "", time.Time{}, err
		}
		title, _ := r.Fields["title"].(string)
		state, _ := r.Fields["state"].(string)
		lines = append(lines, fmt.Sprintf("- %s：%s（%s）", strings.TrimSuffix(filepath.Base(file), ".md"), title, state))
	}
	body := strings.Join(lines, "\n")
	r := vault.Record{Fields: map[string]any{"updated_at": now.Format(time.RFC3339Nano)}, Body: body}
	if err := platform.AtomicWrite(path, vault.Encode(r, nil, vault.Spec{}), 0600); err != nil {
		return "", time.Time{}, platform.Unavailable("無法寫入現況快照")
	}
	return body, now, nil
}

// TrialPersona 回測口吻時被測人設的名字與人設段內文（P6-5b）：cardText 不是空的就用它
// （看板編輯中、還沒存的內容；有檔頭就取內文與 name，沒有檔頭整段當內文，名字沿用存檔的卡），否則讀 slug 的卡；設計中的卡也可以。
func (m *Module) TrialPersona(slug, cardText string) (name, body string, err error) {
	card, err := m.persona(slug)
	if err != nil {
		return "", "", err
	}
	name, _ = card.Fields["name"].(string)
	body = card.Body
	if strings.TrimSpace(cardText) == "" {
		return name, body, nil
	}
	body = cardText
	if t := strings.TrimLeft(cardText, " \t\r\n"); strings.HasPrefix(t, "---") || strings.HasPrefix(t, "+++") {
		draft, err := parseCard([]byte(t))
		if err != nil {
			return "", "", err
		}
		body = draft.Body
		if n, _ := draft.Fields["name"].(string); n != "" {
			name = n
		}
	}
	return name, body, nil
}

// TestInstructions 組出「這位秘書的座位暫時坐 slug 這位人設」時的指令，給測口吻用（P6-5b、REQ-130）。
// 順序、職責、附加規則、共用規則、記憶、名冊、現況都跟 Instructions 一樣；不同的只有：
//   - 人設段：cardText 不是空的就用它，否則讀 slug 的卡（見 TrialPersona）；
//   - 說話層段：styleText 不是空的就用它，否則照目前設定；
//   - 名冊裡這個座位的名字換成被測人設的名字；
//   - 梗本段：quips 不是 nil 就用這幾條（情境自己帶的梗本，DEV-164），否則照家裡的梗本。
func (m *Module) TestInstructions(ctx context.Context, seat, slug, cardText, styleText string, quips []string) (string, error) {
	name, body, err := m.TrialPersona(slug, cardText)
	if err != nil {
		return "", err
	}
	return m.instructions(ctx, seat, "persona-test-"+slug, &personaTrial{slug: slug, name: name, body: body, style: styleText, quips: quips})
}

// indexForPrompt 把職位索引放進開場：拿掉檔案自己的大標題（這一節已經叫「職位索引」），分類標題降一級。
func indexForPrompt(index string) string {
	if _, rest, ok := strings.Cut(index, "\n"); ok && strings.HasPrefix(index, "# ") {
		index = rest
	}
	return strings.ReplaceAll("\n"+strings.TrimLeft(index, "\n"), "\n## ", "\n### ")
}

// oneLine 把換行收成空白（套件補充的說明只當一行標示用）。
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// demoteHeadings 把套件補充裡開頭是 # 的行改成普通文字（前面加反斜線）：補充只是「套件補充」這一節裡的一段，
// 不能自己開一節冒充職責、共用規則或記憶（憲法第 10 條）。
func demoteHeadings(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if t := strings.TrimLeft(l, " \t"); strings.HasPrefix(t, "#") {
			lines[i] = l[:len(l)-len(t)] + "\\" + t
		}
	}
	return strings.Join(lines, "\n")
}

// DutiesFrom 回這位秘書的職責跟著哪份官方樣板走（agent 紀錄的 duties_from，寫成 <套件>/<樣板的職位>；職位改名後照樣指得到）；
// 空字串＝職責是主人自己的，全文在家裡。
func DutiesFrom(fields map[string]any) string {
	s, _ := fields["duties_from"].(string)
	return strings.TrimSpace(s)
}

// DutiesOf 回職責全文（DEV-154）：官方套件帶來的職位（duties_from 有值）讀程式那邊的樣板，家裡不存一份，
// 所以官方改版不用寫家裡的檔；主人自己建的職位用家裡 agent 紀錄的內文。樣板讀不到（套件被拿掉）不擋開場，寫一句白話。
// 可選知識（DEV-161）只留主人勾了的那幾塊（紀錄的 knowledge；沒有欄位照預設），見 knowledge.go。
func (m *Module) DutiesOf(r vault.Record) string {
	body := m.rawDuties(r)
	blocks := ParseKnowledge(body)
	if len(blocks) == 0 {
		return body
	}
	return KeepKnowledge(body, EnabledKnowledge(blocks, r.Fields))
}
