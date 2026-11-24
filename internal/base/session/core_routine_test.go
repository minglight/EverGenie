package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 核心給秘書的系統提示（routine、from system，例：新家第一位秘書剛啟用時的招呼，agents AGS-11）照現有的排隊補送：
// 她的主要對話還沒開時排著；同一個 idempotency key 再送一次不會多一則；派送器把她的主要對話開起來、閒著才貼，
// 只貼一次；那一輪碰到額度用完就退回排隊、恢復前不重送，恢復後補送同一則（不是新的一則）。
// 貼進去的就是原文（「［核心］」開頭），不加主人的外框；那一輪不算主人說的話。
func TestCoreRoutineWaitsForSecretaryThenDeliversOnce(t *testing.T) {
	f := dispatchSetup(t)
	f.m.config.Sessions.Autostart = true
	ctx := context.Background()
	f.h.WithRecords(vault.Record{Kind: "agent", ID: "life", Fields: map[string]any{"persona": "test", "duty": "測試", "enabled": true, "mode": "resident"}})
	greet := &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, From: "system", To: "life", Body: "［核心］主人剛請你上班，請先打招呼。", IdempotencyKey: "first-day:life"}
	e := f.send(t, greet)
	toLife := func() []*sdk.EngineDeliverRequest {
		var out []*sdk.EngineDeliverRequest
		for _, d := range f.deliveries() {
			if d.Session == "life" {
				out = append(out, d)
			}
		}
		return out
	}

	// 對話還沒開：排著；同一個 key 再送一次不多一則。
	f.want(t, e.ID, sdk.LifecycleQueued)
	again := f.send(t, &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, From: "system", To: "life", Body: greet.Body, IdempotencyKey: "first-day:life"})
	if again.ID != e.ID {
		t.Fatalf("同一個 key 再送不能多一則：%s → %s", e.ID, again.ID)
	}
	if rows, err := f.b.List(ctx, "life"); err != nil || len(rows) != 1 {
		t.Fatalf("她的佇列只有一則：%d %v", len(rows), err)
	}

	// 派送器開她的主要對話、閒著才貼，只貼一次、原文照貼。
	f.tickFor(time.Second)
	if st, ok := f.m.State("life"); !ok || st == sdk.SessionStateStopped {
		t.Fatalf("有話排著要開她的對話：%v %v", st, ok)
	}
	f.tickFor(time.Second)
	f.tickFor(time.Second)
	f.want(t, e.ID, sdk.LifecycleNotified)
	if got := toLife(); len(got) != 1 || !strings.HasPrefix(got[0].Text, "［核心］") {
		t.Fatalf("貼一次、原文照貼：%+v", got)
	}

	// 那一輪碰到額度用完：退回排隊，恢復前不重送；恢復後補送同一則。
	f.event(t, sdk.EngineEvent{Session: "life", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: greet.Body})
	f.want(t, e.ID, sdk.LifecycleReceived)
	f.m.mu.Lock()
	notOwner := f.m.entries["life"].lastOwner.IsZero() && f.m.entries["life"].ownerTalk.IsZero()
	f.m.mu.Unlock()
	if !notOwner {
		t.Fatal("核心的系統提示那一輪不算主人說的話（不延後別的話、不算主人在這個對話說過話）")
	}
	until := f.h.Clock.Now().Add(30 * time.Minute)
	f.event(t, sdk.EngineEvent{Session: "life", Type: sdk.EngineEventTypeRateLimited, Until: until})
	f.want(t, e.ID, sdk.LifecycleQueued)
	for i := 0; i < 5; i++ {
		f.tickFor(time.Second)
	}
	if got := len(toLife()); got != 1 {
		t.Fatalf("額度用完時不重送：%d", got)
	}
	f.h.Clock.Advance(until.Sub(f.h.Clock.Now()))
	f.tick()
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	if got := len(toLife()); got != 2 {
		t.Fatalf("恢復後補送同一則：%d", got)
	}
	f.event(t, sdk.EngineEvent{Session: "life", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: greet.Body})
	f.event(t, sdk.EngineEvent{Session: "life", Type: sdk.EngineEventTypeTurnEnded, LastMessage: "你好，我是新來的秘書。"})
	f.want(t, e.ID, sdk.LifecycleReplied)
	if rows, err := f.b.List(ctx, "life"); err != nil || len(rows) != 1 {
		t.Fatalf("從頭到尾只有這一則：%d %v", len(rows), err)
	}
}
