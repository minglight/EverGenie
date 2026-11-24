package session

// 測口吻（P6-5b、REQ-130）：用固定情境、跟常駐一樣的指令，讓某位人設在某個座位上各說一次，結果寫成一份 Markdown。
// 每個情境一次 engine.Ask（不開工具、不留 session），並行最多 4、每則逾時 600 秒；一則失敗不影響其他。
// 結果在暫存資料夾的 runtime/persona-tests/<人設代號>/<YYYY-MM-DD-HHMMSS>.md：檔頭（YAML）記狀態，先寫 running，跑完原子改寫。
// 情境可以自己帶一份梗本（`[梗本]` 行，換掉開場的梗本段）與提梗次數的檢查（`[檢查] 提梗 <n>`），跑完照回覆自動判對錯（DEV-164）。

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

//go:embed tone_scenarios.md
var defaultScenarios string

const (
	toneParallel    = 4
	toneAskSeconds  = 600
	toneInterrupted = "核心重開，這次測試中斷"
	toneStopped     = "核心停止，這次測試中斷"
	toneDefaultList = 10
)

var (
	toneSlugPattern = regexp.MustCompile(`^(my_[a-z0-9]{1,24}|[a-z][a-z0-9]*)$`) // 官方代號，或主人自己的 my_<名字>
	toneIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	toneRunPattern  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{6}(-\d+)?\.md$`)
)

// toneScenario 是情境檔裡的一段。
type toneScenario struct {
	id, title, body string
	seats           []string
	line            int
	quips           []string   // 這則開場的梗本（`[梗本]` 行的梗；nil＝照家裡的梗本）
	quipKeys        [][]string // 每條梗用來認「有沒有提到」的字（第一個也是報告上的名字）
	check           *quipCheck // `[檢查] 提梗 …`；nil＝不檢查
}

// quipCheck 是一則回覆裡回頭提梗的條數要在 min～max 之間（同一條提好幾次算一條）。
type quipCheck struct{ min, max int }

var quipCheckPattern = regexp.MustCompile(`^提梗\s*([0-9]+)(?:\s*[-~～]\s*([0-9]+))?$`)

// parseScenarioExtras 把情境內文裡的 `[梗本] <字>/<別的說法>｜<那一行梗>` 與 `[檢查] 提梗 <n>[-<m>]` 拿出來，剩下的才是主人的訊息。
// first 是內文第一行在檔案裡的行號（報錯用）。
func parseScenarioExtras(sc *toneScenario, body []string, first int, bad func(int, string) error) ([]string, error) {
	var rest []string
	for k, l := range body {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "[梗本]"):
			key, line, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(t, "[梗本]")), "｜")
			var alts []string
			for _, a := range strings.Split(key, "/") {
				if a = strings.TrimSpace(a); a != "" {
					alts = append(alts, a)
				}
			}
			if !ok || len(alts) == 0 || strings.TrimSpace(line) == "" {
				return nil, bad(first+k, "梗本行要寫成「[梗本] <認得出這條梗的字>/<別的說法>｜<那一行梗>」")
			}
			sc.quips = append(sc.quips, strings.TrimSpace(line))
			sc.quipKeys = append(sc.quipKeys, alts)
		case strings.HasPrefix(t, "[檢查]"):
			m := quipCheckPattern.FindStringSubmatch(strings.TrimSpace(strings.TrimPrefix(t, "[檢查]")))
			if m == nil || sc.check != nil {
				return nil, bad(first+k, "檢查只有一種、一則一行：「[檢查] 提梗 <條數>」或「[檢查] 提梗 <最少>-<最多>」")
			}
			lo, _ := strconv.Atoi(m[1])
			hi := lo
			if m[2] != "" {
				hi, _ = strconv.Atoi(m[2])
			}
			if hi < lo {
				return nil, bad(first+k, "提梗的最多不能比最少小")
			}
			sc.check = &quipCheck{min: lo, max: hi}
		default:
			rest = append(rest, l)
		}
	}
	if sc.check != nil && len(sc.quips) == 0 {
		return nil, bad(sc.line, "「"+sc.id+"」有提梗的檢查，要有 [梗本] 行（檢查只認情境自己帶的梗）")
	}
	return rest, nil
}

// judge 照回覆判提梗的條數對不對，回 ok 與一句白話（寫進報告）。
func (c quipCheck) judge(sc toneScenario, reply string) (bool, string) {
	var hits []string
	for _, alts := range sc.quipKeys {
		for _, a := range alts {
			if strings.Contains(reply, a) {
				hits = append(hits, alts[0])
				break
			}
		}
	}
	want := fmt.Sprintf("%d 條", c.min)
	if c.max != c.min {
		want = fmt.Sprintf("%d～%d 條", c.min, c.max)
	}
	named := "（" + strings.Join(hits, "、") + "）"
	n := len(hits)
	switch {
	case n < c.min && n == 0:
		return false, "✗ 完全沒回頭提梗，規定 " + want
	case n < c.min:
		return false, fmt.Sprintf("✗ 只提了 %d 條梗%s，規定 %s", n, named, want)
	case n > c.max && c.max == 0:
		return false, fmt.Sprintf("✗ 這則不該提梗，卻提了 %d 條%s", n, named)
	case n > c.max:
		return false, fmt.Sprintf("✗ 提太多：%d 條%s，規定最多 %d 條", n, named, c.max)
	case n == 0:
		return true, "✓ 沒有提梗，規定 " + want
	}
	return true, fmt.Sprintf("✓ 回頭提了 %d 條梗%s，規定 %s", n, named, want)
}

// parseScenarios 讀情境檔：`## <id>｜<標題>[｜職位,職位]`，下面是主人這一輪送進去的訊息。
// 只留適用於 seat 的（沒寫職位＝每個座位都跑）。格式錯回 INVALID，指出第幾行。
func parseScenarios(text, seat string) ([]toneScenario, error) {
	var all []toneScenario
	var cur *toneScenario
	var body []string
	bad := func(line int, msg string) error {
		return platform.Invalid("scenarios", fmt.Sprintf("情境檔第 %d 行：%s", line, msg))
	}
	flush := func() error {
		if cur == nil {
			return nil
		}
		rest, err := parseScenarioExtras(cur, body, cur.line+1, bad)
		if err != nil {
			return err
		}
		cur.body = strings.TrimSpace(strings.Join(rest, "\n"))
		if cur.body == "" {
			return bad(cur.line, "「"+cur.id+"」沒有內容")
		}
		all = append(all, *cur)
		cur, body = nil, nil
		return nil
	}
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		n := i + 1
		if !strings.HasPrefix(line, "## ") {
			if cur != nil {
				body = append(body, line)
			}
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
		parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "## ")), "｜")
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
			return nil, bad(n, "標題要寫成「## <id>｜<標題>」或「## <id>｜<標題>｜<職位>,<職位>」")
		}
		if !toneIDPattern.MatchString(parts[0]) {
			return nil, bad(n, "情境 id「"+parts[0]+"」只能用英文、數字、底線和減號")
		}
		cur = &toneScenario{id: parts[0], title: parts[1], line: n}
		if len(parts) == 3 {
			for _, s := range strings.FieldsFunc(parts[2], func(r rune) bool { return r == ',' || r == '，' }) {
				if s = strings.TrimSpace(s); s != "" {
					cur.seats = append(cur.seats, s)
				}
			}
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	var out []toneScenario
	seen := map[string]bool{}
	for _, sc := range all {
		if len(sc.seats) > 0 && !contains(sc.seats, seat) {
			continue
		}
		if seen[sc.id] {
			return nil, bad(sc.line, "情境「"+sc.id+"」有兩個版本都適用於 "+seat+"，請用職位欄位分開")
		}
		seen[sc.id] = true
		out = append(out, sc)
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// loadScenarios 讀家裡的情境檔；沒有就用內建的預設。第二個回傳值是來源的說明。
func (m *Module) loadScenarios(seat string) ([]toneScenario, string, error) {
	path := filepath.Join(m.home, "personas", "_test", "scenarios.md")
	data, err := os.ReadFile(path)
	source := "personas/_test/scenarios.md"
	if os.IsNotExist(err) {
		data, source = []byte(defaultScenarios), "內建的預設情境"
	} else if err != nil {
		return nil, "", platform.Unavailable("無法讀取說話測試的情境檔")
	}
	list, err := parseScenarios(string(data), seat)
	return list, source, err
}

func (m *Module) toneDir(slug string) string {
	return platform.RuntimeDir(m.home, "persona-tests", slug)
}

// toneStyle 決定這次測試的說話層：回等級、給 TestInstructions 的文字（空＝照目前設定）、檔頭用的說明。
func (m *Module) toneStyle(ctx context.Context, want string) (level, text, label string, err error) {
	cur, _, _, levels, err := m.d.Agents.StyleInfo(ctx)
	if err != nil {
		return "", "", "", err
	}
	want = strings.ToLower(strings.TrimSpace(want))
	describe := func(l string) string {
		if l == "custom" {
			return "自己的（personas/_style.md）"
		}
		return "等級 " + strings.ToUpper(l)
	}
	if want == "" {
		return cur, "", describe(cur), nil
	}
	for _, l := range levels {
		if l.Level != want {
			continue
		}
		if !l.Exists {
			return "", "", "", platform.Invalid("style", "自己的說話層還沒有：先建立 personas/_style.md")
		}
		return l.Level, l.Text, describe(l.Level), nil
	}
	return "", "", "", platform.Invalid("style", "說話層只能是 a、b、c、d、custom")
}

// toneSeat 決定在哪一位秘書的位置測：指定的，否則坐這位人設、編號最小的啟用秘書。
func (m *Module) toneSeat(ctx context.Context, seat, slug, name string) (string, vault.Record, error) {
	if seat = strings.TrimSpace(seat); seat != "" {
		id, ok := m.d.Agents.Resolve(ctx, seat)
		if !ok {
			return "", vault.Record{}, platform.Invalid("seat", "找不到這位秘書："+seat)
		}
		r, err := m.d.Vault.Get(ctx, "agent", id)
		return id, r, err
	}
	rows, err := m.d.Agents.List(ctx)
	if err != nil {
		return "", vault.Record{}, err
	}
	var ids []string
	for _, a := range rows {
		if p, _ := a.Fields["persona"].(string); p != slug {
			continue
		}
		if on, ok := a.Fields["enabled"].(bool); ok && !on {
			continue
		}
		ids = append(ids, a.ID)
	}
	if len(ids) == 0 {
		return "", vault.Record{}, platform.Invalid("seat", name+" 還沒坐任何秘書，請指定要在哪一位的位置測")
	}
	sort.Strings(ids)
	r, err := m.d.Vault.Get(ctx, "agent", ids[0])
	return ids[0], r, err
}

type toneMeta struct {
	status, slug, seat, model, style, reason, checks string
	at                                               time.Time
	scenarios                                        int
}

func (t toneMeta) encode(body string) []byte {
	f := map[string]any{"status": t.status, "at": t.at.Format(time.RFC3339), "slug": t.slug, "seat": t.seat, "model": t.model, "style": t.style, "scenarios": t.scenarios}
	if t.reason != "" {
		f["reason"] = t.reason
	}
	if t.checks != "" {
		f["checks"] = t.checks
	}
	return vault.Encode(vault.Record{Fields: f, Body: body}, []string{"status", "at", "slug", "seat", "model", "style", "scenarios", "reason", "checks"}, vault.Spec{})
}

func messageOf(err error) string {
	var e *platform.Err
	if errors.As(err, &e) {
		return e.Message
	}
	return err.Error()
}

type toneJob struct {
	meta      toneMeta
	path      string
	head      string // 檔案內文的開頭（標題與說明）
	name      string
	scenarios []toneScenario
	instr     []string // 每則自己的指令（情境帶了梗本的才有；空＝用 ask.Instructions）
	engine    EngineAccess
	ask       sdk.EngineAskRequest // 除了 Message 之外都填好了
	release   func()               // 放掉「還在跑」的旗標（可重複呼叫）；寫最終結果前先放，看到 done 就能立刻再開
}

// TestPersona 開始一次測口吻：檢查完就回結果檔名，情境在背景跑（P6-5b）。
func (m *Module) TestPersona(ctx context.Context, req *sdk.PersonasTestRequest) (*sdk.PersonasTestResponse, error) {
	if req == nil || !toneSlugPattern.MatchString(req.Slug) {
		return nil, platform.Invalid("slug", "請指定要測的角色代號")
	}
	m.mu.Lock()
	err := m.ready(ctx)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	slug := req.Slug
	card, _, _, err := m.d.Agents.ShowPersona(ctx, slug)
	if err != nil {
		return nil, err
	}
	name, _, err := m.d.Agents.TrialPersona(slug, req.Card)
	if err != nil {
		return nil, err
	}
	seat, rec, err := m.toneSeat(ctx, req.Seat, slug, card.Name)
	if err != nil {
		return nil, err
	}
	level, styleText, styleLabel, err := m.toneStyle(ctx, req.Style)
	if err != nil {
		return nil, err
	}
	list, source, err := m.loadScenarios(seat)
	if err != nil {
		return nil, err
	}
	if len(req.Only) > 0 {
		var picked []toneScenario
		for _, sc := range list {
			if contains(req.Only, sc.id) {
				picked = append(picked, sc)
			}
		}
		list = picked
	}
	if len(list) == 0 {
		return nil, platform.Invalid("only", "沒有情境可以跑（"+source+"）")
	}
	account, table, engine, err := m.seatAccount(rec)
	if err != nil {
		return nil, err
	}
	instructions, err := m.d.Agents.TestInstructions(ctx, seat, slug, req.Card, styleText, nil)
	if err != nil {
		return nil, err
	}
	perScenario := make([]string, len(list))
	for i, sc := range list {
		if sc.quips == nil {
			continue
		}
		if perScenario[i], err = m.d.Agents.TestInstructions(ctx, seat, slug, req.Card, styleText, sc.quips); err != nil {
			return nil, err
		}
	}
	model, effort := strings.TrimSpace(req.Model), text(rec.Fields, "effort")
	if model == "" {
		model = text(rec.Fields, "model")
	} else {
		effort = "" // 換了模型，座位的思考力度可能不適用（舊系統同樣不帶）
	}
	config, _ := table["engine_config"].(map[string]any)

	now := m.d.Clock.Now().In(m.config.Location())
	dir := m.toneDir(slug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, platform.Unavailable("無法建立說話測試的資料夾")
	}
	m.toneMu.Lock()
	if m.toneRunning[slug] {
		m.toneMu.Unlock()
		return nil, platform.Conflict(name + " 的測試還在跑")
	}
	run := now.Format("2006-01-02-150405")
	file := run + ".md"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, file)); os.IsNotExist(err) {
			break
		}
		file = fmt.Sprintf("%s-%d.md", run, i)
	}
	m.toneRunning[slug] = true
	m.toneMu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { m.toneMu.Lock(); delete(m.toneRunning, slug); m.toneMu.Unlock() }) }

	cardLabel := fmt.Sprintf("`%s`（%s）", toneCardPath(m.home, card, m.d.Agents.OfficialCardFile), toneCardKind(card))
	if strings.TrimSpace(req.Card) != "" {
		cardLabel = "看板上還沒存的內容（原本是 " + cardLabel + "）"
	}
	job := &toneJob{
		meta:    toneMeta{status: "running", slug: slug, seat: seat, model: model, style: level, at: now, scenarios: len(list)},
		release: release, path: filepath.Join(dir, file), name: name, scenarios: list, instr: perScenario, engine: engine,
		ask: sdk.EngineAskRequest{Agent: seat, Cwd: m.workdir(), Account: account, EngineConfig: config, Model: model, Effort: effort, Instructions: instructions, TimeoutSeconds: toneAskSeconds},
	}
	job.head = strings.Join([]string{
		fmt.Sprintf("# %s 說話測試（%s）", name, now.Format("2006-01-02 15:04")), "",
		"- 卡片：" + cardLabel,
		"- 說話層：" + styleLabel,
		fmt.Sprintf("- 座位：`%s`（帳號 %s，模型 %s）", seat, account, model),
		"- 情境：" + source,
		"- 跟常駐一樣的指令；不開工具、不留 session", "",
	}, "\n")
	if err := platform.AtomicWrite(job.path, job.meta.encode(job.head+"\n（測試進行中，跑完會出現在這裡）\n"), 0o600); err != nil {
		release()
		return nil, platform.Unavailable("無法寫入說話測試的結果")
	}
	m.mu.Lock()
	run2 := m.ctx
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer release()
		m.runTone(run2, job)
	}()
	return &sdk.PersonasTestResponse{Run: file, Scenarios: int32(len(list))}, nil
}

// toneCardPath 是報告上寫的卡片位置：主人的卡在家裡 personas/mine/；官方卡由角色庫的找卡入口給（官方資料夾先、沒有才是跟著程式的那張，
// PRS-25），在家裡寫相對路徑，在家外面（第 2 版的狀態資料夾、程式資料夾）寫絕對路徑。
func toneCardPath(home string, c sdk.PersonaCard, official func(string) (string, bool)) string {
	if c.Source == "mine" {
		return "personas/mine/" + c.Slug + ".md"
	}
	abs, _ := official(c.Slug)
	if rel, err := filepath.Rel(home, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return abs
}

func toneCardKind(c sdk.PersonaCard) string {
	switch {
	case c.Source != "mine":
		return "官方"
	case c.Status == "design":
		return "我的，設計中"
	}
	return "我的"
}

// runTone 並行跑完所有情境，把結果原子改寫進檔案。
func (m *Module) runTone(ctx context.Context, job *toneJob) {
	n := len(job.scenarios)
	replies := make([]string, n)
	failures := make([]string, n)
	sem := make(chan struct{}, toneParallel)
	var wg sync.WaitGroup
	for i := range job.scenarios {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if recover() != nil {
					failures[i] = "引擎中斷了"
				}
			}()
			req := job.ask
			if job.instr[i] != "" {
				req.Instructions = job.instr[i]
			}
			req.Message = strings.ReplaceAll(job.scenarios[i].body, "{name}", job.name)
			resp, err := m.ask(ctx, job.engine, &req)
			switch {
			case err != nil:
				failures[i] = messageOf(err)
			case resp == nil || strings.TrimSpace(resp.Reply) == "":
				failures[i] = "引擎沒有回覆"
			default:
				replies[i] = strings.TrimSpace(resp.Reply)
			}
		}(i)
	}
	wg.Wait()

	var b strings.Builder
	b.WriteString(job.head)
	failed := 0
	checked, passed := 0, 0
	var missed, skipped []string
	for i, sc := range job.scenarios {
		var quoted []string
		for _, l := range strings.Split(strings.ReplaceAll(sc.body, "{name}", job.name), "\n") {
			quoted = append(quoted, "> "+l)
		}
		reply := replies[i]
		if failures[i] != "" {
			failed++
			reply = "（這則沒跑成：" + failures[i] + "）"
		}
		fmt.Fprintf(&b, "\n## %s｜%s\n\n%s\n\n%s\n", sc.id, sc.title, strings.Join(quoted, "\n"), reply)
		if sc.quips != nil {
			fmt.Fprintf(&b, "\n（這則開場的梗本換成情境寫的 %d 條：%s）\n", len(sc.quips), quipNames(sc))
		}
		if sc.check == nil {
			continue
		}
		if failures[i] != "" {
			skipped = append(skipped, sc.id)
			fmt.Fprintf(&b, "\n檢查：沒跑成，不算\n")
			continue
		}
		ok, verdict := sc.check.judge(sc, replies[i])
		checked++
		if ok {
			passed++
		} else {
			missed = append(missed, sc.id)
		}
		fmt.Fprintf(&b, "\n檢查：%s\n", verdict)
	}
	meta := job.meta
	meta.status = "done"
	if checked > 0 || len(skipped) > 0 {
		meta.checks = fmt.Sprintf("通過 %d／%d", passed, checked)
		if len(missed) > 0 {
			meta.checks += "（沒過：" + strings.Join(missed, "、") + "）"
		}
		if len(skipped) > 0 {
			meta.checks += "（沒跑成不算：" + strings.Join(skipped, "、") + "）"
		}
	}
	if ctx.Err() != nil {
		meta.status, meta.reason = "failed", toneStopped
	} else if failed == n {
		meta.status, meta.reason = "failed", "每個情境都沒跑成："+firstNonEmpty(failures)
	}
	job.release()
	if err := platform.AtomicWrite(job.path, meta.encode(b.String()), 0o600); err != nil {
		m.warn("寫入說話測試的結果失敗", err)
	}
}

// quipNames 是情境梗本裡每條梗的名字（認得出它的第一個字），報告用。
func quipNames(sc toneScenario) string {
	var names []string
	for _, alts := range sc.quipKeys {
		names = append(names, alts[0])
	}
	return strings.Join(names, "、")
}

func firstNonEmpty(list []string) string {
	for _, s := range list {
		if s != "" {
			return s
		}
	}
	return ""
}

// recoverToneRuns 行程重開時，把留在 running 的測試改成 failed（行程已經不在了）。
func (m *Module) recoverToneRuns() {
	files, _ := filepath.Glob(platform.RuntimeDir(m.home, "persona-tests", "*", "*.md"))
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		r, order, err := vault.Decode(data)
		if err != nil || r.Fields["status"] != "running" {
			continue
		}
		r.Fields["status"], r.Fields["reason"] = "failed", toneInterrupted
		body := strings.Replace(r.Body, "（測試進行中，跑完會出現在這裡）", "（"+toneInterrupted+"）", 1)
		if err := platform.AtomicWrite(path, vault.Encode(vault.Record{Fields: r.Fields, Body: body}, order, vault.Spec{}), 0o600); err != nil {
			m.warn("整理中斷的說話測試失敗", err)
		}
	}
}

// PersonaTestResults 讀這位人設最近幾次的測試結果（新到舊）。
func (m *Module) PersonaTestResults(ctx context.Context, req *sdk.PersonasTestResultsRequest) (*sdk.PersonasTestResultsResponse, error) {
	if req == nil || !toneSlugPattern.MatchString(req.Slug) {
		return nil, platform.Invalid("slug", "請指定角色代號")
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = toneDefaultList
	}
	entries, err := os.ReadDir(m.toneDir(req.Slug))
	if err != nil && !os.IsNotExist(err) {
		return nil, platform.Unavailable("無法讀取說話測試的結果")
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && toneRunPattern.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	// 檔名是時間（同一秒的後綴 -2、-3 放在 .md 前面），新到舊。
	sort.Slice(names, func(i, j int) bool { return toneSortKey(names[i]) > toneSortKey(names[j]) })
	out := &sdk.PersonasTestResultsResponse{Runs: []sdk.PersonaTestRun{}}
	for _, name := range names {
		if len(out.Runs) >= limit {
			break
		}
		data, err := os.ReadFile(filepath.Join(m.toneDir(req.Slug), name))
		if err != nil {
			continue
		}
		r, _, err := vault.Decode(data)
		if err != nil {
			continue // 檔頭壞掉的略過，不擋其他次
		}
		at, _ := time.Parse(time.RFC3339, text(r.Fields, "at"))
		out.Runs = append(out.Runs, sdk.PersonaTestRun{
			Run: name, Status: text(r.Fields, "status"), At: at, Seat: text(r.Fields, "seat"), Model: text(r.Fields, "model"),
			Style: text(r.Fields, "style"), Reason: text(r.Fields, "reason"), Checks: text(r.Fields, "checks"), Markdown: strings.TrimSpace(r.Body),
		})
	}
	return out, nil
}

// toneSortKey：「2026-10-26-140305.md」排在「2026-10-26-140305-2.md」前面，所以把 .md 去掉、沒有後綴補 -1。
func toneSortKey(name string) string {
	base := strings.TrimSuffix(name, ".md")
	if len(base) == len("2006-01-02-150405") {
		return base + "-00001"
	}
	i := strings.LastIndex(base, "-")
	k, _ := strconv.Atoi(base[i+1:])
	return fmt.Sprintf("%s-%05d", base[:i], k)
}
