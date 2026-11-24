package bus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
)

var envelopeID = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func (m *Module) path(id string) string { return platform.RuntimeDir(m.home, "queue", id+".md") }
func (m *Module) persist(e Envelope) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return platform.Internal("無法整理訊息內容")
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return platform.Internal("無法整理訊息欄位")
	}
	fields["schema"] = 1
	fields["updated_at"] = e.History[len(e.History)-1].At.Format("2006-01-02T15:04:05.999999999Z07:00")
	fields["body"] = truncate(e.Body, 200)
	data := vault.Encode(vault.Record{Fields: fields, Body: e.Body}, nil, vault.Spec{})
	if err := platform.AtomicWrite(m.path(e.ID), data, 0600); err != nil {
		m.log.Error("persist envelope failed", "id", e.ID, "error", err)
		return platform.Internal("無法儲存訊息，請檢查磁碟")
	}
	return nil
}
func (m *Module) restore() error {
	files, err := filepath.Glob(platform.RuntimeDir(m.home, "queue", "*.md"))
	if err != nil {
		return platform.Internal("無法讀取訊息清單")
	}
	var records []Envelope
	for _, file := range files {
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			return platform.Invalid("queue", "訊息檔案格式不正確")
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return platform.Internal("無法讀取訊息檔案")
		}
		r, _, err := vault.Decode(raw)
		if err != nil {
			return err
		}
		data, err := json.Marshal(r.Fields)
		if err != nil {
			return platform.Invalid("queue", "訊息欄位格式不正確")
		}
		var e Envelope
		if err := json.Unmarshal(data, &e); err != nil || e == nil {
			return platform.Invalid("queue", "訊息欄位格式不正確")
		}
		if !envelopeID.MatchString(e.ID) || filepath.Base(file) != e.ID+".md" || e.To == "" || len(e.History) == 0 {
			return platform.Invalid("queue", "訊息識別資料不完整")
		}
		e.Body = strings.TrimSuffix(r.Body, "\n")
		records = append(records, e)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].CreatedAt.Before(records[j].CreatedAt)
	})
	m.boxes = map[string]*mailbox{}
	m.idem = map[string]string{}
	for _, e := range records {
		b := m.boxes[e.To]
		if b == nil {
			b = &mailbox{requests: make(chan func())}
			m.boxes[e.To] = b
		}
		b.records = append(b.records, e)
		if e.IdempotencyKey != "" {
			m.idem[e.IdempotencyKey] = e.ID
		}
	}
	return nil
}
