package charter

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// loaded 是一份文件現在在磁碟上的樣子。
type loaded struct {
	conf   docConf
	path   string // 實際寫入的位置（符號連結會換成它指到的檔）
	file   string // 整個檔案的內容；檔案不在時是空的
	mode   fs.FileMode
	text   string // 這份文件的文字（整個檔，或那一段）
	exists bool
	rev    string
}

func (m *Module) find(id string) (docConf, config, error) {
	cfg := loadConfig(m.home)
	for _, d := range cfg.docs {
		if d.ID == id {
			return d, cfg, nil
		}
	}
	if strings.TrimSpace(id) == "" {
		return docConf{}, cfg, platform.Invalid("doc", "請指定哪一份規矩文件（例：law）")
	}
	return docConf{}, cfg, platform.NotFound("沒有這份規矩文件：" + id)
}

func (m *Module) load(d docConf) (loaded, error) {
	l := loaded{conf: d, path: d.resolve(m.home), mode: 0o644}
	if real, err := filepath.EvalSymlinks(l.path); err == nil {
		l.path = real
	}
	info, err := os.Stat(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil || info.IsDir() {
		return l, platform.Unavailable("讀不到規矩文件 " + d.Path)
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		return l, platform.Unavailable("讀不到規矩文件 " + d.Path)
	}
	l.mode = info.Mode().Perm()
	l.file = string(data)
	l.text, l.exists = docText(l.file, d.Section)
	l.rev = revisionOf(l.text, l.exists)
	return l, nil
}

func (m *Module) describe(ctx context.Context, l loaded) (sdk.RulesDoc, error) {
	pending, err := m.pendingFor(ctx, l.conf.ID, "")
	if err != nil {
		return sdk.RulesDoc{}, err
	}
	return sdk.RulesDoc{ID: l.conf.ID, Title: l.conf.Title, Rank: l.conf.Rank, Path: l.conf.Path, Section: l.conf.Section,
		Exists: l.exists, Revision: l.rev, Pending: len(pending)}, nil
}

// pendingFor 列出待決的提案（doc 或 from 空的就不篩）。
func (m *Module) pendingFor(ctx context.Context, doc, from string) ([]vault.Record, error) {
	where := map[string]string{"status": string(sdk.RulesProposalStatusPending)}
	if doc != "" {
		where["doc"] = doc
	}
	if from != "" {
		where["from"] = from
	}
	return m.d.Vault.List(ctx, proposalKind, vault.Query{Where: where})
}

// List 列出設定的規矩文件（照位階）。
func (m *Module) List(ctx context.Context) (*sdk.RulesListResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	cfg := loadConfig(m.home)
	out := &sdk.RulesListResponse{Problem: cfg.problem}
	for _, d := range cfg.docs {
		l, loadErr := m.load(d)
		doc, err := m.describe(ctx, l)
		if err != nil {
			return nil, err
		}
		if loadErr != nil {
			// 一份讀不到（是資料夾、沒有權限）不擋其他份：那一份標原因（RUL-12 看板照樣開）。
			doc.Problem = platform.SDKError(loadErr).Message
		}
		out.Docs = append(out.Docs, doc)
	}
	return out, nil
}

// Get 讀一份文件。
func (m *Module) Get(ctx context.Context, id string) (*sdk.RulesGetResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	d, _, err := m.find(id)
	if err != nil {
		return nil, err
	}
	l, err := m.load(d)
	if err != nil {
		return nil, err
	}
	doc, err := m.describe(ctx, l)
	if err != nil {
		return nil, err
	}
	return &sdk.RulesGetResponse{Doc: &doc, Text: l.text}, nil
}

func ownerOnly(by string) error {
	if !platform.IsOwner(by) {
		return platform.Forbidden("只有主人能直接改規矩；秘書請用 evergenie rules propose 送修改提案")
	}
	return nil
}

func checkNote(note string) error {
	if utf8.RuneCountInString(note) > MaxNoteRunes {
		return platform.Invalid("note", fmt.Sprintf("備註太長了（上限 %d 字）", MaxNoteRunes))
	}
	return nil
}

// docLevel 是這份文件那一段的標題級數（整個檔＝0；那一段還不在＝2，存檔時會加成 ## 標題）。
func docLevel(l loaded) int {
	if l.conf.Section == "" {
		return 0
	}
	if s := findSection(strings.ReplaceAll(l.file, "\r\n", "\n"), l.conf.Section); s.found {
		return s.level
	}
	return 2
}

func checkSize(field, text string) error {
	if len(text) > MaxTextBytes {
		return platform.Invalid(field, fmt.Sprintf("內容太長了（上限 %d KB）", MaxTextBytes/1024))
	}
	if !utf8.ValidString(text) {
		return platform.Invalid(field, "內容不是有效的文字（UTF-8）")
	}
	return nil
}

// Save 主人改一份文件：revision 要是讀到的那一版。
func (m *Module) Save(ctx context.Context, req *sdk.RulesSaveRequest, by string) (*sdk.RulesSaveResponse, error) {
	if err := ownerOnly(by); err != nil {
		return nil, err
	}
	if err := checkSize("text", req.Text); err != nil {
		return nil, err
	}
	if err := checkNote(strings.TrimSpace(req.Note)); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	d, _, err := m.find(req.Doc)
	if err != nil {
		return nil, err
	}
	l, err := m.load(d)
	if err != nil {
		return nil, err
	}
	if req.Revision != l.rev {
		return nil, platform.Conflict("你打開之後這份文件改過了（可能是別的分頁、秘書的提案被採用，或在電腦上直接改）；請重新整理再改")
	}
	doc, v, err := m.apply(ctx, l, req.Text, change{source: sdk.RulesVersionSourceSave, note: strings.TrimSpace(req.Note), by: by})
	if err != nil {
		return nil, err
	}
	return &sdk.RulesSaveResponse{Doc: doc, Version: v}, nil
}

// change 是一次改動要記進版本的資訊。
type change struct {
	source       sdk.RulesVersionSource
	note, by     string
	proposal     string
	restoredFrom string
}

// apply 把文件換成 text（呼叫端持有 m.mu、已經檢查過權限與版本）：
// ① 內容沒變就什麼都不做 ② 現在的內容還沒留過版本（第一次經看板改，或在看板外改過）就先留一版「改之前的樣子」
// ③ 留新的版本 ④ 寫檔。先留版本再寫檔：寫檔失敗時多一個沒寫進去的版本，下次改會再補一版現在的樣子，不會掉內容。
func (m *Module) apply(ctx context.Context, l loaded, text string, c change) (*sdk.RulesDoc, *sdk.RulesVersion, error) {
	clean := cleanText(l.conf.Section, text)
	if msg := checkBody(clean, docLevel(l)); msg != "" {
		return nil, nil, platform.Invalid("text", msg)
	}
	if l.exists && clean == l.text {
		doc, err := m.describe(ctx, l)
		return &doc, nil, err
	}
	if l.exists {
		latest, err := m.latestVersion(ctx, l.conf.ID)
		if err != nil {
			return nil, nil, err
		}
		if latest == nil || latest.Body != l.text {
			if _, err := m.putVersion(ctx, l, l.text, change{source: sdk.RulesVersionSourceOutside, note: "改之前的樣子", by: "system"}); err != nil {
				return nil, nil, err
			}
		}
	}
	rec, err := m.putVersion(ctx, l, clean, c)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return nil, nil, platform.Unavailable("建不了規矩文件的資料夾")
	}
	if err := platform.AtomicWrite(l.path, []byte(withDoc(l.file, l.conf.Section, clean)), l.mode); err != nil {
		return nil, nil, platform.Unavailable("寫不進規矩文件 " + l.conf.Path)
	}
	after, err := m.load(l.conf)
	if err != nil {
		return nil, nil, err
	}
	doc, err := m.describe(ctx, after)
	if err != nil {
		return nil, nil, err
	}
	v := decodeVersion(rec, false)
	return &doc, &v, nil
}

func (m *Module) latestVersion(ctx context.Context, doc string) (*vault.Record, error) {
	rs, err := m.d.Vault.List(ctx, versionKind, vault.Query{Where: map[string]string{"doc": doc}})
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	newestFirst(rs)
	return &rs[0], nil
}

func (m *Module) putVersion(ctx context.Context, l loaded, text string, c change) (vault.Record, error) {
	id, err := m.d.Vault.NextID(ctx, versionKind)
	if err != nil {
		return vault.Record{}, err
	}
	f := map[string]any{"kind": versionKind, "doc": l.conf.ID, "title": l.conf.Title, "source": string(c.source),
		"created_at": m.now().Format(time.RFC3339Nano)}
	for k, v := range map[string]string{"note": c.note, "proposal": c.proposal, "restored_from": c.restoredFrom} {
		if v != "" {
			f[k] = v
		}
	}
	return m.d.Vault.Put(ctx, vault.Record{Kind: versionKind, ID: id, Fields: f, Body: text}, c.by, "")
}

// History 列出一份文件的版本（新的在前、不含全文）。文件不在設定裡了也照樣列得出以前的版本。
func (m *Module) History(ctx context.Context, doc string) ([]sdk.RulesVersion, error) {
	if !docIDPattern.MatchString(doc) {
		return nil, platform.Invalid("doc", "請指定哪一份規矩文件（例：law）")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	rs, err := m.d.Vault.List(ctx, versionKind, vault.Query{Where: map[string]string{"doc": doc}})
	if err != nil {
		return nil, err
	}
	newestFirst(rs)
	out := make([]sdk.RulesVersion, 0, len(rs))
	for _, r := range rs {
		out = append(out, decodeVersion(r, false))
	}
	return out, nil
}

func (m *Module) getVersion(ctx context.Context, id string) (vault.Record, error) {
	if strings.TrimSpace(id) == "" {
		return vault.Record{}, platform.Invalid("id", "請指定版本（例：RV-3）")
	}
	r, err := m.d.Vault.Get(ctx, versionKind, id)
	if err != nil && platform.CodeOf(err) == sdk.ErrorCodeNotFound {
		return r, platform.NotFound("找不到這個版本：" + id)
	}
	return r, err
}

// Version 讀一個版本的全文和差異（against → 這一版）。
func (m *Module) Version(ctx context.Context, id, against string) (*sdk.RulesVersionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	r, err := m.getVersion(ctx, id)
	if err != nil {
		return nil, err
	}
	v := decodeVersion(r, true)
	out := &sdk.RulesVersionResponse{Version: &v}
	base, has := "", false
	switch against {
	case "":
		rs, err := m.d.Vault.List(ctx, versionKind, vault.Query{Where: map[string]string{"doc": v.Doc}})
		if err != nil {
			return nil, err
		}
		newestFirst(rs)
		for _, p := range rs {
			if seqOf(p.ID) < seqOf(v.ID) {
				out.Against, base, has = p.ID, p.Body, true
				break
			}
		}
	case "current":
		d, _, err := m.find(v.Doc)
		if err != nil {
			return nil, err
		}
		l, err := m.load(d)
		if err != nil {
			return nil, err
		}
		out.Against, base, has = "current", l.text, true
	default:
		o, err := m.getVersion(ctx, against)
		if err != nil {
			return nil, err
		}
		if field(o, "doc") != v.Doc {
			return nil, platform.Invalid("against", "只能跟同一份文件的版本比")
		}
		out.Against, base, has = o.ID, o.Body, true
	}
	if has {
		out.Diff = lineDiff(base, v.Text)
	} else {
		out.Diff = lineDiff("", v.Text)
	}
	return out, nil
}

// Restore 主人把文件換回某一版。
func (m *Module) Restore(ctx context.Context, req *sdk.RulesRestoreRequest, by string) (*sdk.RulesRestoreResponse, error) {
	if err := ownerOnly(by); err != nil {
		return nil, err
	}
	if err := checkNote(strings.TrimSpace(req.Note)); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	r, err := m.getVersion(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	d, _, err := m.find(field(r, "doc"))
	if err != nil {
		return nil, err
	}
	l, err := m.load(d)
	if err != nil {
		return nil, err
	}
	if req.Revision != "" && req.Revision != l.rev {
		return nil, platform.Conflict("你打開之後這份文件改過了；請重新整理再還原")
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "還原成 " + r.ID
	}
	doc, v, err := m.apply(ctx, l, r.Body, change{source: sdk.RulesVersionSourceRestore, note: note, by: by, restoredFrom: r.ID})
	if err != nil {
		return nil, err
	}
	return &sdk.RulesRestoreResponse{Doc: doc, Version: v}, nil
}

// proposedText 是採用提案之後這份文件的文字：整份換掉，或只換文件裡那一段（標題留著）。
func proposedText(current, section, text string) (string, error) {
	if section == "" {
		return text, nil
	}
	s := findSection(current, section)
	if !s.found {
		return "", platform.Invalid("section", "文件裡沒有這個標題：「"+section+"」（寫標題的文字，不含 #）")
	}
	if msg := checkBody(text, s.level); msg != "" {
		return "", platform.Invalid("text", msg)
	}
	return withDoc(current, section, text), nil
}

// Propose 秘書送修改提案（不改文件）；送一則安靜的通知給主人。
func (m *Module) Propose(ctx context.Context, req *sdk.RulesProposeRequest, by string) (*sdk.RulesProposal, error) {
	if platform.IsOwner(by) {
		return nil, platform.Invalid("by", "主人直接在看板改就好，不用提案")
	}
	why := strings.TrimSpace(req.Why)
	if why == "" {
		return nil, platform.Invalid("why", "請寫為什麼要改（主人看這個決定要不要採用）")
	}
	if utf8.RuneCountInString(why) > MaxWhyRunes {
		return nil, platform.Invalid("why", fmt.Sprintf("理由太長了（上限 %d 字）", MaxWhyRunes))
	}
	if strings.TrimSpace(req.Text) == "" {
		return nil, platform.Invalid("text", "提案內容不能是空的（要拿掉一段，請寫清楚留下的內容）")
	}
	if err := checkSize("text", req.Text); err != nil {
		return nil, err
	}
	section := strings.TrimSpace(req.Section)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	d, _, err := m.find(req.Doc)
	if err != nil {
		return nil, err
	}
	l, err := m.load(d)
	if err != nil {
		return nil, err
	}
	next, err := proposedText(l.text, section, req.Text)
	if err != nil {
		return nil, err
	}
	if msg := checkBody(cleanText(d.Section, next), docLevel(l)); msg != "" {
		return nil, platform.Invalid("text", msg)
	}
	if l.exists && cleanText(d.Section, next) == l.text {
		return nil, platform.Invalid("text", "提案內容跟現在的文件一樣")
	}
	mine, err := m.pendingFor(ctx, "", by)
	if err != nil {
		return nil, err
	}
	if len(mine) >= MaxPendingPerSender {
		return nil, platform.Conflict(fmt.Sprintf("你已經有 %d 個提案在等主人決定，等他看完再送", len(mine)))
	}
	id, err := m.d.Vault.NextID(ctx, proposalKind)
	if err != nil {
		return nil, err
	}
	f := map[string]any{"kind": proposalKind, "doc": d.ID, "from": by, "why": why, "status": string(sdk.RulesProposalStatusPending),
		"base": l.rev, "created_at": m.now().Format(time.RFC3339Nano)}
	if section != "" {
		f["section"] = section
	}
	rec, err := m.d.Vault.Put(ctx, vault.Record{Kind: proposalKind, ID: id, Fields: f, Body: strings.ReplaceAll(req.Text, "\r\n", "\n")}, by, "")
	if err != nil {
		return nil, err
	}
	if m.d.Notify != nil {
		where := "「" + d.Title + "」"
		if section != "" {
			where += "的「" + section + "」"
		}
		n, err := m.d.Notify.Send(ctx, &sdk.Notification{
			Category: sdk.CategoryDigest, Title: "提案修改" + where, Context: why,
			Todo: "到「設定 › 規矩」看提案，採用或退回", URL: "/settings/rules?proposal=" + id, Agent: by, By: by,
		})
		if err != nil {
			m.warn("規矩的修改提案通知送不出去")
		} else if n != nil && n.ID != "" {
			f := cloneFields(rec.Fields)
			f["notice"] = n.ID
			if saved, err := m.d.Vault.Put(ctx, vault.Record{Kind: proposalKind, ID: id, Fields: f, Body: rec.Body}, by, rec.Revision); err == nil {
				rec = saved
			}
		}
	}
	p := decodeProposal(rec)
	return &p, nil
}

func cloneFields(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Proposals 列出提案（新的在前）；待決的附上差異與「文件改過了」。
func (m *Module) Proposals(ctx context.Context, doc string, statuses []sdk.RulesProposalStatus) ([]sdk.RulesProposal, error) {
	for _, s := range statuses {
		if !slices.Contains([]sdk.RulesProposalStatus{sdk.RulesProposalStatusPending, sdk.RulesProposalStatusAccepted, sdk.RulesProposalStatusRejected}, s) {
			return nil, platform.Invalid("statuses", "不認得的狀態："+string(s)+"（pending、accepted、rejected）")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	where := map[string]string{}
	if doc != "" {
		where["doc"] = doc
	}
	rs, err := m.d.Vault.List(ctx, proposalKind, vault.Query{Where: where})
	if err != nil {
		return nil, err
	}
	newestFirst(rs)
	cfg := loadConfig(m.home)
	current := map[string]*loaded{}
	out := []sdk.RulesProposal{}
	for _, r := range rs {
		p := decodeProposal(r)
		if p.Status == sdk.RulesProposalStatusPending {
			if healed, ok := m.healAccepted(ctx, r); ok {
				p = healed
			}
		}
		if len(statuses) > 0 && !slices.Contains(statuses, p.Status) {
			continue
		}
		if p.Status == sdk.RulesProposalStatusPending {
			l, ok := current[p.Doc]
			if !ok {
				for _, d := range cfg.docs {
					if d.ID == p.Doc {
						got, err := m.load(d)
						if err != nil {
							return nil, err
						}
						l = &got
					}
				}
				current[p.Doc] = l
			}
			if l == nil {
				p.Stale = true
			} else {
				p.Stale = l.rev != p.Base
				if next, err := proposedText(l.text, p.Section, p.Text); err == nil {
					p.Diff = lineDiff(l.text, cleanText(l.conf.Section, next))
				} else {
					p.Stale = true
				}
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// Decide 主人採用或退回一個待決的提案。
func (m *Module) Decide(ctx context.Context, req *sdk.RulesDecideRequest, by string) (*sdk.RulesDecideResponse, error) {
	if err := ownerOnly(by); err != nil {
		return nil, err
	}
	if err := checkNote(strings.TrimSpace(req.Note)); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, platform.Invalid("id", "請指定提案（例：RP-3）")
	}
	r, err := m.d.Vault.Get(ctx, proposalKind, req.ID)
	if err != nil {
		if code := platform.CodeOf(err); code == sdk.ErrorCodeNotFound || code == sdk.ErrorCodeInvalid {
			return nil, platform.NotFound("找不到這個提案：" + req.ID)
		}
		return nil, err
	}
	p := decodeProposal(r)
	if p.Status != sdk.RulesProposalStatusPending {
		return nil, platform.Conflict("這個提案已經決定過了")
	}
	note := strings.TrimSpace(req.Note)
	out := &sdk.RulesDecideResponse{}
	title := p.Doc
	f := cloneFields(r.Fields)
	if req.Accept {
		d, _, err := m.find(p.Doc)
		if err != nil {
			return nil, err
		}
		title = d.Title
		l, err := m.load(d)
		if err != nil {
			return nil, err
		}
		if l.rev != p.Base {
			return nil, platform.Conflict("提案之後文件改過了，不能直接採用；請退回並請秘書照新版重送")
		}
		next, err := proposedText(l.text, p.Section, p.Text)
		if err != nil {
			return nil, err
		}
		vnote := "採用 " + p.From + " 的提案 " + p.ID
		if note != "" {
			vnote += "：" + note
		}
		_, v, err := m.apply(ctx, l, next, change{source: sdk.RulesVersionSourceProposal, note: vnote, by: by, proposal: p.ID})
		if err != nil {
			return nil, err
		}
		out.Version = v
		f["status"] = string(sdk.RulesProposalStatusAccepted)
		if v != nil {
			f["version"] = v.ID
		}
	} else {
		if d, _, err := m.find(p.Doc); err == nil {
			title = d.Title
		}
		f["status"] = string(sdk.RulesProposalStatusRejected)
	}
	f["decided_at"] = m.now().Format(time.RFC3339Nano)
	f["decided_by"] = by
	if note != "" {
		f["note"] = note
	}
	saved, err := m.d.Vault.Put(ctx, vault.Record{Kind: proposalKind, ID: p.ID, Fields: f, Body: r.Body}, by, r.Revision)
	if err != nil {
		if out.Version != nil {
			// 文件已經改了、版本也留了，只有提案的狀態沒記上：下次列提案時由 healAccepted 照版本補記，不會變成「過期、只能退回」。
			m.warn("採用了規矩提案，但記不上提案的狀態")
			return nil, platform.Unavailable("文件已經照提案改好（版本 " + out.Version.ID + "），但提案的狀態沒記上；重新整理後會自動補上")
		}
		return nil, err
	}
	done := decodeProposal(saved)
	out.Proposal = &done
	if m.d.Notify != nil && done.Notice != "" {
		if err := m.d.Notify.Retire(ctx, []string{done.Notice}, "主人已經決定了"); err != nil {
			m.warn("規矩提案的通知收不掉")
		}
	}
	m.tellProposer(ctx, done, title)
	return out, nil
}

// tellProposer 把決定送回提案的秘書（不在線上的話等她下次開對話）。
func (m *Module) tellProposer(ctx context.Context, p sdk.RulesProposal, title string) {
	if m.d.Bus == nil || p.From == "" {
		return
	}
	var body string
	if p.Status == sdk.RulesProposalStatusAccepted {
		body = fmt.Sprintf("你提的規矩修改 %s（%s）主人採用了，已經寫進文件", p.ID, title)
		if p.Version != "" {
			body += "（版本 " + p.Version + "）"
		}
		body += "。"
	} else {
		body = fmt.Sprintf("你提的規矩修改 %s（%s）主人退回了，文件沒有改。", p.ID, title)
	}
	if p.Note != "" {
		body += "主人說：" + p.Note
	}
	if _, err := m.d.Bus.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindEcho, Route: sdk.EnvelopeRouteCore, From: "system", To: p.From, Body: body,
		IdempotencyKey: "rules:" + p.ID + ":" + string(p.Status)}); err != nil {
		m.warn("規矩提案的結果送不回提案的秘書")
	}
}

// healAccepted：提案還是待決、但已經有版本記著「採用了這個提案」（採用時寫檔成功、記提案狀態失敗），
// 照那個版本把提案補記成已採用並收掉通知（呼叫端持有 m.mu）。
func (m *Module) healAccepted(ctx context.Context, r vault.Record) (sdk.RulesProposal, bool) {
	vs, err := m.d.Vault.List(ctx, versionKind, vault.Query{Where: map[string]string{"proposal": r.ID}})
	if err != nil || len(vs) == 0 {
		return sdk.RulesProposal{}, false
	}
	v := vs[0]
	f := cloneFields(r.Fields)
	f["status"] = string(sdk.RulesProposalStatusAccepted)
	f["version"] = v.ID
	f["decided_at"] = field(v, "created_at")
	by := field(v, "by")
	f["decided_by"] = by
	saved, err := m.d.Vault.Put(ctx, vault.Record{Kind: proposalKind, ID: r.ID, Fields: f, Body: r.Body}, by, r.Revision)
	if err != nil {
		return sdk.RulesProposal{}, false
	}
	p := decodeProposal(saved)
	if m.d.Notify != nil && p.Notice != "" {
		if err := m.d.Notify.Retire(ctx, []string{p.Notice}, "主人已經決定了"); err != nil {
			m.warn("規矩提案的通知收不掉")
		}
	}
	return p, true
}
