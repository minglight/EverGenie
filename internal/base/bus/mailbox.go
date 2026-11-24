package bus

import (
	"context"
	"os"
	"sort"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

type mailbox struct {
	requests chan func()
	records  []Envelope
}

func (m *Module) runBox(b *mailbox) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.recoverWorker()
		for {
			select {
			case <-m.ctx.Done():
				return
			case f := <-b.requests:
				f()
			}
		}
	}()
}
func (m *Module) box(to string) *mailbox {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b := m.boxes[to]; b != nil {
		return b
	}
	b := &mailbox{requests: make(chan func())}
	m.boxes[to] = b
	m.runBox(b)
	return b
}
func (m *Module) allBoxes(to string) []*mailbox {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*mailbox
	for name, b := range m.boxes {
		if to == "" || to == name {
			out = append(out, b)
		}
	}
	return out
}
func (m *Module) call(ctx context.Context, b *mailbox, f func()) error {
	done := make(chan struct{})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return platform.Unavailable("訊息服務已停止")
	case b.requests <- func() { defer close(done); f() }:
	}
	// Once accepted, wait for the durable result, even if the caller cancels.
	<-done
	return nil
}
func clone(e Envelope) Envelope {
	v := *e
	v.History = append([]sdk.LifecycleStep(nil), e.History...)
	return &v
}
func (m *Module) get(ctx context.Context, id string) (Envelope, error) {
	for _, b := range m.allBoxes("") {
		var out Envelope
		if err := m.call(ctx, b, func() {
			for _, e := range b.records {
				if e.ID == id {
					out = clone(e)
					return
				}
			}
		}); err != nil {
			return nil, err
		}
		if out != nil {
			return out, nil
		}
	}
	return nil, platform.NotFound("找不到這則訊息")
}
func (m *Module) Get(ctx context.Context, id string) (Envelope, error) {
	m.life.RLock()
	defer m.life.RUnlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	return m.get(ctx, id)
}
func (m *Module) Send(ctx context.Context, e Envelope) (Envelope, error) {
	m.life.RLock()
	defer m.life.RUnlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, platform.Invalid("envelope", "請提供訊息")
	}
	e = clone(e)
	if e.To == "" {
		return nil, platform.Invalid("to", "請指定收件者")
	}
	if e.From == "" {
		return nil, platform.Invalid("from", "請指定寄件者")
	}
	switch e.Kind {
	case sdk.EnvelopeKindChat, sdk.EnvelopeKindSay, sdk.EnvelopeKindDelegate, sdk.EnvelopeKindReport, sdk.EnvelopeKindAnswer, sdk.EnvelopeKindRoutine, sdk.EnvelopeKindEcho:
	default:
		return nil, platform.Invalid("kind", "請指定有效的訊息種類")
	}
	if e.Route == "" {
		e.Route = sdk.EnvelopeRouteCore
	}
	if e.Route != sdk.EnvelopeRouteCore && e.Route != sdk.EnvelopeRouteDirect {
		return nil, platform.Invalid("route", "請指定有效的傳送方式")
	}
	if e.IdempotencyKey != "" {
		m.idemMu.Lock()
		defer m.idemMu.Unlock()
		if id := m.idem[e.IdempotencyKey]; id != "" {
			return m.get(ctx, id)
		}
	}
	var result Envelope
	var writeErr error
	b := m.box(e.To)
	err := m.call(ctx, b, func() {
		e.ID = platform.NewID()
		e.CreatedAt = m.d.Clock.Now()
		e.Attempts = 0
		e.Reason = ""
		e.LastMessage = ""
		e.Lifecycle = sdk.LifecycleQueued
		if e.Route == sdk.EnvelopeRouteDirect {
			e.Lifecycle = sdk.LifecycleNotified
		}
		e.History = []sdk.LifecycleStep{{State: e.Lifecycle, At: e.CreatedAt}}
		if writeErr = m.persist(e); writeErr != nil {
			return
		}
		b.records = append(b.records, e)
		result = clone(e)
		m.changed(e)
	})
	if err != nil {
		return nil, err
	}
	if writeErr != nil {
		return nil, writeErr
	}
	if e.IdempotencyKey != "" {
		m.idem[e.IdempotencyKey] = e.ID
	}
	return result, nil
}
func (m *Module) List(ctx context.Context, to string, states ...Lifecycle) ([]Envelope, error) {
	m.life.RLock()
	defer m.life.RUnlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	var out []Envelope
	for _, b := range m.allBoxes(to) {
		if err := m.call(ctx, b, func() {
			for _, e := range b.records {
				if len(states) == 0 {
					out = append(out, clone(e))
					continue
				}
				for _, s := range states {
					if e.Lifecycle == s {
						out = append(out, clone(e))
						break
					}
				}
			}
		}); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
func validTransition(from, to Lifecycle) bool {
	switch from {
	case sdk.LifecycleQueued:
		return to == sdk.LifecycleNotified || to == sdk.LifecycleFailed
	case sdk.LifecycleNotified:
		return to == sdk.LifecycleQueued || to == sdk.LifecycleReceived || to == sdk.LifecycleFailed
	// received → queued：收件方的帳號額度用完，那一輪什麼都沒做，退回排隊等恢復後補送（REQ-378）；不算一次貼上失敗。
	case sdk.LifecycleReceived:
		return to == sdk.LifecycleWorking || to == sdk.LifecycleBlocked || to == sdk.LifecycleReplied || to == sdk.LifecycleUnanswered || to == sdk.LifecycleInterrupted || to == sdk.LifecycleQueued
	case sdk.LifecycleWorking:
		return to == sdk.LifecycleBlocked || to == sdk.LifecycleReplied || to == sdk.LifecycleUnanswered || to == sdk.LifecycleInterrupted
	case sdk.LifecycleBlocked:
		return to == sdk.LifecycleWorking || to == sdk.LifecycleInterrupted
	}
	return false
}
func ended(s Lifecycle) bool {
	return s == sdk.LifecycleReplied || s == sdk.LifecycleUnanswered || s == sdk.LifecycleInterrupted || s == sdk.LifecycleFailed
}
func (m *Module) Transition(ctx context.Context, id string, to Lifecycle, opt TransitionOpt) error {
	m.life.RLock()
	defer m.life.RUnlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	e, err := m.get(ctx, id)
	if err != nil {
		return err
	}
	b := m.box(e.To)
	var result error
	if err := m.call(ctx, b, func() {
		for i, current := range b.records {
			if current.ID != id {
				continue
			}
			if !validTransition(current.Lifecycle, to) {
				result = platform.Conflict("訊息不能轉到這個狀態")
				return
			}
			next := clone(current)
			next.Lifecycle = to
			next.Reason = opt.Reason
			next.LastMessage = truncate(opt.LastMessage, 200)
			if current.Lifecycle == sdk.LifecycleNotified && to == sdk.LifecycleQueued {
				next.Attempts++
			}
			now := m.d.Clock.Now()
			last := next.History[len(next.History)-1].At
			if now.Before(last) {
				now = last
			}
			next.History = append(next.History, sdk.LifecycleStep{State: to, At: now, Detail: opt.Reason})
			if result = m.persist(next); result != nil {
				return
			}
			b.records[i] = next
			m.changed(next)
			return
		}
		result = platform.NotFound("找不到這則訊息")
	}); err != nil {
		return err
	}
	return result
}
func (m *Module) Sweep(ctx context.Context, now time.Time) (int, error) {
	m.life.RLock()
	defer m.life.RUnlock()
	if err := m.ready(ctx); err != nil {
		return 0, err
	}
	// Serialize deduplication so no key can resolve to a removed envelope.
	m.idemMu.Lock()
	defer m.idemMu.Unlock()
	removed := 0
	for _, b := range m.allBoxes("") {
		var result error
		err := m.call(ctx, b, func() {
			for i := 0; i < len(b.records); {
				e := b.records[i]
				age := 7 * 24 * time.Hour
				if e.Lifecycle == sdk.LifecycleFailed {
					age = 30 * 24 * time.Hour
				}
				if !ended(e.Lifecycle) || now.Sub(e.History[len(e.History)-1].At) < age {
					i++
					continue
				}
				if err := os.Remove(m.path(e.ID)); err != nil && !os.IsNotExist(err) {
					result = platform.Internal("無法清理過期的訊息")
					return
				}
				delete(m.idem, e.IdempotencyKey)
				b.records = append(b.records[:i], b.records[i+1:]...)
				removed++
			}
		})
		if err != nil {
			return removed, err
		}
		if result != nil {
			return removed, result
		}
	}
	return removed, nil
}
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
