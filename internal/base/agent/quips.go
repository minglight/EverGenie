package agent

// 梗本（DEV-164；規格 ai_docs/schemas/note.md「梗本」）：全家共用的老梗，是一份固定名字的主題筆記「梗本」，
// 格式照一般主題筆記（一行一條、行尾標寫的人與日期），秘書寫的那一行要有「出處」。
// 開場帶一節「梗本」（Instructions）：有長度上限，太長只帶精選與最近的幾條；這週提過的（QuipUsed 記在
// runtime/quips-used.json，不進 vault）先不帶，同一條梗一週內不會被誰再拿出來。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

const (
	// QuipTopicName 是梗本的主題名字；家裡已經有同名（或別名）的主題就沿用那一份，沒有就第一次記的時候建在 QuipTopicCategory。
	QuipTopicName     = "梗本"
	QuipTopicCategory = "喜好"
	quipTopicSummary  = "全家共用的老梗：主人笑過、說喜歡的句子、秘書之間的老梗；一行一條，附日期與出處"
	// QuipRestDays：提過的梗這麼多天內不再帶進開場（說話層「同一條梗一週內不重複」）。
	QuipRestDays = 7
	// QuipShowLines、QuipShowRunes 是開場帶梗的上限（條數、字數，兩個都要守）；超過就帶精選與最近的。
	QuipShowLines = 8
	QuipShowRunes = 1200
	quipUsesFile  = "quips-used.json"
	quipKeepDays  = 30 // 用過的紀錄留這麼多天（超過 QuipRestDays 的只是讓主人查得到最近誰提過）
	quipSource    = "出處"
)

// quipIntro 是開場「梗本」那一節的開頭：怎麼用（頻率規則的正本在說話層「老梗」那段，這裡再提醒一次，主人自己寫的說話層也照樣有）、怎麼記。
const quipIntro = "全家共用的老梗（主題筆記「梗本」）。照說話層「老梗」那段用：聊到相關的事才順口回頭提一條，一次對話最多一次；主人累、心情不好或出事時不提。" +
	"提了之後打 `evergenie notes quips used <那條梗裡的字>`，這一週它就不再出現在任何人的開場。梗只是氣氛：跟主人有關的事實照原樣說，不為了梗改寫。\n" +
	"主人被逗笑、說喜歡哪一句、或秘書之間形成新的老梗時，記一條：`evergenie notes add 梗本 「<那句梗>」<怎麼來的>——出處：<在哪、誰說的>`（核心會在行尾標你和日期）。"

// QuipLines 回梗本內文裡一條一條的梗（去掉前後空白；空行、標題、分隔線不算），pinned 是放在標題含「精選」那一段底下的。
func QuipLines(body string) (lines []string, pinned []bool) {
	inPinned := false
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			inPinned = strings.Contains(t, "精選")
			continue
		}
		if !contentLine(t) {
			continue
		}
		lines = append(lines, t)
		pinned = append(pinned, inPinned)
	}
	return lines, pinned
}

// QuipPick 是開場要帶的梗：Lines 照梗本裡的順序；Total 是一共幾條（含這週提過的），Resting 是這週提過、先不帶的。
type QuipPick struct {
	Lines   []string
	Total   int
	Resting []string
}

// PickQuips 從梗本內文挑開場要帶的：先拿掉 resting（去掉行尾標記後比），再先拿精選、其餘從最新（最後面）往前拿，
// 直到 QuipShowLines 條或 QuipShowRunes 個字；結果照原本的順序排。決定性：同樣的內文與 resting 挑出一樣的。
func PickQuips(body string, resting map[string]bool) QuipPick {
	lines, pinned := QuipLines(body)
	p := QuipPick{Total: len(lines)}
	var cand []int
	for i, l := range lines {
		if resting[normalizedLine(l)] {
			p.Resting = append(p.Resting, normalizedLine(l))
			continue
		}
		cand = append(cand, i)
	}
	order := []int{}
	for _, i := range cand {
		if pinned[i] {
			order = append(order, i)
		}
	}
	for k := len(cand) - 1; k >= 0; k-- {
		if !pinned[cand[k]] {
			order = append(order, cand[k])
		}
	}
	keep := map[int]bool{}
	runes := 0
	for _, i := range order {
		n := utf8.RuneCountInString(lines[i])
		if len(keep) >= QuipShowLines || runes+n > QuipShowRunes {
			continue // 太長的這條略過，後面短的還塞得下就照樣帶
		}
		keep[i] = true
		runes += n
	}
	for i, l := range lines {
		if keep[i] {
			p.Lines = append(p.Lines, l)
		}
	}
	return p
}

// QuipSection 組開場「梗本」那一節的內文。
func QuipSection(p QuipPick) string {
	parts := []string{quipIntro}
	if p.Total == 0 {
		return strings.Join(append(parts, "（梗本還沒有梗。）"), "\n\n")
	}
	var lines []string
	for _, l := range p.Lines {
		if !strings.HasPrefix(l, "- ") {
			l = "- " + l
		}
		lines = append(lines, l)
	}
	if len(lines) > 0 {
		parts = append(parts, demoteHeadings(strings.Join(lines, "\n")))
	}
	if left := p.Total - len(p.Lines) - len(p.Resting); left > 0 || len(p.Resting) > 0 {
		note := fmt.Sprintf("（梗本一共 %d 條，這裡帶 %d 條", p.Total, len(p.Lines))
		if left > 0 {
			note += "（精選與最近的）"
		}
		if len(p.Resting) > 0 {
			note += fmt.Sprintf("；這週提過的 %d 條先不帶", len(p.Resting))
		}
		parts = append(parts, note+"。全部用 `evergenie notes show 梗本` 看。）")
	}
	return strings.Join(parts, "\n\n")
}

// isQuipBook：這份主題是不是梗本（名字或別名是「梗本」）。
func isQuipBook(r vault.Record) bool {
	return TopicName(r) == QuipTopicName || slices.Contains(fieldList(r, "aliases"), QuipTopicName)
}

// quipBook 找梗本：名字或別名是「梗本」的那一份（家裡已經有的直接沿用）。
func (m *Module) quipBook(ctx context.Context) (body string, found bool, err error) {
	r, found, err := m.findTopic(ctx, QuipTopicName)
	if err != nil || !found {
		return "", found, err
	}
	return r.Body, true, nil
}

// Quips 回開場「梗本」那一節（Instructions 用同一份）：讀梗本、拿掉這週提過的、照上限挑。
// 讀不到梗本（例：別名撞名）不擋開場：記一行警告，當成還沒有梗。
func (m *Module) Quips(ctx context.Context) (QuipPick, string) {
	body, _, err := m.quipBook(ctx)
	if err != nil {
		m.warn("讀不到梗本", err)
		body = ""
	}
	p := PickQuips(body, m.restingQuips())
	return p, QuipSection(p)
}

// NoteQuips 是 sdk notes.quips：開場「梗本」那一節的全文與條數（`evergenie notes quips`）。
func (m *Module) NoteQuips(ctx context.Context, _ *sdk.NotesQuipsRequest, by string) (*sdk.NotesQuipsResponse, error) {
	p, text := m.Quips(ctx)
	return &sdk.NotesQuipsResponse{Text: text, Total: int32(p.Total), Shown: int32(len(p.Lines)), Resting: p.Resting}, nil
}

// ---- 提過的梗 ----

type quipUse struct {
	Quip string    `json:"quip"`
	At   time.Time `json:"at"`
	By   string    `json:"by"`
}

type quipUses struct {
	Used []quipUse `json:"used"`
}

func (m *Module) quipUsesPath() string { return platform.RuntimeDir(m.home, quipUsesFile) }

// readQuipUses 讀提過的梗；檔案不在或讀不懂當成沒有（只是少擋幾條重複，不擋開場）。
func (m *Module) readQuipUses() quipUses {
	var u quipUses
	data, err := os.ReadFile(m.quipUsesPath())
	if err != nil {
		return u
	}
	if json.Unmarshal(data, &u) != nil {
		m.warn("提過的梗紀錄讀不懂，當成沒有", platform.Invalid("quips", m.quipUsesPath()))
		return quipUses{}
	}
	return u
}

// restingQuips 回這週（QuipRestDays 天內）提過的梗（去掉行尾標記）。
func (m *Module) restingQuips() map[string]bool {
	since := m.d.Clock.Now().Add(-QuipRestDays * 24 * time.Hour)
	out := map[string]bool{}
	for _, u := range m.readQuipUses().Used {
		if u.At.After(since) {
			out[u.Quip] = true
		}
	}
	return out
}

// QuipUsed 記下剛剛回頭提了梗本裡的哪一條（sdk notes.quip_used）：text 對到梗本裡的一條（先比「」裡的那句，再比含這段字的），
// 之後 QuipRestDays 天開場不帶它。寫的人照 noteWriter（主人、system、名冊上的職位）。
func (m *Module) QuipUsed(ctx context.Context, req *sdk.NotesQuipUsedRequest, by string) (*sdk.NotesQuipUsedResponse, error) {
	who, err := m.noteWriter(ctx, by)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, platform.Invalid("text", "要給那條梗裡的一段字（例：「」裡的那句）")
	}
	m.notesMu.Lock()
	defer m.notesMu.Unlock()
	body, found, err := m.quipBook(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, platform.NotFound("還沒有梗本；先用 evergenie notes add 梗本 <一行> 記第一條")
	}
	lines, _ := QuipLines(body)
	var hits []string
	for _, l := range lines {
		if n := normalizedLine(l); strings.Contains(n, text) && !slices.Contains(hits, n) {
			hits = append(hits, n)
		}
	}
	if len(hits) > 1 { // 好幾條都含這段字：「」裡剛好是這句的那條算數
		var exact []string
		for _, h := range hits {
			if strings.Contains(h, "「"+text+"」") {
				exact = append(exact, h)
			}
		}
		if len(exact) == 1 {
			hits = exact
		}
	}
	switch len(hits) {
	case 0:
		return nil, platform.NotFound("梗本裡沒有含「" + text + "」的梗；用 evergenie notes show 梗本 看有哪些")
	case 1:
	default:
		if len(hits) > 5 {
			hits = append(hits[:5], "…")
		}
		return nil, platform.Conflict("梗本裡有好幾條含「" + text + "」，請給長一點的字：\n  " + strings.Join(hits, "\n  "))
	}
	now := m.d.Clock.Now()
	keep := now.Add(-quipKeepDays * 24 * time.Hour)
	u := m.readQuipUses()
	next := quipUses{Used: []quipUse{}}
	for _, old := range u.Used {
		if old.Quip != hits[0] && old.At.After(keep) {
			next.Used = append(next.Used, old)
		}
	}
	next.Used = append(next.Used, quipUse{Quip: hits[0], At: now, By: who})
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return nil, platform.Unavailable("無法記下提過的梗")
	}
	if err := os.MkdirAll(platform.RuntimeDir(m.home), 0o700); err != nil {
		return nil, platform.Unavailable("無法記下提過的梗")
	}
	if err := platform.AtomicWrite(m.quipUsesPath(), append(data, '\n'), 0o600); err != nil {
		return nil, platform.Unavailable("無法記下提過的梗")
	}
	until := now.Add(QuipRestDays * 24 * time.Hour)
	return &sdk.NotesQuipUsedResponse{Quip: hits[0], Until: until,
		Message: "記下了：這條梗 " + until.In(m.loc).Format("01-02 15:04") + " 以前不會再帶進開場（" + hits[0] + "）"}, nil
}

// quipNeedsSource：秘書寫進梗本的那一行要有出處（主人自己寫的不檢查）。
func quipNeedsSource(line, who string) error {
	if who == ownerMark || who == "system" || strings.Contains(line, quipSource) {
		return nil
	}
	return platform.Invalid("line", "梗本一行一條要寫出處，例：「<那句梗>」<怎麼來的>——出處：<在哪、誰說的>（日期核心會標）")
}
