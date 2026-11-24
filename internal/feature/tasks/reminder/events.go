package reminder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/feature/tasks/attach"
	"github.com/minglight/evergenie/sdk"
)

func (m *Module) subscribe(ctx context.Context) error {
	run, cancel := context.WithCancel(ctx)
	events, err := m.d.Bus.Subscribe(run, "schedule.fire", "notice.answered", "notice.sent")
	if err != nil {
		cancel()
		return err
	}
	m.cancel = cancel
	m.done = make(chan struct{})
	done := m.done
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				m.log.Warn("reminders 事件處理中斷")
			}
		}()
		for {
			select {
			case <-run.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if ev == nil {
					continue
				}
				if err := m.handle(run, ev); err != nil && run.Err() == nil {
					m.log.Warn("reminders 事件處理失敗", "error", err)
				}
			}
		}
	}()
	return nil
}

func (m *Module) startEvents(ctx context.Context) error {
	cfg := struct {
		Reask struct {
			After int `toml:"after_minutes"`
			Max   int `toml:"max_asks"`
		} `toml:"reask"`
	}{}
	cfg.Reask.After = 30
	cfg.Reask.Max = 3
	data, err := os.ReadFile(filepath.Join(m.home, "settings/notify.toml"))
	if err != nil && !os.IsNotExist(err) {
		return platform.Unavailable("無法讀取追問設定")
	}
	if err == nil {
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return platform.Invalid("reask", "追問設定格式不正確")
		}
	}
	if cfg.Reask.After <= 0 || cfg.Reask.Max <= 0 {
		return platform.Invalid("reask", "追問間隔與次數需大於零")
	}
	m.reaskMinutes = cfg.Reask.After
	m.maxAsks = cfg.Reask.Max
	m.checks = map[string]time.Time{}
	m.noticed = map[string]time.Time{}
	rows, err := m.d.Vault.List(ctx, "reminder", vault.Query{})
	if err != nil {
		return err
	}
	if err := m.subscribe(ctx); err != nil {
		return err
	}
	started := false
	defer func() {
		if !started {
			m.cancel()
			<-m.done
			m.cancel = nil
		}
	}()
	for _, r := range rows {
		m.markRoomWait(r.ID, text(r, "room"), text(r, "status"))
		if ended(text(r, "status")) || text(r, "status") == "shelved" || text(r, "follow") == "none" || stamp(r, "at").IsZero() {
			continue
		}
		if err := m.d.Scheduler.At(ctx, r.ID, stamp(r, "at")); err != nil {
			return err
		}
		if text(r, "status") == "asking" && !stamp(r, "sent_at").IsZero() {
			if err := m.d.Scheduler.At(ctx, r.ID+"#noreply", stamp(r, "sent_at").Add(time.Duration(m.reaskMinutes)*time.Minute)); err != nil {
				return err
			}
		}
	}
	started = true
	return nil
}
func (m *Module) handle(ctx context.Context, ev *sdk.BusSubscribeResponse) error {
	p := ev.Payload
	if ev.Topic == "notice.answered" {
		id, _ := p["reminder"].(string)
		if id == "" {
			return nil
		}
		by, _ := p["by"].(string)
		if by == "" {
			by = "owner(web)"
		}
		var err error
		switch p["text"] {
		case AnswerDone:
			_, err = m.Done(ctx, id, by)
		case AnswerLater:
			_, err = m.Snooze(ctx, id, SnoozeLater, by)
		case AnswerNotToday:
			_, err = m.Snooze(ctx, id, SnoozeNotToday, by)
		// 下面三個是改版前送出、還掛在畫面上的舊通知（2026-11-07 前）。
		case "延到明天同一時間":
			_, err = m.Snooze(ctx, id, "tomorrow-same", by)
		case "3 小時後":
			_, err = m.Snooze(ctx, id, "3h", by)
		case "取消":
			_, err = m.Cancel(ctx, id, by)
		}
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.d.Clock.Now().In(m.config.Location())
	if ev.Topic == "notice.sent" {
		id, _ := p["reminder"].(string)
		if start, ok := m.checks[id]; ok && !now.After(start.Add(10*time.Minute)) {
			m.noticed[id] = now
			r, err := m.d.Vault.Get(ctx, "reminder", id)
			if err != nil {
				return err
			}
			if ended(text(r, "status")) || text(r, "status") == "shelved" || text(r, "status") == "asking" {
				return nil
			}
			r.Fields["status"] = "asking"
			r.Fields["sent_at"] = now.Format(time.RFC3339Nano)
			if _, err := m.save(ctx, r, text(r, "at"), text(r, "follow"), "system", "sent"); err != nil {
				return err
			}
			return m.d.Scheduler.At(ctx, id+"#noreply", now.Add(time.Duration(m.reaskMinutes)*time.Minute))
		}
		return nil
	}
	if ev.Topic != "schedule.fire" || p["kind"] != "at" {
		return nil
	}
	id, _ := p["id"].(string)
	parts := strings.SplitN(id, "#", 2)
	id = parts[0]
	if !strings.HasPrefix(id, "R-") {
		return nil
	}
	r, err := m.d.Vault.Get(ctx, "reminder", id)
	if err != nil {
		return err
	}
	if ended(text(r, "status")) || text(r, "status") == "shelved" || text(r, "follow") == "none" {
		return nil
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "noreply":
			if text(r, "status") != "asking" || now.Before(stamp(r, "sent_at").Add(time.Duration(m.reaskMinutes)*time.Minute)) {
				return nil
			}
			current, err := decode(r)
			if err != nil {
				return err
			}
			// 沒回不念、也不自動擱置（RMD-8）：當天不再推；最多再問到 max_asks 次，之後留在逾期清單不推，等主人碰它。
			asks := int(current.Asks) + 1
			r.Fields["asks"] = asks
			r.Fields["status"] = "todo"
			if asks >= m.maxAsks {
				_, err = m.save(ctx, r, text(r, "at"), text(r, "follow"), "system", "reask")
				return err
			}
			keepDue(r)
			at, err := m.notToday(ctx, r, stamp(r, "at").In(m.config.Location()), now)
			if err != nil {
				return err
			}
			r.Fields["at"] = at.Format(time.RFC3339Nano)
			_, err = m.save(ctx, r, text(r, "at"), text(r, "follow"), "system", "reask")
			if err != nil {
				return err
			}
			return m.d.Scheduler.At(ctx, id, stamp(r, "at"))
		case "check":
			start, ok := m.checks[id]
			if !ok || now.Before(start.Add(10*time.Minute)) {
				return nil
			}
			delete(m.checks, id)
			noticed := m.noticed[id]
			delete(m.noticed, id)
			if !noticed.IsZero() || text(r, "status") == "asking" {
				return nil
			}
			return m.sendDue(ctx, r, false)
		}
		return nil
	}
	if text(r, "status") == "asking" || now.Before(stamp(r, "at")) || m.gaveUp(r) {
		return nil
	}
	if text(r, "follow") == "check" {
		if _, ok := m.checks[id]; ok {
			return nil
		}
		m.checks[id] = now
		delete(m.noticed, id)
		if err := m.askCheck(ctx, r); err != nil {
			delete(m.checks, id)
			return err
		}
		return m.d.Scheduler.At(ctx, id+"#check", now.Add(10*time.Minute))
	}
	return m.sendDue(ctx, r, text(r, "follow") == "discuss")
}

// 提醒的一鍵回覆（RMD-5）。
const (
	AnswerDone     = "做了"
	AnswerLater    = "晚點"
	AnswerNotToday = "今天不做"
)

// gaveUp：已經連續沒回到上限，不再推，留在逾期清單（RMD-8）。
func (m *Module) gaveUp(r vault.Record) bool {
	current, err := decode(r)
	return err == nil && int(current.Asks) >= m.maxAsks
}

func (m *Module) sendDue(ctx context.Context, r vault.Record, discuss bool) error {
	n := &sdk.Notification{Category: sdk.CategoryDue, Title: text(r, "content"), Options: []string{AnswerDone, AnswerLater, AnswerNotToday}, Reminder: r.ID, Agent: text(r, "owner"), Room: text(r, "room")}
	if text(r, "follow") == "confirm" {
		n.Title += "，做了嗎？"
	}
	if discuss {
		n.Category = sdk.CategoryOpening
		n.URL = "/chat/" + text(r, "owner") + "-r-" + strings.TrimPrefix(r.ID, "R-")
	}
	if _, err := m.d.Notify.Send(ctx, n); err != nil {
		return err
	}
	now := m.d.Clock.Now().In(m.config.Location())
	r.Fields["status"] = "asking"
	r.Fields["sent_at"] = now.Format(time.RFC3339Nano)
	if _, err := m.save(ctx, r, text(r, "at"), text(r, "follow"), "system", "sent"); err != nil {
		return err
	}
	return m.d.Scheduler.At(ctx, r.ID+"#noreply", now.Add(time.Duration(m.reaskMinutes)*time.Minute))
}

// askCheck：要查證的待辦（follow: check）到時間，開專屬對話（room，沒有就用「<owner>-r-<編號>」）請秘書去查（RMD-6）：
// 經 Deps.Sessions 開對話、登記這個對話在等待辦，再送一則 routine「［待辦 R-n］<查證內容>」；
// 同一次到期只送一則（冪等鍵 R-n#check#<到期時間>）。2026-11-01 從 session 搬來（以前發 reminder.check_due 給 session）。
func (m *Module) askCheck(ctx context.Context, r vault.Record) error {
	if m.d.Sessions == nil {
		return nil
	}
	owner, room := text(r, "owner"), text(r, "room")
	if room == "" {
		room = owner + "-r-" + strings.TrimPrefix(r.ID, "R-")
	}
	if _, err := m.d.Sessions.Open(ctx, &sdk.SessionOpenRequest{Name: room, Agent: owner, Kind: sdk.SessionKindRoom, Topic: text(r, "content")}); err != nil {
		return err
	}
	m.d.Sessions.SetRoomWait(r.ID, room, sdk.WaitingReasonReminder)
	// 描述與附件（RMD-23）接在要查什麼後面：秘書查證要的背景都在這一則。
	body := "［待辦 " + r.ID + "］" + text(r, "check")
	if desc := strings.TrimSpace(r.Body); desc != "" {
		body += "\n描述：" + desc
	}
	body += attach.Lines(attach.StringList(r, attach.Field))
	_, err := m.d.Bus.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, From: "system", To: room, Body: body, IdempotencyKey: r.ID + "#check#" + stamp(r, "at").Format(time.RFC3339)})
	return err
}

// markRoomWait：有專屬對話（room）而且還沒做完、沒取消的待辦，那個對話顯示「等待辦」；其餘取消登記。
func (m *Module) markRoomWait(id, room, status string) {
	if m.d.Sessions == nil {
		return
	}
	if room == "" || status == "done" || status == "cancelled" {
		room = ""
	}
	m.d.Sessions.SetRoomWait(id, room, sdk.WaitingReasonReminder)
}
