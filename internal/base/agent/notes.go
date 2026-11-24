package agent

// 全家共用的主題筆記（DEV-161；規格 ai_docs/schemas/note.md，法律第 1.5、2 條）：
// vault/notes/<分類>/<主題>.md 一個主題一份，分類資料夾各有說明檔 <分類>/<分類>.md。任何秘書與主人都能寫，
// 每一行內文的尾巴標是誰、哪天寫的；寫入一律經 vault（Put），核心照檔頭產生兩層索引（總索引、職位索引）。

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

const (
	// TopicKind、CategoryKind 是主題筆記與分類說明的紀錄種類（schemas/note.md）。
	// 種類名 note 已經是秘書自己的做事筆記（vault/agents/<職位>/notes/），所以主題筆記叫 topic。
	TopicKind    = "topic"
	CategoryKind = "topic_category"
	// OwnerTopic 是固定主題「主人」（取代退休的 vault/profile.md）：所有職位開場都讀全文，算全部職位跟進。
	OwnerTopicName     = "主人"
	OwnerTopicCategory = "人"
	OwnerTopicID       = OwnerTopicCategory + "/" + OwnerTopicName
	ownerTopicSummary  = "主人自己：身分、家人、目標、偏好這類穩定的事實"
	// MemoryCoreLines 是記憶核心（memory.md）的行數上限預設值；以前的預設 200 寫進檔頭的當成沒設（schemas/memory.md）。
	MemoryCoreLines   = 120
	legacyMemoryLines = 200
	ownerMark         = "主人" // 主人寫的行標「（主人 MM-DD）」
)

// DefaultCategory 是官方預設的分類：核心啟動時缺的就建（只建缺的，主人改過的不動）。
type DefaultCategory struct{ Name, Summary, Body string }

// DefaultCategories 是官方預設的五個分類（schemas/note.md）。說明檔裡的「建議段落」只是建議（DEV-161）：不驗證、舊的說明檔與筆記不動。
var DefaultCategories = []DefaultCategory{
	{"人", "主人和他身邊的人：家人、朋友、同事，一個重要的人一份", "放什麼：主人自己（固定主題「主人」）和他身邊重要的人，一個人一份主題（例：伴侶、孩子、主管）。寫這個人是誰、跟主人的關係、說過的話與要注意的事。\n\n" + suggestedSections("他是誰", "喜好與禁忌", "最近的事") + "\n\n誰常用：所有職位；生活秘書記家人，工作秘書記同事。"},
	{"東西", "主人用的東西：器材、工具、電腦、帳號（不寫密碼）", "放什麼：主人用的東西，一樣一份主題（例：健身器材、電腦、車）。寫怎麼用、壞過什麼、主人的評價。套件已經有紀錄的（例：器材清單）只放連結，不抄。不寫密碼與 token。\n\n" + suggestedSections("設定", "上次的感想", "要注意的") + "\n\n誰常用：電腦秘書記電腦與工具，生活秘書記家裡的東西。"},
	{"地方", "家、公司、常去的店與地點", "放什麼：主人常去或在意的地方，一個地方一份主題（例：公司、常去的餐廳、診所）。寫在哪、怎麼去、主人的習慣與評價。\n\n" + suggestedSections("怎麼去", "注意的事", "上次去的感想") + "\n\n誰常用：生活秘書、工作秘書。"},
	{"專案", "主人工作或生活上的長期專案", "放什麼：會持續一段時間的事，一個專案一份主題（例：搬家、某個工作專案）。寫目標、現況、決定過的事；任務單與 Goal 只放連結。\n\n" + suggestedSections("目標", "現況", "決定過的事") + "\n\n誰常用：工作秘書、寫程式的秘書、生活秘書。"},
	{"喜好", "主人的偏好與習慣：吃的、用的、說話方式", "放什麼：主人的偏好，一類一份主題（例：飲食、回覆方式、穿著）。寫喜歡什麼、不喜歡什麼、主人糾正過什麼。\n\n" + suggestedSections("喜歡", "不喜歡", "糾正過的") + "\n\n誰常用：所有職位。"},
}

// suggestedSections 是分類說明檔裡「建議段落」那一段：主題內容多了以後用這幾個 `## 標題` 整理（只是建議，不強制）。
func suggestedSections(names ...string) string {
	heads := make([]string, len(names))
	for i, n := range names {
		heads[i] = "`## " + n + "`"
	}
	return "建議段落：" + strings.Join(heads, "、") + "。只是建議、不強制：一開始用 `evergenie notes add` 一行一行加就好（會接在最後），內容多了再用 `evergenie notes set` 照這幾段整理。"
}

// noteSpecs 是主題筆記與分類說明的資料種類宣告（schemas/note.md）；欄位照檔頭的順序。
func noteSpecs() []vault.Spec {
	meta := []vault.Field{{Name: "updated_at", Type: vault.DateTime, Required: true}, {Name: "by", Type: vault.String, Required: true}}
	topic := vault.Spec{Kind: TopicKind, Fields: append([]vault.Field{
		{Name: "schema", Type: vault.Int, Required: true},
		{Name: "name", Type: vault.String, Required: true},
		{Name: "aliases", Type: vault.List, Default: []string{}},
		{Name: "summary", Type: vault.String, Required: true},
		{Name: "follow", Type: vault.List, Default: []string{}},
		{Name: "links", Type: vault.List, Default: []string{}},
	}, meta...), Path: func(id string) string {
		cat, name, ok := strings.Cut(id, "/")
		if !ok || cat == "" || name == "" || strings.Contains(name, "/") {
			return ""
		}
		return "vault/notes/" + cat + "/" + name + ".md"
	}}
	category := vault.Spec{Kind: CategoryKind, Fields: append([]vault.Field{
		{Name: "schema", Type: vault.Int, Required: true},
		{Name: "kind", Type: vault.Enum, Required: true, Default: "category", Enum: []string{"category"}},
		{Name: "summary", Type: vault.String, Required: true},
	}, meta...), Path: func(id string) string {
		if id == "" || strings.Contains(id, "/") {
			return ""
		}
		return "vault/notes/" + id + "/" + id + ".md"
	}}
	return []vault.Spec{topic, category}
}

// ---- 名字、標記 ----

const badNameChars = "/\\:*?\"<>|[]#^\r\n\t"

// checkNoteName：主題與分類的名字就是檔名（Obsidian 用檔名連結 [[名字]]），要能當檔名、不跟核心保留的名字撞。
func checkNoteName(field, name string) error {
	switch {
	case name == "":
		return platform.Invalid(field, "名字不能是空的")
	case strings.TrimSpace(name) != name:
		return platform.Invalid(field, "名字前後不要有空白")
	case strings.ContainsAny(name, badNameChars):
		return platform.Invalid(field, "名字不能有 / \\ : * ? \" < > | [ ] # ^ 這些符號（名字就是檔名，也是 [[連結]]）")
	case strings.HasPrefix(name, "."), strings.Contains(name, ".bak-"):
		return platform.Invalid(field, "名字不能用 . 開頭，也不能含 .bak-（這兩種檔案核心不看）")
	case name == strings.TrimSuffix(vault.IndexFileName, ".md"), name == "archive":
		return platform.Invalid(field, "「"+name+"」是核心保留的名字，換一個")
	case utf8.RuneCountInString(name) > 60:
		return platform.Invalid(field, "名字太長（最多 60 個字）")
	}
	return nil
}

// markPattern 是行尾的標記「（<誰> MM-DD）」。
var markPattern = regexp.MustCompile(`（[^（）\s]+ [0-9]{2}-[0-9]{2}）\s*$`)

// contentLine：這一行算不算一件事（要有標記）。空行、標題（#）、分隔線不算。
func contentLine(line string) bool {
	t := strings.TrimSpace(line)
	return t != "" && !strings.HasPrefix(t, "#") && t != "---"
}

// UnmarkedLines 回內文裡沒有標記的行號（從 1 起算）。
func UnmarkedLines(body string) []int {
	var out []int
	for i, line := range strings.Split(body, "\n") {
		if contentLine(line) && !markPattern.MatchString(line) {
			out = append(out, i+1)
		}
	}
	return out
}

// newUnmarkedLines 回新內文裡沒有標記、而且舊內文沒有的行號：主人自己手寫的行可以沒有標記（法律第 1.5 條只要求秘書留名），
// 秘書整理時原樣保留這些行不算違規；秘書自己新寫的行一定要標。
func newUnmarkedLines(old, body string) []int {
	had := map[string]bool{}
	for _, l := range strings.Split(old, "\n") {
		had[strings.TrimSpace(l)] = true
	}
	var out []int
	for _, n := range UnmarkedLines(body) {
		if !had[strings.TrimSpace(strings.Split(body, "\n")[n-1])] {
			out = append(out, n)
		}
	}
	return out
}

// foreignMarkedLines 回 body 裡新出現（old 沒有）、行尾標的卻不是 who 的行號：整理時保留別人原本的行可以，
// 新寫的行不能標成別人（不能冒名，法律第 1.5 條）。
func foreignMarkedLines(old, body, who string) []int {
	had := map[string]bool{}
	for _, l := range strings.Split(old, "\n") {
		had[strings.TrimSpace(l)] = true
	}
	var out []int
	for i, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		if !contentLine(l) || had[t] {
			continue
		}
		if mk := markPattern.FindString(t); mk != "" {
			name, _, _ := strings.Cut(strings.TrimSpace(strings.Trim(strings.TrimSpace(mk), "（）")), " ")
			if name != who {
				out = append(out, i+1)
			}
		}
	}
	return out
}

// MarkLine 在一行尾巴補上「（<誰> MM-DD）」；已經有標記就原樣回。
func MarkLine(line, who, monthDay string) string {
	line = strings.TrimRight(line, " \t")
	if markPattern.MatchString(line) {
		return line
	}
	return line + "（" + who + " " + monthDay + "）"
}

// ---- 讀 ----

func fieldString(r vault.Record, key string) string { s, _ := r.Fields[key].(string); return s }

// fieldList 回檔頭的清單欄位（YAML 讀進來是 []any，程式寫的是 []string）。
func fieldList(r vault.Record, key string) []string {
	out := []string{}
	switch xs := r.Fields[key].(type) {
	case []string:
		out = append(out, xs...)
	case []any:
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// TopicName 回主題的名字（檔頭 name；沒有就用檔名）。
func TopicName(r vault.Record) string {
	if n := fieldString(r, "name"); n != "" {
		return n
	}
	_, name, _ := strings.Cut(r.ID, "/")
	return name
}

// TopicCategory 回主題放在哪個分類（id 的前半）。
func TopicCategory(r vault.Record) string { cat, _, _ := strings.Cut(r.ID, "/"); return cat }

// follows：這個職位跟不跟進這份主題（「主人」與梗本算全部職位跟進）。
func follows(r vault.Record, agent string) bool {
	return followedByAll(r) || slices.Contains(fieldList(r, "follow"), agent)
}

// followedByAll：「主人」與梗本（名字或別名是「梗本」的那一份，DEV-164）每個職位都跟進。
func followedByAll(r vault.Record) bool { return r.ID == OwnerTopicID || isQuipBook(r) }

func (m *Module) topics(ctx context.Context) ([]vault.Record, error) {
	return m.d.Vault.List(ctx, TopicKind, vault.Query{})
}

func (m *Module) categories(ctx context.Context) ([]vault.Record, error) {
	return m.d.Vault.List(ctx, CategoryKind, vault.Query{})
}

// findTopic 用名字或別名找主題：先比名字（檔名），再比別名；別名對到好幾份回 CONFLICT。
func (m *Module) findTopic(ctx context.Context, name string) (vault.Record, bool, error) {
	rows, err := m.topics(ctx)
	if err != nil {
		return vault.Record{}, false, err
	}
	for _, r := range rows {
		if TopicName(r) == name || strings.HasSuffix(r.ID, "/"+name) {
			return r, true, nil
		}
	}
	var hits []vault.Record
	for _, r := range rows {
		if slices.Contains(fieldList(r, "aliases"), name) {
			hits = append(hits, r)
		}
	}
	switch len(hits) {
	case 0:
		return vault.Record{}, false, nil
	case 1:
		return hits[0], true, nil
	}
	names := []string{}
	for _, r := range hits {
		names = append(names, TopicName(r))
	}
	return vault.Record{}, false, platform.Conflict("「" + name + "」是好幾份主題的別名（" + strings.Join(names, "、") + "），請用名字")
}

// ---- 寫 ----

// noteWriter 檢查寫的人並回行尾標記用的名字：主人標「主人」、核心標 system、秘書標自己的職位 id（要在名冊上）。
func (m *Module) noteWriter(ctx context.Context, by string) (string, error) {
	switch {
	case platform.IsOwner(by):
		return ownerMark, nil
	case by == "system":
		return by, nil
	}
	r, err := m.d.Vault.Get(ctx, "agent", by)
	if err != nil {
		if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			return "", platform.Forbidden("主題筆記要由秘書（在自己的對話裡，身分是職位 id）或主人寫")
		}
		return "", err
	}
	if r.Archived {
		return "", platform.Forbidden("職位「" + by + "」已經封存，不能寫主題筆記")
	}
	return by, nil
}

func (m *Module) monthDay() string { return m.d.Clock.Now().In(m.loc).Format("01-02") }

// checkFollow：跟進的職位要在名冊上。
func (m *Module) checkFollow(ctx context.Context, follow []string) ([]string, error) {
	out := []string{}
	for _, id := range follow {
		id = strings.TrimSpace(id)
		if id == "" || slices.Contains(out, id) {
			continue
		}
		a, err := m.d.Vault.Get(ctx, "agent", id)
		if err != nil {
			if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
				return nil, platform.Invalid("follow", "跟進的職位「"+id+"」不在名冊上")
			}
			return nil, err
		}
		if a.Archived {
			return nil, platform.Invalid("follow", "跟進的職位「"+id+"」已經封存")
		}
		out = append(out, id)
	}
	return out, nil
}

// cleanList 去掉空白與重複，照原順序。
func cleanList(xs []string) []string {
	out := []string{}
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// cleanLinks 把連結寫成 [[…]]（已經是就不動）。
func cleanLinks(xs []string) []string {
	out := []string{}
	for _, x := range cleanList(xs) {
		if !strings.HasPrefix(x, "[[") {
			x = "[[" + strings.TrimSuffix(x, "]]") + "]]"
		}
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// AddNote 在主題尾巴加一行（自動補「（<寫的人> MM-DD）」）；主題還沒有就照 req 的分類、一句說明建一份。
// 新主題沒給分類拒絕（「主人」例外，固定在「人」）；分類要已經有了（新分類先問主人，再 AddNoteCategory）。
func (m *Module) AddNote(ctx context.Context, req *sdk.NotesAddRequest, by string) (*sdk.NotesAddResponse, error) {
	who, err := m.noteWriter(ctx, by)
	if err != nil {
		return nil, err
	}
	line := strings.TrimSpace(req.Line)
	switch {
	case line == "":
		return nil, platform.Invalid("line", "要寫的那一行是空的")
	case strings.ContainsAny(line, "\r\n"):
		return nil, platform.Invalid("line", "一次只寫一行；整份整理用 notes set")
	}
	name := strings.TrimSpace(req.Name)
	m.notesMu.Lock()
	defer m.notesMu.Unlock()
	r, found, err := m.findTopic(ctx, name)
	if err != nil {
		return nil, err
	}
	created := false
	if found {
		if req.Category != "" && req.Category != TopicCategory(r) {
			return nil, platform.Invalid("category", "主題「"+TopicName(r)+"」已經在分類「"+TopicCategory(r)+"」裡；要改檔頭用 notes set")
		}
	} else {
		if r, err = m.newTopic(ctx, name, req, who, by); err != nil {
			return nil, err
		}
		created = true
	}
	// 送進來的行尾標記先拿掉，一律標寫的人（不能冒名）。
	line = strings.TrimSpace(markPattern.ReplaceAllString(line, ""))
	if line == "" {
		return nil, platform.Invalid("line", "要寫的那一行是空的")
	}
	if name == QuipTopicName || isQuipBook(r) {
		if err := quipNeedsSource(line, who); err != nil {
			return nil, err
		}
	}
	line = MarkLine(line, who, m.monthDay())
	body := strings.TrimRight(r.Body, "\n")
	if body != "" {
		body += "\n"
	}
	r.Body = body + line + "\n"
	out, err := m.d.Vault.Put(ctx, r, by, r.Revision)
	if err != nil {
		return nil, err
	}
	if err := m.rebuildIndexesLocked(ctx); err != nil {
		m.warn("主題筆記的索引重產失敗", err)
	}
	return &sdk.NotesAddResponse{Topic: &out, Created: created, Line: line}, nil
}

// newTopic 組一份還沒寫入的新主題（呼叫者持有 notesMu）。
func (m *Module) newTopic(ctx context.Context, name string, req *sdk.NotesAddRequest, who, by string) (vault.Record, error) {
	if err := checkNoteName("name", name); err != nil {
		return vault.Record{}, err
	}
	cat, summary := strings.TrimSpace(req.Category), strings.TrimSpace(req.Summary)
	if name == OwnerTopicName {
		if cat == "" {
			cat = OwnerTopicCategory
		}
		if summary == "" {
			summary = ownerTopicSummary
		}
		if cat != OwnerTopicCategory {
			return vault.Record{}, platform.Invalid("category", "「主人」固定放在分類「人」")
		}
	}
	if name == QuipTopicName { // 梗本（DEV-164）：第一次記的時候建，分類與說明有預設，算全部職位跟進
		if cat == "" {
			cat = QuipTopicCategory
		}
		if summary == "" {
			summary = quipTopicSummary
		}
	}
	if cat == "" {
		return vault.Record{}, platform.Invalid("category", "「"+name+"」是新主題，要給分類（--category，例：人、東西、地方、專案、喜好）")
	}
	if summary == "" {
		return vault.Record{}, platform.Invalid("summary", "「"+name+"」是新主題，要給一句說明（--summary），索引才看得懂")
	}
	if cat == name {
		return vault.Record{}, platform.Invalid("name", "主題不能跟分類同名（那是分類的說明檔）")
	}
	if _, err := m.d.Vault.Get(ctx, CategoryKind, cat); err != nil {
		if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			return vault.Record{}, platform.Invalid("category", "沒有分類「"+cat+"」；要新分類先問主人一次，再用 evergenie notes categories add")
		}
		return vault.Record{}, err
	}
	if m.ownerFileAt("vault/notes/" + cat + "/" + name + ".md") {
		return vault.Record{}, platform.Conflict("vault/notes/" + cat + "/" + name + ".md 是主人自己的檔（不是主題筆記格式），不蓋它；換個名字")
	}
	follow := req.Follow
	if len(follow) == 0 && who != ownerMark && who != "system" {
		follow = []string{by}
	}
	follow, err := m.checkFollow(ctx, follow)
	if err != nil {
		return vault.Record{}, err
	}
	if name == OwnerTopicName || name == QuipTopicName {
		follow = []string{} // 「主人」與梗本算全部職位跟進（開場都帶），不用列
	}
	return vault.Record{Kind: TopicKind, ID: cat + "/" + name, Fields: map[string]any{
		"name": name, "aliases": cleanList(req.Aliases), "summary": summary, "follow": follow, "links": cleanLinks(req.Links),
	}}, nil
}

// SetNote 整份取代主題的內文（整理用，要帶 Revision）及／或改檔頭。內文有沒標記的行就拒絕並列出行號（主人寫的不檢查）。
func (m *Module) SetNote(ctx context.Context, req *sdk.NotesSetRequest, by string) (*sdk.NotesSetResponse, error) {
	who, err := m.noteWriter(ctx, by)
	if err != nil {
		return nil, err
	}
	m.notesMu.Lock()
	defer m.notesMu.Unlock()
	r, found, err := m.findTopic(ctx, strings.TrimSpace(req.Name))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, platform.NotFound("沒有主題「" + req.Name + "」；新主題用 notes add 建")
	}
	// 整份取代內文一定要帶讀到的版本：兩邊前後各整理同一份時，後寫的不能無聲蓋掉前一位剛加的行。
	// 只改檔頭（KeepBody）不碰內文，不用帶；帶了就照樣檢查。
	if !req.KeepBody && req.Revision == "" {
		return nil, platform.Invalid("revision", "改內文要帶版本：先 evergenie notes show "+TopicName(r)+" 拿版本，再 notes set … --revision <版本>")
	}
	if req.Revision != "" && req.Revision != r.Revision {
		return nil, platform.Conflict("主題「" + TopicName(r) + "」在你讀過之後被別人改過了：重新 notes show 拿新版本，把對方的行留著再 set")
	}
	if !req.KeepBody {
		body := strings.ReplaceAll(req.Body, "\r\n", "\n")
		if strings.TrimSpace(body) == "" {
			return nil, platform.Invalid("body", "新的內文是空的，沒有寫入")
		}
		if t := strings.TrimLeft(body, " \t\n"); strings.HasPrefix(t, "---\n") {
			return nil, platform.Invalid("body", "只放內文，不要放檔頭（檔頭用 --summary、--aliases、--follow、--links 改）")
		}
		if who != ownerMark {
			if bad := newUnmarkedLines(r.Body, body); len(bad) > 0 {
				nums := []string{}
				for _, n := range bad {
					nums = append(nums, fmt.Sprint(n))
				}
				return nil, platform.Invalid("body", "第 "+strings.Join(nums, "、")+" 行沒有標是誰、哪天寫的：每一行尾巴要有「（<職位> MM-DD）」；整理時保留原本的標記，自己新寫的行補上自己的")
			}
			if bad := foreignMarkedLines(r.Body, body, who); len(bad) > 0 {
				nums := []string{}
				for _, n := range bad {
					nums = append(nums, fmt.Sprint(n))
				}
				return nil, platform.Invalid("body", "第 "+strings.Join(nums, "、")+" 行是新寫的，標的卻不是你（"+who+"）：別人的行原樣保留，自己新寫或改寫的行標自己")
			}
		}
		r.Body = strings.TrimRight(body, "\n") + "\n"
	}
	if s := strings.TrimSpace(req.Summary); s != "" {
		r.Fields["summary"] = s
	}
	if req.Aliases != nil {
		r.Fields["aliases"] = cleanList(req.Aliases)
	}
	if req.Links != nil {
		r.Fields["links"] = cleanLinks(req.Links)
	}
	if req.Follow != nil {
		follow, err := m.checkFollow(ctx, req.Follow)
		if err != nil {
			return nil, err
		}
		r.Fields["follow"] = follow
	}
	for _, f := range req.Clear {
		switch f {
		case "aliases", "follow", "links":
			r.Fields[f] = []string{}
		default:
			return nil, platform.Invalid("clear", "只能清空 aliases、follow、links")
		}
	}
	out, err := m.d.Vault.Put(ctx, r, by, r.Revision)
	if err != nil {
		return nil, err
	}
	if err := m.rebuildIndexesLocked(ctx); err != nil {
		m.warn("主題筆記的索引重產失敗", err)
	}
	return &sdk.NotesSetResponse{Topic: &out}, nil
}

// normalizedLine 去掉行尾標記再比：同一件事被別人用 notes add 併過去，標記會換成併的人與日期，內容才是要比的。
func normalizedLine(line string) string {
	return strings.TrimSpace(markPattern.ReplaceAllString(strings.TrimSpace(line), ""))
}

// unmergedLines 回 from 內文裡 into 內文沒有的行（照原順序，附行號；標記不比）。
func unmergedLines(from, into string) []string {
	have := map[string]bool{}
	for _, l := range strings.Split(into, "\n") {
		if contentLine(l) {
			have[normalizedLine(l)] = true
		}
	}
	var out []string
	for i, l := range strings.Split(from, "\n") {
		if contentLine(l) && !have[normalizedLine(l)] {
			out = append(out, fmt.Sprintf("  第 %d 行：%s", i+1, strings.TrimSpace(l)))
		}
	}
	return out
}

// ArchiveNote 封存一份主題（搬進 archive/，不刪除）：整理重複的主題用。「主人」主題與分類說明檔不能封存。
// 帶 Into＝併進另一份：目標要存在，被封存的主題有目標沒有的行就拒絕並列出（別人的行不能代標、也不自動搬，
// 請先用 notes add 併過去）。寫的人照 noteWriter；封存後重產索引。
func (m *Module) ArchiveNote(ctx context.Context, req *sdk.NotesArchiveRequest, by string) (*sdk.NotesArchiveResponse, error) {
	if _, err := m.noteWriter(ctx, by); err != nil {
		return nil, err
	}
	name, into := strings.TrimSpace(req.Name), strings.TrimSpace(req.Into)
	if name == "" {
		return nil, platform.Invalid("name", "要封存哪一份主題？給名字（例：notes archive <重複的> --into <留的>）")
	}
	m.notesMu.Lock()
	defer m.notesMu.Unlock()
	r, found, err := m.findTopic(ctx, name)
	if err != nil {
		return nil, err
	}
	if !found {
		if _, cerr := m.d.Vault.Get(ctx, CategoryKind, name); cerr == nil {
			return nil, platform.Invalid("name", "「"+name+"」是分類的說明檔，不能封存")
		}
		return nil, platform.NotFound("沒有主題「" + name + "」；用 evergenie notes list 看有哪些")
	}
	if r.ID == OwnerTopicID {
		return nil, platform.Invalid("name", "「"+OwnerTopicName+"」是固定主題，不能封存")
	}
	if isQuipBook(r) {
		return nil, platform.Invalid("name", "「"+TopicName(r)+"」是梗本（固定主題），不能封存；要整理就用 evergenie notes set")
	}
	var target *vault.Record
	if into != "" {
		t, ok, err := m.findTopic(ctx, into)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, platform.NotFound("目標主題「" + into + "」不存在；先 notes add 建，或確認名字")
		}
		if t.ID == r.ID {
			return nil, platform.Invalid("into", "不能封存進自己")
		}
		if miss := unmergedLines(r.Body, t.Body); len(miss) > 0 {
			return nil, platform.Invalid("into", "「"+TopicName(r)+"」有 "+fmt.Sprint(len(miss))+" 行「"+TopicName(t)+"」還沒有，沒有封存：\n"+strings.Join(miss, "\n")+
				"\n請先用 notes add "+TopicName(t)+" <一行> 一行一行併過去（核心會標你和日期，不能代標別人），再封存一次")
		}
		target = &t
	}
	if err := m.d.Vault.ArchiveNow(ctx, TopicKind, r.ID, by); err != nil {
		return nil, err
	}
	if err := m.rebuildIndexesLocked(ctx); err != nil {
		m.warn("主題筆記的索引重產失敗", err)
	}
	path := m.archivedTopicPath(r)
	out := &sdk.NotesArchiveResponse{Name: TopicName(r), Path: path}
	msg := "已封存主題「" + TopicName(r) + "」"
	if target != nil {
		out.Into = TopicName(*target)
		msg += "（併進「" + out.Into + "」）"
	}
	msg += "：list、show、find 和索引都不會再看到它。檔案沒有刪，現在在 " + path +
		"；要找回就把檔案搬回 vault/notes/" + r.ID + ".md（請主人處理，現在沒有還原指令）。"
	out.Message = msg
	return out, nil
}

// archivedTopicPath 回剛封存的主題檔現在在哪（相對於家）：archive/<YYYY-MM>/，同月封存過同名的放 <YYYY-MM>-2…（vault 的做法），取最後一個。
func (m *Module) archivedTopicPath(r vault.Record) string {
	cat, name, _ := strings.Cut(r.ID, "/")
	month := m.d.Clock.Now().In(m.loc).Format("2006-01")
	last := ""
	for n := 1; n < 100; n++ {
		dir := month
		if n > 1 {
			dir = fmt.Sprintf("%s-%d", month, n)
		}
		rel := "vault/notes/" + cat + "/archive/" + dir + "/" + name + ".md"
		if m.home == "" {
			return rel
		}
		if _, err := os.Lstat(filepath.Join(m.home, filepath.FromSlash(rel))); err != nil {
			break
		}
		last = rel
	}
	if last == "" {
		return "vault/notes/" + cat + "/archive/" + month + "/" + name + ".md"
	}
	return last
}

// AddNoteCategory 加一個分類（資料夾＋說明檔 <分類>/<分類>.md）。秘書加之前要先問主人一次（共用規則）。
func (m *Module) AddNoteCategory(ctx context.Context, req *sdk.NotesAddCategoryRequest, by string) (*sdk.NotesAddCategoryResponse, error) {
	if _, err := m.noteWriter(ctx, by); err != nil {
		return nil, err
	}
	name, summary := strings.TrimSpace(req.Name), strings.TrimSpace(req.Summary)
	if err := checkNoteName("name", name); err != nil {
		return nil, err
	}
	if summary == "" {
		return nil, platform.Invalid("summary", "新分類要給一句說明（--summary）：這個分類放什麼")
	}
	m.notesMu.Lock()
	defer m.notesMu.Unlock()
	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = "放什麼：" + summary
	}
	out, err := m.putCategory(ctx, name, summary, body, by)
	if err != nil {
		return nil, err
	}
	if err := m.rebuildIndexesLocked(ctx); err != nil {
		m.warn("主題筆記的索引重產失敗", err)
	}
	return &sdk.NotesAddCategoryResponse{Category: &out}, nil
}

func (m *Module) putCategory(ctx context.Context, name, summary, body, by string) (vault.Record, error) {
	if _, err := m.d.Vault.Get(ctx, CategoryKind, name); err == nil {
		return vault.Record{}, platform.Conflict("已經有分類「" + name + "」")
	} else if platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		return vault.Record{}, err
	}
	if m.ownerFileAt("vault/notes/" + name + "/" + name + ".md") {
		return vault.Record{}, platform.Conflict("vault/notes/" + name + "/" + name + ".md 是主人自己的檔，不蓋它；換個分類名字")
	}
	return m.d.Vault.Put(ctx, vault.Record{Kind: CategoryKind, ID: name, Fields: map[string]any{"kind": "category", "summary": summary}, Body: body + "\n"}, by, "")
}

// ensureDefaultCategories 補上缺的官方預設分類（核心啟動時）：只建缺的，已經有的（主人改過的）一個字都不動。
func (m *Module) ensureDefaultCategories(ctx context.Context) error {
	m.notesMu.Lock()
	defer m.notesMu.Unlock()
	for _, c := range DefaultCategories {
		_, err := m.d.Vault.Get(ctx, CategoryKind, c.Name)
		if err == nil {
			continue
		}
		if platform.CodeOf(err) != sdk.ErrorCodeNotFound {
			// 主人改壞的說明檔（vault 標成壞檔）不擋啟動，也不蓋掉它；doctor 會報。
			m.warn("預設分類「"+c.Name+"」的說明檔讀不了，沒有補", err)
			continue
		}
		if _, err := m.putCategory(ctx, c.Name, c.Summary, c.Body, "system"); err != nil && platform.CodeOf(err) != sdk.ErrorCodeConflict {
			return err
		}
	}
	return nil
}

// ---- 索引 ----

// indexHeader 是兩層索引的檔頭：標明核心產生、別手改（沒有時間欄位：同樣的筆記產生的位元組一樣，VFS-3）。
const indexHeader = "---\nschema: 1\ngenerated_by: evergenie\nnote: 核心照主題筆記的檔頭產生，別手改（改了會被下一次產生蓋掉）；要改請改主題筆記的檔頭\n---\n"

// indexLine 是索引裡的一行：[[名字]]、別名、一句說明、跟進的職位、分類。
func indexLine(r vault.Record) string {
	s := "- [[" + TopicName(r) + "]]"
	if a := fieldList(r, "aliases"); len(a) > 0 {
		s += "（別名：" + strings.Join(a, "、") + "）"
	}
	s += " — " + fieldString(r, "summary")
	follow := "全部職位"
	if !followedByAll(r) {
		f := fieldList(r, "follow")
		sort.Strings(f)
		follow = strings.Join(f, "、")
		if follow == "" {
			follow = "（沒有）"
		}
	}
	return s + " · 跟進：" + follow + " · 分類：" + TopicCategory(r)
}

// RenderNoteIndex 產生索引的內文（不含檔頭）：agent 空字串是總索引（全部主題），否則是這個職位跟進的主題。
// 照分類、名字排序，同樣的資料產生的位元組一樣（決定性，VFS-3）。
func RenderNoteIndex(categories, topics []vault.Record, agent string) string {
	var b strings.Builder
	if agent == "" {
		b.WriteString("# 總索引\n\n全家的主題筆記，一個主題一行：[[名字]]（別名）— 一句說明 · 跟進的職位 · 分類。要看內容用 `evergenie notes show <名字>`。\n")
	} else {
		b.WriteString("# " + agent + " 的職位索引\n\n" + agent + " 跟進的主題，一個主題一行。要看內容用 `evergenie notes show <名字>`；別人的主題看總索引 `vault/notes/" + vault.IndexFileName + "`。\n")
	}
	summary := map[string]string{}
	names := []string{}
	for _, c := range categories {
		summary[c.ID] = fieldString(c, "summary")
		names = append(names, c.ID)
	}
	byCat := map[string][]vault.Record{}
	for _, t := range topics {
		if agent != "" && !follows(t, agent) {
			continue
		}
		cat := TopicCategory(t)
		if _, ok := summary[cat]; !ok {
			summary[cat] = ""
			names = append(names, cat)
		}
		byCat[cat] = append(byCat[cat], t)
	}
	sort.Strings(names)
	empty := true
	for _, cat := range names {
		rows := byCat[cat]
		if agent != "" && len(rows) == 0 {
			continue
		}
		b.WriteString("\n## " + cat)
		if s := summary[cat]; s != "" {
			b.WriteString("（" + s + "）")
		}
		b.WriteString("\n\n")
		sort.Slice(rows, func(i, j int) bool {
			if TopicName(rows[i]) != TopicName(rows[j]) {
				return TopicName(rows[i]) < TopicName(rows[j])
			}
			return rows[i].ID < rows[j].ID
		})
		for _, r := range rows {
			b.WriteString(indexLine(r) + "\n")
			empty = false
		}
		if len(rows) == 0 {
			b.WriteString("（還沒有主題）\n")
		}
	}
	if empty && agent != "" {
		b.WriteString("\n（還沒有跟進的主題）\n")
	}
	return b.String()
}

// NoteIndex 回這個職位的職位索引內文（開場注入用）；agent 空字串是總索引。
func (m *Module) NoteIndex(ctx context.Context, agent string) (string, error) {
	cats, err := m.categories(ctx)
	if err != nil {
		return "", err
	}
	topics, err := m.topics(ctx)
	if err != nil {
		return "", err
	}
	return RenderNoteIndex(cats, topics, agent), nil
}

// indexFiles 回所有該有的索引檔（相對家的路徑 → 內容）：總索引，加上名冊上每一位的職位索引。
func (m *Module) indexFiles(ctx context.Context) (map[string][]byte, error) {
	cats, err := m.categories(ctx)
	if err != nil {
		return nil, err
	}
	topics, err := m.topics(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{"vault/notes/" + vault.IndexFileName: []byte(indexHeader + RenderNoteIndex(cats, topics, ""))}
	for _, a := range rows {
		if a.Archived {
			continue // 封存的職位不產生索引
		}
		out["vault/agents/"+a.ID+"/"+vault.IndexFileName] = []byte(indexHeader + RenderNoteIndex(cats, topics, a.ID))
	}
	return out, nil
}

// RebuildNoteIndexes 重產兩層索引（主題筆記、分類或名冊變了就叫；內容沒變的檔不寫）。
func (m *Module) RebuildNoteIndexes(ctx context.Context) error {
	m.notesMu.Lock()
	defer m.notesMu.Unlock()
	return m.rebuildIndexesLocked(ctx)
}

func (m *Module) rebuildIndexesLocked(ctx context.Context) error {
	if m.home == "" {
		return nil // 還沒接上家（測試或組裝前），不寫到目前的資料夾
	}
	files, err := m.indexFiles(ctx)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		path := filepath.Join(m.home, filepath.FromSlash(rel))
		if old, err := os.ReadFile(path); err == nil && (bytes.Equal(old, files[rel]) || !generatedByCore(old)) {
			continue // 沒變；或是主人自己同名的檔（不是核心產生的），不蓋（VLT-18），doctor 會報索引不符
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := platform.AtomicWrite(path, files[rel], 0600); err != nil {
			return err
		}
	}
	return nil
}

// ownerFileAt：rel（相對於家）已經有檔、但 vault 沒把它當紀錄（主人自己的筆記）。新建主題或分類不能蓋掉它。
func (m *Module) ownerFileAt(rel string) bool {
	if m.home == "" {
		return false
	}
	_, err := os.Lstat(filepath.Join(m.home, filepath.FromSlash(rel)))
	return err == nil
}

// RenameNoteFollower 是職位改名（agents mv）的收尾：主題筆記 follow 裡的舊 id 換成新的（寫的人記 by）、
// 舊職位資料夾裡核心產生的 索引.md 刪掉、新位置重產。
func (m *Module) RenameNoteFollower(ctx context.Context, old, next, by string) error {
	m.notesMu.Lock()
	defer m.notesMu.Unlock()
	topics, err := m.topics(ctx)
	if err != nil {
		return err
	}
	for _, r := range topics {
		follow := fieldList(r, "follow")
		i := slices.Index(follow, old)
		if i < 0 {
			continue
		}
		follow[i] = next
		follow = slices.Compact(follow)
		r.Fields["follow"] = follow
		if _, err := m.d.Vault.Put(ctx, r, by, r.Revision); err != nil {
			return err
		}
	}
	if m.home != "" {
		removeGeneratedIndex(filepath.Join(m.home, "vault", "agents", old, vault.IndexFileName))
	}
	return m.rebuildIndexesLocked(ctx)
}

// removeGeneratedIndex 刪掉 path，只在它是核心產生的索引時；資料夾因此空了也一起收掉。
func removeGeneratedIndex(path string) {
	data, err := os.ReadFile(path)
	if err != nil || !generatedByCore(data) {
		return
	}
	if os.Remove(path) == nil {
		_ = os.Remove(filepath.Dir(path)) // 只有空的才刪得掉
	}
}

// RemoveGeneratedNoteIndexes 是家的遷移 OwnerTopicMigration 的退回收尾（HomeMigration.Undo）：刪掉核心產生的
// 總索引與每個職位的 索引.md（只刪檔頭有 generated_by: evergenie 的），舊版不認得這些檔、會當成壞檔。新版再啟動會重產。
func RemoveGeneratedNoteIndexes(home string) error {
	removeGeneratedIndex(filepath.Join(home, "vault", "notes", vault.IndexFileName))
	dirs, err := os.ReadDir(filepath.Join(home, "vault", "agents"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, d := range dirs {
		if d.IsDir() {
			p := filepath.Join(home, "vault", "agents", d.Name(), vault.IndexFileName)
			data, err := os.ReadFile(p)
			if err == nil && generatedByCore(data) {
				if err := os.Remove(p); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// generatedByCore：檔頭有 generated_by: evergenie（核心產生的索引）才能重寫。
func generatedByCore(data []byte) bool {
	head, _, _ := strings.Cut(string(data), "\n---\n")
	return strings.HasPrefix(string(data), "---\n") && strings.Contains(head, "\ngenerated_by: evergenie")
}

// OwnerTopic 回「主人」主題的內文（開場注入用）；還沒有回空字串。
func (m *Module) OwnerTopic(ctx context.Context) (string, error) {
	r, err := m.d.Vault.Get(ctx, TopicKind, OwnerTopicID)
	if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return r.Body, nil
}

// ---- 健檢 ----

// NoteProblems 是 doctor 的黃字（報告、不擋）：筆記檔頭不合或放錯位置、索引跟筆記對不上、記憶核心超過上限。只讀。
func (m *Module) NoteProblems(ctx context.Context) []string {
	out := append(m.noteFileProblems(), m.ownNoteProblems()...)
	if files, err := m.indexFiles(ctx); err != nil {
		out = append(out, "讀不到主題筆記，沒辦法比對索引")
	} else {
		paths := make([]string, 0, len(files))
		for p := range files {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, rel := range paths {
			if old, err := os.ReadFile(filepath.Join(m.home, filepath.FromSlash(rel))); err != nil || !bytes.Equal(old, files[rel]) {
				out = append(out, "索引 "+rel+" 跟主題筆記對不上（核心下次改到筆記時會重產；也可以請秘書跑一次夜間整理）")
			}
		}
	}
	rows, err := m.List(ctx)
	if err != nil {
		return out
	}
	for _, a := range rows {
		r, err := m.d.Vault.Get(ctx, "memory", a.ID)
		if err != nil {
			continue
		}
		limit := MemoryCoreLines
		if n, ok := r.Fields["max_lines"].(int); ok && n > 0 && n != legacyMemoryLines {
			limit = n
		}
		if n := countLines(r.Body); n > limit {
			out = append(out, fmt.Sprintf("秘書 %s 的記憶核心有 %d 行，超過上限 %d 行：夜間整理時把舊的、跟某個人或東西有關的搬進主題筆記", a.ID, n, limit))
		}
	}
	return out
}

func countLines(body string) int {
	n := 0
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// noteFileProblems 掃 vault/notes/ 的檔案：檔頭讀不懂或欄位不合、放錯位置、名字跟檔名不一樣、分類沒有說明檔。
// 隱藏的、備份（.bak-）、核心產生的索引不看（VLT-18）。
func (m *Module) noteFileProblems() []string {
	if m.home == "" {
		return nil
	}
	root := filepath.Join(m.home, "vault", "notes")
	specs := map[string]vault.Spec{}
	for _, sc := range noteSpecs() {
		specs[sc.Kind] = sc
	}
	var out []string
	dirs := map[string]bool{}
	described := map[string]bool{}
	hasTopic := map[string]bool{} // 有主題筆記的分類資料夾（只放主人自己筆記的資料夾不報缺說明檔）
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		base := d.Name()
		if rel != "." && (strings.HasPrefix(base, ".") || strings.Contains(base, ".bak-")) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		parts := strings.Split(rel, "/")
		if d.IsDir() {
			if len(parts) == 1 && rel != "." {
				dirs[parts[0]] = true
			}
			if len(parts) == 2 && parts[1] == "archive" {
				return filepath.SkipDir // 封存的不檢查
			}
			return nil
		}
		if !strings.HasSuffix(base, ".md") || rel == vault.IndexFileName {
			return nil
		}
		show := "vault/notes/" + rel
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		r, _, err := vault.Decode(data)
		if vault.OwnerNoteFile("notes/"+rel, r, err) {
			return nil // 主人自己的筆記（不是主題筆記格式）：不報、不碰（DEV-154 原則）
		}
		if len(parts) != 2 {
			out = append(out, "主題筆記 "+show+" 放錯位置：要放在 vault/notes/<分類>/<主題>.md")
			return nil
		}
		name := strings.TrimSuffix(parts[1], ".md")
		kind := TopicKind
		if name == parts[0] {
			kind = CategoryKind
			described[parts[0]] = true
		} else {
			hasTopic[parts[0]] = true
		}
		if err := specs[kind].Validate(r); err != nil {
			out = append(out, "主題筆記 "+show+" 的檔頭不合規格："+platform.SDKError(err).Message+"（欄位見 evergenie docs show schemas/note.md）")
			return nil
		}
		if kind == TopicKind {
			if n := fieldString(r, "name"); n != name {
				out = append(out, "主題筆記 "+show+" 的 name 是「"+n+"」，跟檔名不一樣（[[連結]] 看檔名）")
			}
		}
		return nil
	})
	names := make([]string, 0, len(dirs))
	for d := range dirs {
		names = append(names, d)
	}
	sort.Strings(names)
	for _, d := range names {
		if !described[d] && hasTopic[d] {
			out = append(out, "分類 vault/notes/"+d+"/ 沒有說明檔 "+d+".md（寫這個分類放什麼、誰常用）")
		}
	}
	return out
}

// ownNoteProblems 列出沒有檔頭的做事筆記（vault/agents/<職位>/notes/*.md，秘書或主人直接寫的）：只是注意，不是要處理——
// 讀得到、搜得到，不擋部署；下次經 evergenie memory note write 寫回時核心才補上檔頭（DEV-161）。
// 不是 .md 的檔、子資料夾、隱藏檔與備份（.bak-）不看（VLT-18）。
func (m *Module) ownNoteProblems() []string {
	if m.home == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(m.home, "vault", "agents", "*", "notes", "*.md"))
	sort.Strings(files)
	var out []string
	for _, f := range files {
		rel, err := filepath.Rel(filepath.Join(m.home, "vault"), f)
		if err != nil || strings.HasPrefix(filepath.Base(f), ".") || strings.Contains(filepath.Base(f), ".bak-") {
			continue
		}
		if data, err := os.ReadFile(f); err == nil && vault.HeaderlessOwnNote(filepath.ToSlash(rel), data) {
			out = append(out, "做事筆記 vault/agents/"+filepath.ToSlash(strings.TrimPrefix(rel, "agents/"))+" 沒有檔頭：讀得到、不擋，不用處理；秘書下次用 evergenie memory note write 改它時核心會補上")
		}
	}
	return out
}

// warn 記一筆不擋流程的錯誤（還沒開 log 時不記）。
func (m *Module) warn(msg string, err error) {
	if m.log != nil {
		m.log.Warn(msg, "error", err)
	}
}
