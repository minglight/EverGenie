package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

func addAgent(t *testing.T, f *dispatchFixture, id, mode string) {
	t.Helper()
	f.h.WithRecords(vault.Record{Kind: "agent", ID: id, Fields: map[string]any{"persona": "test", "duty": "測試", "enabled": true, "mode": mode}})
}

func (f *dispatchFixture) tickFor(d time.Duration) {
	f.h.Clock.Advance(d)
	f.tick()
}

// Guards: REQ-72, REQ-194
func TestOndemandStartsOnFirstMessage(t *testing.T) {
	f := dispatchSetup(t)
	addAgent(t, f, "solo", "ondemand")
	f.m.config.Sessions.Autostart = true
	f.tick()
	if _, ok := f.m.State("solo"); ok {
		t.Fatal("隨選的秘書沒有人說話前不該開")
	}
	e := f.send(t, &sdk.Envelope{From: "owner(web)", To: "solo", Kind: sdk.EnvelopeKindChat, Body: "第一句話"})
	f.tick()
	state, ok := f.m.State("solo")
	if !ok || state == sdk.SessionStateStopped {
		t.Fatalf("第一句話來了要把主要 session 開起來：%v %v", state, ok)
	}
	f.tick()
	f.tick()
	if got := f.get(t, e.ID).Lifecycle; got != sdk.LifecycleNotified {
		t.Fatalf("開起來、閒著之後那句話要貼進去：%s", got)
	}

	// 閒置 60 分鐘才收掉：59 分鐘還在。
	f.event(t, sdk.EngineEvent{Session: "solo", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "第一句話"})
	f.event(t, sdk.EngineEvent{Session: "solo", Type: sdk.EngineEventTypeTurnEnded, LastMessage: "好"})
	f.tickFor(59 * time.Minute)
	if state, _ := f.m.State("solo"); state == sdk.SessionStateStopped {
		t.Fatal("閒置不到 60 分鐘不該收掉")
	}
	f.tickFor(2 * time.Minute)
	if state, _ := f.m.State("solo"); state != sdk.SessionStateStopped {
		t.Fatalf("閒置 60 分鐘要收掉：%s", state)
	}
	// 常駐的不受影響。
	if state, _ := f.m.State("code"); state == sdk.SessionStateStopped {
		t.Fatal("常駐的 session 不該被收掉")
	}

	// 收掉以後下一句話再開。
	f.send(t, &sdk.Envelope{From: "owner(web)", To: "solo", Kind: sdk.EnvelopeKindChat, Body: "又來了"})
	f.tickFor(time.Second)
	if state, _ := f.m.State("solo"); state == sdk.SessionStateStopped {
		t.Fatal("下一句話要把它再開起來")
	}
}

// Guards: REQ-72, REQ-194
func TestScheduledDayIsResident(t *testing.T) {
	f := dispatchSetup(t)
	addAgent(t, f, "solo", "ondemand")
	addAgent(t, f, "plain", "ondemand")
	f.m.config.Sessions.Autostart = true
	f.m.SetScheduledToday(func(_ context.Context, agent string, _ time.Time) bool { return agent == "solo" })
	f.tick()
	if state, ok := f.m.State("solo"); !ok || state == sdk.SessionStateStopped {
		t.Fatal("當天有排程的隨選秘書當天視為常駐，沒人說話也要開")
	}
	if _, ok := f.m.State("plain"); ok {
		t.Fatal("當天沒有排程的隨選秘書不該開")
	}
	// 閒了好幾個小時也不收（排程隨時會來）。
	for i := 0; i < 4; i++ {
		f.tickFor(time.Hour)
	}
	if state, _ := f.m.State("solo"); state == sdk.SessionStateStopped {
		t.Fatal("當天有排程的隨選秘書閒置也不收")
	}
}

// Guards: REQ-196, REQ-197
