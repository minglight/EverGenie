package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

const transcriptPollInterval = 500 * time.Millisecond

// projectDirName：平台把工作資料夾的每個非英數字元換成 - 當 transcript 的資料夾名稱（S0 ②）。
func projectDirName(cwd string) string {
	b := []byte(cwd)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// transcriptPath 找這個 session 的 transcript：<config_dir>/projects/<資料夾名>/<id>.jsonl。
// macOS 的 /tmp 之類是連結，平台記的可能是實際路徑，所以兩種都試，最後用 id 在所有專案資料夾裡找。
func transcriptPath(configDir, cwd, id string) string {
	projects := filepath.Join(configDir, "projects")
	cands := []string{cwd}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		cands = append(cands, real)
	}
	for _, c := range cands {
		p := filepath.Join(projects, projectDirName(c), id+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if m, _ := filepath.Glob(filepath.Join(projects, "*", id+".jsonl")); len(m) > 0 {
		return m[0]
	}
	return ""
}

// hasConversation：這個對話檔接得回來（裡面真的有對話）。平台可能只寫了標題這類中繼資料，
// 這種檔 claude --resume 跟檔案不存在一樣回「No conversation found」並退出（2026-11-06 實測）。
func hasConversation(path string) bool {
	if path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		var row struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &row) == nil && (row.Type == "user" || row.Type == "assistant") {
			return true
		}
		if err != nil {
			return false
		}
	}
}

// transcriptRecord 是 session 的對話檔在哪裡。session 停掉或 engine 重開之後記憶裡就沒有它了，
// 但對話記錄還在磁碟上，主人從「去聊」點進已結束的工作 session 要看得到（DEV-22），所以開的時候寫一份，
// 停掉不刪（下次用同名重開會蓋掉）。
type transcriptRecord struct {
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	ConfigDir string `json:"config_dir"`
}

func (e *Engine) rememberTranscript(s *session) {
	data, err := json.Marshal(transcriptRecord{ID: s.id, Cwd: s.cwd, ConfigDir: s.configDir})
	if err != nil {
		return
	}
	_ = writeFileAtomic(sessionFilePath(e.d.Home, "transcripts", s.name), data, 0o644)
}

// transcriptSource 先看記憶裡開著的 session，沒有就讀記下來的對話檔位置；都沒有回 nil。
// 開著的 session 也看紀錄：主人清掉重來（/clear）之後平台換了新的對話檔，hook 把新的編號寫進紀錄（ECL-47），以紀錄為準。
func (e *Engine) transcriptSource(name string) *session {
	r := e.transcriptRecordOf(name)
	e.mu.Lock()
	s := e.sessions[name]
	var live session
	if s != nil {
		live = session{name: s.name, id: s.id, cwd: s.cwd, configDir: s.configDir}
	}
	e.mu.Unlock()
	if s != nil {
		if r != nil && r.ConfigDir == live.configDir && r.ID != live.id {
			live.id = r.ID
		}
		return &live
	}
	if r == nil {
		return e.transcriptFromSessionFile(name)
	}
	return &session{name: name, id: r.ID, cwd: r.Cwd, configDir: r.ConfigDir}
}

// transcriptRecordOf 讀記下來的對話檔位置；沒有或不完整回 nil。
func (e *Engine) transcriptRecordOf(name string) *transcriptRecord {
	return readTranscriptRecord(e.d.Home, name)
}

func readTranscriptRecord(home, name string) *transcriptRecord {
	data, err := os.ReadFile(sessionFilePath(home, "transcripts", name))
	if err != nil {
		return nil
	}
	var r transcriptRecord
	if json.Unmarshal(data, &r) != nil || r.ID == "" || r.ConfigDir == "" {
		return nil
	}
	return &r
}

// transcriptFromSessionFile：engine 重開（部署）之後，重開前就在跑的 session 沒有記憶也沒有對話檔位置紀錄（DEV-24），
// 對話框就成了空的。退路：讀核心記的 <暫存>/sessions/<名稱>.md 的 engine_session_id 與 cwd，
// 再到這個家用過的帳號資料夾（knownConfigDirs）找 <config_dir>/projects/*/<id>.jsonl（id 是 UUID，不會撞）。
// 不掃 ~/.claude*：同一台電腦上另一個家的、或沒設定的帳號資料夾不是這個家的，一律不碰（SEC-373）。
func (e *Engine) transcriptFromSessionFile(name string) *session {
	if !sessionIDPattern.MatchString(name) {
		return nil
	}
	data, err := os.ReadFile(sdk.RuntimeDir(e.d.Home, "sessions", name+".md"))
	if err != nil {
		return nil
	}
	var id, cwd string
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "engine_session_id: "); ok {
			id = strings.Trim(strings.TrimSpace(v), `"`)
		} else if v, ok := strings.CutPrefix(line, "cwd: "); ok {
			cwd = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	if !sessionIDPattern.MatchString(id) {
		return nil
	}
	for _, d := range e.knownConfigDirs() {
		if transcriptPath(d, cwd, id) != "" {
			return &session{name: name, id: id, cwd: cwd, configDir: d}
		}
	}
	return nil
}

// knownConfigDirs 是這個家用過的帳號資料夾：開著的 session 的，加上這個家記下的對話檔位置紀錄（<暫存>/transcripts/*.json）裡的。
// 都是開 session 時照 home.toml 的 engine_config.config_dir 來的，不會有別的家或沒設定的帳號。已排序、不重複。
func (e *Engine) knownConfigDirs() []string {
	seen := map[string]bool{}
	e.mu.Lock()
	for _, s := range e.sessions {
		if s.configDir != "" {
			seen[filepath.Clean(s.configDir)] = true
		}
	}
	e.mu.Unlock()
	paths, _ := filepath.Glob(sdk.RuntimeDir(e.d.Home, "engine-claude", "transcripts", "*.json"))
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".json")
		if r := readTranscriptRecord(e.d.Home, name); r != nil {
			seen[filepath.Clean(r.ConfigDir)] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// Transcript 把 session 的對話記錄解析成 ChatMessage。SinceUuid 之後的才送；Follow 會持續等新的直到 ctx 結束，
// 跟隨時每輪也看一次畫面，把還沒進記錄的那段用 Draft 先送（ECL-61）。
func (e *Engine) Transcript(ctx context.Context, req *sdk.EngineTranscriptRequest, send func(*sdk.EngineTranscriptResponse) error) error {
	if req == nil || strings.TrimSpace(req.Session) == "" {
		return invalid("session", "請指定 session 名稱")
	}
	s := e.transcriptSource(req.Session)
	if s == nil {
		return notFound("沒有這個 session")
	}
	var offset int64
	skipping := req.SinceUuid != ""
	first := true
	// 還在打的那段（ECL-61）：said 是最後一則主人的話之後、秘書已經寫進記錄的文字；draft 是上次送出的。
	var said []string
	draft := ""
	var lingerFrom time.Time // 畫面上那段沒了、正式那則還沒到：最多再留 draftLinger
	for {
		if !first {
			// 跟著看的時候平台可能換了對話檔（主人清掉重來，ECL-47）：換到新的檔、從頭送。
			if next := e.transcriptSource(req.Session); next != nil && next.id != s.id {
				s, offset = next, 0
			}
		}
		path := transcriptPath(s.configDir, s.cwd, s.id)
		if path != "" {
			data, next, err := readComplete(path, offset)
			if err != nil {
				return unavailable("讀不到對話記錄")
			}
			offset = next
			msgs, err := parseAll(data)
			if err != nil {
				return unavailable("對話記錄讀取失敗")
			}
			// 重連時游標之前、同一輪的秘書文字也要算「已經寫進記錄」，不然最後一句會被當成還在打再送一次。
			for i := range msgs {
				said = noteSaid(said, &msgs[i])
			}
			if first && skipping {
				// 游標找不到（檔案被換掉了）就從頭送，不要漏。
				idx := -1
				for i, m := range msgs {
					if m.Uuid == req.SinceUuid {
						idx = i
					}
				}
				if idx >= 0 {
					msgs = msgs[idx+1:]
				}
			}
			first = false
			for i := range msgs {
				if err := send(&sdk.EngineTranscriptResponse{Message: &msgs[i]}); err != nil {
					return err
				}
				if msgs[i].Kind == sdk.ChatKindAssistant || msgs[i].Kind == sdk.ChatKindUser {
					draft, lingerFrom = "", time.Time{} // 看板收到正式的訊息就把淡色那則收掉
				}
			}
		}
		if !req.Follow {
			return nil
		}
		next := e.screenDraft(req.Session, said)
		if next == "" && draft != "" {
			// 秘書剛做完時正式那則通常晚一點才進記錄：淡色那段先留著，免得收掉又冒出來。
			if lingerFrom.IsZero() {
				lingerFrom = e.now()
			}
			if e.now().Sub(lingerFrom) < draftLinger {
				next = draft
			}
		} else {
			lingerFrom = time.Time{}
		}
		if next != draft {
			draft = next
			if err := send(&sdk.EngineTranscriptResponse{Message: &sdk.ChatMessage{Kind: sdk.ChatKindAssistant, Text: next, Ts: e.now(), Draft: true}}); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return nil
		}
		e.sleep(transcriptPollInterval)
		if err := ctx.Err(); err != nil {
			return nil
		}
	}
}

// readComplete 從 offset 讀到最後一個換行（寫到一半的行留給下一次）。
func readComplete(path string, offset int64) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() < offset {
		offset = 0 // 檔案變短＝被換掉了，重讀
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, offset, nil
	}
	return data[:end+1], offset + int64(end+1), nil
}

func parseAll(data []byte) ([]sdk.ChatMessage, error) {
	var out []sdk.ChatMessage
	for m, err := range Parse(bytes.NewReader(data)) {
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// transcriptTail 是重查時最多讀對話記錄的尾巴多少（ECL-39）：剛貼的話一定在最後面。
const transcriptTail = 512 << 10

// transcriptTailOf 讀對話記錄最後 transcriptTail 的完整幾行（第一行不完整的跳過）；讀不到回 nil。
func transcriptTailOf(path string) []byte {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	offset := info.Size() - transcriptTail
	if offset < 0 {
		offset = 0
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	if offset > 0 { // 從中間開始讀：第一行不完整，跳過
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil
	}
	return data[:end+1]
}

// inTranscript：session 的對話記錄裡，since（往前放寬 slack）之後有沒有一則使用者的話含有 text 的開頭（ECL-39、ECL-42）。
// 重查放寬 2 秒（transcriptSlack）；Deliver 剛送出時的確認不放寬（同一句 2 秒內送兩次不能被前一則對上）。
func (e *Engine) inTranscript(name, text string, since time.Time, slack time.Duration) bool {
	s := e.transcriptSource(name)
	if s == nil {
		return false
	}
	data := transcriptTailOf(transcriptPath(s.configDir, s.cwd, s.id))
	if data == nil {
		return false
	}
	msgs, err := parseAll(data)
	if err != nil {
		return false
	}
	probe := probeText(text, 40)
	for _, m := range msgs {
		if m.Kind == sdk.ChatKindUser && !m.Ts.Before(since.Add(-slack)) && strings.Contains(m.Text, probe) {
			return true
		}
	}
	return false
}

// draftLinger：秘書做完、畫面上的轉圈沒了，淡色那段最多再留多久等正式那則進記錄。
const draftLinger = 3 * time.Second

// noteSaid 記下這一輪（最後一則主人的話之後）秘書已經寫進記錄的文字，比對畫面上的話用。
func noteSaid(said []string, m *sdk.ChatMessage) []string {
	switch m.Kind {
	case sdk.ChatKindUser:
		return nil
	case sdk.ChatKindAssistant:
		if k := draftKey(m.Text); k != "" {
			return append(said, k)
		}
	}
	return said
}

// screenDraft 是畫面上還沒進記錄的那段話；session 不在、抓不到畫面、或已經在記錄裡都回空字串。
func (e *Engine) screenDraft(name string, said []string) string {
	// 不另外問 session 在不在：抓不到畫面本來就回空字串，少開一個 tmux 行程。
	if e.d.Tmux == nil {
		return ""
	}
	screen, err := e.d.Tmux.Capture(name)
	if err != nil {
		return ""
	}
	d := draftFromScreen(screen)
	if draftCovered(d, said) {
		return ""
	}
	return d
}
