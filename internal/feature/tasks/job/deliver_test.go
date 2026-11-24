package job

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

type fakeRenderer struct{}

func (fakeRenderer) RenderJob(_ context.Context, agent, body string) (string, error) {
	return body + "（" + agent + " 的設定）", nil
}

type fakeRotator struct {
	mu      sync.Mutex
	rotated []string
}

func (r *fakeRotator) Rotate(_ context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rotated = append(r.rotated, name)
	return nil
}

func (r *fakeRotator) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.rotated...)
}

// Guards: REQ-16, REQ-139
// 秘書的排程到期：jobs 自己把內文（slot 套好）封成 routine 封包送給那位秘書，同一次觸發只送一則（REQ-16）。
func TestDueJobSendsRoutineToAgentOnce(t *testing.T) {
	m, c, b, _ := setup(t, job("* * * * *", "skip"))
	m.d.Agents = fakeRenderer{}
	tick(m, c, time.Minute)
	sent := b.envelopes()
	if len(sent) != 1 {
		t.Fatalf("要送一則：%+v", sent)
	}
	e := sent[0]
	if e.To != "code" || e.From != "system" || e.Kind != sdk.EnvelopeKindRoutine || e.Route != sdk.EnvelopeRouteCore || e.Body != "［排程 code/ping］測試用的指令內文（code 的設定）" || !strings.HasPrefix(e.IdempotencyKey, "job:code/ping#") {
		t.Fatalf("封包不對：%+v", e)
	}
	at := b.of("schedule.fire")[0].Payload["at"].(time.Time)
	m.deliver(context.Background(), "code/ping", "code", at) // 同一次觸發再送一次（例如事件重複）
	if n := len(b.envelopes()); n != 1 {
		t.Fatalf("同一次觸發只能送一則：%d", n)
	}
}

// 已經刪掉的排程不用事件裡的舊內文再送。
func TestRemovedJobIsNotSent(t *testing.T) {
	m, c, b, _ := setup(t)
	m.deliver(context.Background(), "code/removed", "code", c.Now())
	if n := len(b.envelopes()); n != 0 {
		t.Fatalf("已刪的排程不該送：%d", n)
	}
}

// 系統級的排程（沒有秘書）只發事件，不送封包。
func TestSystemJobSendsNoEnvelope(t *testing.T) {
	r := job("* * * * *", "skip")
	r.Fields["agent"] = ""
	m, c, b, _ := setup(t, r)
	tick(m, c, time.Minute)
	if len(b.of("schedule.fire")) != 1 || len(b.envelopes()) != 0 {
		t.Fatalf("事件 %d、封包 %d", len(b.of("schedule.fire")), len(b.envelopes()))
	}
}

// Guards: REQ-16, REQ-20
// 排程的封包回覆了：發 schedule.done；夜間整理（action: rotate）請 session 重開收件的那個對話（SES-9）。
func TestRepliedJobPublishesDoneAndRotates(t *testing.T) {
	for _, action := range []string{"rotate", "none"} {
		t.Run(action, func(t *testing.T) {
			r := job("* * * * *", "skip")
			r.Fields["action"] = action
			m, c, b, _ := setup(t, r)
			rot := &fakeRotator{}
			m.d.Sessions = rot
			m.deliver(context.Background(), "code/ping", "code", c.Now())
			e := b.envelopes()[0]
			m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "bus.envelope.state", Payload: map[string]any{"id": e.ID, "kind": string(sdk.EnvelopeKindRoutine), "state": string(sdk.LifecycleWorking)}})
			if len(b.of("schedule.done")) != 0 {
				t.Fatal("還沒回覆不能算做完")
			}
			m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "bus.envelope.state", Payload: map[string]any{"id": e.ID, "kind": string(sdk.EnvelopeKindRoutine), "state": string(sdk.LifecycleReplied)}})
			done := b.of("schedule.done")
			if len(done) != 1 || done[0].Payload["id"] != "code/ping" {
				t.Fatalf("schedule.done：%+v", done)
			}
			want := 0
			if action == "rotate" {
				want = 1
			}
			if got := rot.names(); len(got) != want || want == 1 && got[0] != "code" {
				t.Fatalf("重開對話：%v", got)
			}
		})
	}
}

// 排程的封包結束在 failed（沒送進去）或 interrupted（做到一半斷了）：只記一行 WARN（哪個排程、哪位秘書、結局、原因），
// 不發任何事件（不推給主人、不發 system.alert、不算 schedule.done）、不重開對話；原因已經在那位秘書的對話裡（法律第 5 條）。
func TestUnfinishedJobOnlyLogsWarning(t *testing.T) {
	for _, state := range []sdk.Lifecycle{sdk.LifecycleFailed, sdk.LifecycleInterrupted} {
		t.Run(string(state), func(t *testing.T) {
			r := job("* * * * *", "skip")
			r.Fields["action"] = "rotate"
			m, c, b, _ := setup(t, r)
			var logs bytes.Buffer
			m.log = slog.New(slog.NewTextHandler(&logs, nil))
			rot := &fakeRotator{}
			m.d.Sessions = rot
			m.deliver(context.Background(), "code/ping", "code", c.Now())
			e := b.envelopes()[0]
			e.Reason = "平台暫時忙不過來" // 假 bus 的 Get 回同一個封包：當成 session 結束封包時記的原因
			before := len(b.events)
			m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "bus.envelope.state", Payload: map[string]any{"id": e.ID, "kind": string(sdk.EnvelopeKindRoutine), "state": string(state), "reason": e.Reason}})
			if got := b.events[before:]; len(got) != 0 {
				t.Fatalf("沒做完不發任何事件：%+v", got)
			}
			if len(rot.names()) != 0 {
				t.Fatal("沒做完不重開對話")
			}
			line := logs.String()
			for _, want := range []string{"level=WARN", "code/ping", "agent=code", "state=" + string(state), "平台暫時忙不過來"} {
				if !strings.Contains(line, want) {
					t.Fatalf("WARN 要寫 %q：%s", want, line)
				}
			}
		})
	}
	// 還在路上的（排隊、處理中、等確認）什麼都不發、不記。
	m, c, b, _ := setup(t, job("* * * * *", "skip"))
	var logs bytes.Buffer
	m.log = slog.New(slog.NewTextHandler(&logs, nil))
	m.deliver(context.Background(), "code/ping", "code", c.Now())
	e := b.envelopes()[0]
	before := len(b.events)
	for _, st := range []sdk.Lifecycle{sdk.LifecycleQueued, sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked} {
		m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "bus.envelope.state", Payload: map[string]any{"id": e.ID, "kind": string(sdk.EnvelopeKindRoutine), "state": string(st)}})
	}
	if n := len(b.events) - before; n != 0 || logs.Len() != 0 {
		t.Fatalf("還沒結束不發不記：%d 則事件、log %q", n, logs.String())
	}
}

// 不是排程送的封包回覆了不關 jobs 的事。
func TestOtherEnvelopesIgnored(t *testing.T) {
	m, _, b, _ := setup(t, vault.Record{Kind: "job", ID: "code/ping", Fields: map[string]any{"action": "rotate"}})
	rot := &fakeRotator{}
	m.d.Sessions = rot
	var logs bytes.Buffer
	m.log = slog.New(slog.NewTextHandler(&logs, nil))
	e, _ := b.Send(context.Background(), &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, To: "code", IdempotencyKey: "R-1#check#x"})
	before := len(b.events)
	m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "bus.envelope.state", Payload: map[string]any{"id": e.ID, "kind": string(sdk.EnvelopeKindRoutine), "state": string(sdk.LifecycleReplied)}})
	m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "bus.envelope.state", Payload: map[string]any{"id": e.ID, "kind": string(sdk.EnvelopeKindRoutine), "state": string(sdk.LifecycleFailed)}})
	if len(b.events) != before || len(rot.names()) != 0 || logs.Len() != 0 {
		t.Fatalf("別人的封包不能發事件、重開對話或記「排程沒做完」：%+v、log %q", b.events[before:], logs.String())
	}
}
