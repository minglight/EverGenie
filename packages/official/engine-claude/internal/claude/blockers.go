package claude

import (
	"fmt"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 對話中途擋住輸入框的平台選單（ECL-66，DEV-159）。
// 2026-11-14 事件：平台 2.1.323 在一輪結束後跳出「Teach auto mode about your environment?」，輸入框被蓋住，
// 核心的話貼不進去、重試了上千次，主人只看到逾時。按 Esc 選單就關了、之後貼得進去（輝夜手動驗過）。
//
// 只處理清單裡的畫面：每一項寫清楚「由上到下一定有的幾行」與「怎麼取消」。不在清單裡的畫面一個鍵都不按，
// 改由 stuck（ECL-67）告訴主人。之後平台再冒出別的選單，在 screenBlockers 加一項、補一個畫面樣本與測試。

type screenBlocker struct {
	// label 是給主人看的白話（看板的提示、紀錄檔）。
	label string
	// lines：選單由上到下一定有的幾行，各比「那一行去掉前後空白與游標（❯）之後的開頭」；要照順序、擠在 blockerSpan 行以內。
	lines []string
	// keys：取消這個選單要按的鍵（照順序）。只放「取消、什麼都不改」的鍵，不替主人選任何選項。
	keys []string
}

var screenBlockers = []screenBlocker{
	{
		label: "平台跳出選單「Teach auto mode about your environment?」（問要不要教自動模式認識環境）",
		lines: []string{"Teach auto mode about your environment?", "1. Yes", "2. Not now", "3. Don't show again", "Enter to confirm"},
		keys:  []string{"Escape"},
	},
}

const (
	// blockerSpan：選單第一行到最後一行最多隔幾行（選單本身 6 行，留一點給空行與折行）。
	blockerSpan = 12
	// blockerTrailNoBox：畫面上找不到輸入框時，選單下面最多還能有幾行有字的（狀態列）還算真的選單。
	blockerTrailNoBox = 2
	// blockerChecks：按了取消鍵之後最多看幾次畫面確認它收掉（每次隔 answerSettle）。
	blockerChecks = 3
)

// blockerAt 是 findBlocker 找到的選單：第一行與最後一行在畫面上的行號。
type blockerAt struct {
	b        *screenBlocker
	top, end int
	lines    []string
}

// findBlocker 在畫面上找清單裡的選單（找最下面那一個）；找不到回 nil。只比字，不判斷它是不是真的開著
// （秘書們會在對話裡引用這個選單的字）：那要看位置，atBottom、blocking。
func findBlocker(screen string) *blockerAt {
	lines := strings.Split(screen, "\n")
	norm := make([]string, len(lines))
	for i, l := range lines {
		norm[i] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), dialogCursor))
	}
	var at *blockerAt
	for k := range screenBlockers {
		want := screenBlockers[k].lines
		for top := len(norm) - 1; top >= 0; top-- {
			if !strings.HasPrefix(norm[top], want[0]) {
				continue
			}
			end, ok := top, true
			for _, w := range want[1:] {
				ok = false
				for j := end + 1; j < len(norm) && j <= top+blockerSpan; j++ {
					if strings.HasPrefix(norm[j], w) {
						end, ok = j, true
						break
					}
				}
				if !ok {
					break
				}
			}
			if ok {
				if at == nil || end > at.end {
					at = &blockerAt{b: &screenBlockers[k], top: top, end: end, lines: lines}
				}
				break
			}
		}
	}
	return at
}

// textLines 數 from（含）到 to（不含）之間有字的行（框線也算字）。
func (a *blockerAt) textLines(from, to int) int {
	n := 0
	for i := from; i < to && i < len(a.lines); i++ {
		if strings.TrimSpace(a.lines[i]) != "" {
			n++
		}
	}
	return n
}

// atBottom：選單就是畫面最底下的東西（下面一個字都沒有）：它取代了輸入框，跟確認框一樣。貼上前只認這一種。
func (a *blockerAt) atBottom() bool {
	return a != nil && a.textLines(a.end+1, len(a.lines)) == 0
}

// blocking：貼了之後字沒進輸入框時，這個選單的位置像不像真的開著（而不是對話裡引用的字）：
//   - 在畫面最底下；
//   - 看得到輸入框：選單畫在輸入框裡面（兩條橫線中間），或緊貼在輸入框上緣的上面（中間沒有別的字）——
//     引用的字後面一定還有對話的其他行（她接著說的話、花了多久）；
//   - 看不到輸入框：選單下面最多 blockerTrailNoBox 行字（狀態列）。
func (a *blockerAt) blocking() bool {
	if a == nil {
		return false
	}
	if a.atBottom() {
		return true
	}
	bottom, top := -1, -1
	for i := len(a.lines) - 1; i >= 0; i-- {
		if !isRule(a.lines[i]) {
			continue
		}
		if bottom < 0 {
			bottom = i
			continue
		}
		top = i
		break
	}
	if top < 0 {
		return a.textLines(a.end+1, len(a.lines)) <= blockerTrailNoBox
	}
	if a.top > top && a.end < bottom {
		return true
	}
	return a.end < top && a.textLines(a.end+1, top) == 0
}

// dismissBlocker 按這個選單的取消鍵，再看畫面確認它收掉了。回收掉之後的畫面；ok=false＝按不了、看不到畫面或它還在。
// 同一個 session 按過、它還在，就不再按（記在 cancelTried；送進去了或 session 重開才重新算）：之後每次 Deliver 都按一次
// 等於一直對著畫面按 Esc，改由 ECL-67 的提示交給主人。呼叫時已拿著這個 session 的鎖。
func (e *Engine) dismissBlocker(name string, b *screenBlocker) (screen string, ok bool) {
	e.mu.Lock()
	tried := e.cancelTried[name]
	e.cancelTried[name] = true
	e.mu.Unlock()
	if tried {
		return "", false
	}
	for _, k := range b.keys {
		if err := e.d.Tmux.SendKey(name, k); err != nil {
			return "", false
		}
	}
	for i := 0; i < blockerChecks; i++ {
		e.sleep(answerSettle)
		after, err := e.d.Tmux.Capture(name)
		if err != nil {
			return "", false
		}
		if findBlocker(after) == nil {
			e.mu.Lock()
			delete(e.cancelTried, name)
			e.mu.Unlock()
			e.logf("%s：%s，已經替她按取消（%s）", name, b.label, strings.Join(b.keys, "、"))
			return after, true
		}
	}
	return "", false
}

// 貼不進去不能默默重試（ECL-67，DEV-159）：同一個 session 連續 stuckAfter 都沒把話送進去，就登記一個提示（跟信任資料夾
// 同一條路：看板最上面的橫幅、健檢），寫明哪位秘書被什麼擋住；送進去了就自動撤掉。
const (
	// stuckAfter：連續多久貼不進去才告訴主人。核心退避到 10 秒一次（SES-51），平常的暫時狀況（畫面還沒畫好、
	// 主人在終端機打了一半的字、她剛好在收尾）幾次重試內就過了；2 分鐘大約連續 10 次都失敗，不是暫時的，
	// 又比主人放棄等待的時間短。
	stuckAfter = 2 * time.Minute
	// stuckStale：這麼久沒有再試（那句話被主人取消了）就撤掉提示。核心平常 10 秒多就再交來一次，但有幾種情況會先不交
	// （主人剛說過話的 3 分鐘內別人的話先等、她在等主人回答）；取 5 分鐘，提示才不會在這些空檔裡消失又出現。
	stuckStale = 5 * time.Minute

	stuckSuffix = "#blocked"
	// bootChoiceEscape：貼不進去的提示只給一個選項，由主人決定要不要按（引擎不認得的畫面不自己按鍵）。
	bootChoiceEscape = "按 Esc 關掉它（秘書正在做事的話會停下）"
)

type stuckState struct {
	since time.Time // 連續貼不進去是從什麼時候開始的
	last  time.Time // 最後一次沒貼進去
}

// noteDelivery 記這次 Deliver 的結果（呼叫時已拿著這個 session 的鎖、沒拿 e.mu）。
// 送進去了、她不在了：撤掉；有確認框（dialog）不算貼不進去——核心已經把它當「等你回答」顯示在看板。
func (e *Engine) noteDelivery(name string, r *sdk.EngineDeliverResponse) {
	if r == nil {
		return
	}
	if r.Delivered || r.Reason == "gone" || r.Reason == "dialog" {
		e.mu.Lock()
		e.clearStuck(name)
		e.mu.Unlock()
		return
	}
	now := e.now()
	e.mu.Lock()
	st := e.stuck[name]
	if st == nil {
		st = &stuckState{since: now}
		e.stuck[name] = st
	}
	st.last = now
	due := !now.Before(st.since.Add(stuckAfter))
	since := st.since
	e.mu.Unlock()
	if !due {
		return
	}
	what := e.blockedBy(name)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stuck[name] != st {
		return // 這段時間她被關掉或重開了
	}
	id := name + stuckSuffix
	first := e.prompts[id] == nil
	e.prompts[id] = &bootPrompt{BootPrompt: sdk.BootPrompt{
		ID: id, Session: name,
		Title:   fmt.Sprintf("已經 %d 分鐘貼不進秘書的對話，要給秘書的話都還排著。%s", int(now.Sub(since)/time.Minute), what),
		Choices: []string{bootChoiceEscape},
	}}
	if first {
		defer e.logf("%s：從 %s 起連續貼不進她的對話；%s", name, since.Format("15:04:05"), what) // 放掉 e.mu 之後才寫檔
	}
}

// clearStuck 撤掉「貼不進去」的紀錄與提示（呼叫時持有 e.mu）。
func (e *Engine) clearStuck(name string) {
	delete(e.stuck, name)
	delete(e.cancelTried, name)
	delete(e.prompts, name+stuckSuffix)
}

// pruneStuck：好一陣子沒有再試的就撤掉（那句話不在了，沒有下一次 Deliver 來替它撤）。
func (e *Engine) pruneStuck() {
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	for name, st := range e.stuck {
		if !now.Before(st.last.Add(stuckStale)) {
			e.clearStuck(name)
		}
	}
}

// blockedBy 現場看一次畫面，說出是什麼擋著：清單裡的選單說名字；不認得的附上畫面最後幾行，讓主人自己判斷。
func (e *Engine) blockedBy(name string) string {
	screen, err := e.d.Tmux.Capture(name)
	if err != nil {
		return "看不到秘書的畫面。"
	}
	if at := findBlocker(screen); at.blocking() {
		return "擋住的是：" + at.b.label + "，按了取消還在。"
	}
	tail := screenTail(screen, 4, 120)
	if tail == "" {
		return "畫面上看不出是什麼擋著。"
	}
	return "畫面上可能有引擎不認得的東西擋著（不認得的不會自己按）。畫面最後幾行：「" + tail + "」"
}

// screenTail：畫面最後 n 行有字的（框線不算），用「 ⏎ 」接起來，最多 max 個字。
func screenTail(screen string, n, max int) string {
	var picked []string
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0 && len(picked) < n; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" || isRule(t) {
			continue
		}
		picked = append([]string{strings.Join(strings.Fields(t), " ")}, picked...)
	}
	r := []rune(strings.Join(picked, " ⏎ "))
	if len(r) > max {
		r = append(r[:max], '…')
	}
	return string(r)
}

// answerStuck：主人在「貼不進去」的提示上選了按 Esc。按之前確認提示還在（剛好送進去了就不按）。
func (e *Engine) answerStuck(p *bootPrompt, choice string) error {
	if choice != bootChoiceEscape {
		return invalid("choice", "不認得這個選項")
	}
	l := e.lockFor(p.Session)
	l.Lock()
	defer l.Unlock()
	e.mu.Lock()
	still := e.prompts[p.ID] != nil
	e.mu.Unlock()
	if !still {
		return notFound("這個提示已經不在了")
	}
	if err := e.d.Tmux.SendKey(p.Session, "Escape"); err != nil {
		return unavailable(fmt.Sprintf("按鍵送不出去：%v", err))
	}
	e.logf("%s：主人從看板選了按 Esc（貼不進她的對話）", p.Session)
	e.mu.Lock()
	e.clearStuck(p.Session) // 重新算：還是貼不進去的話，stuckAfter 之後再提示一次
	e.mu.Unlock()
	return nil
}
