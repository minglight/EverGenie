package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// adoptedAfterLimit（DEV-162）：排程的夜間整理貼進去就碰到額度用完、退回排隊；
// 過了恢復時間補送，引擎說「已經在她的對話裡」→ 記收到、替它開一輪（adopted）。平台之後沒有接著做。
func adoptedAfterLimit(t *testing.T) (f *dispatchFixture, nightly *sdk.Envelope) {
	t.Helper()
	f = dispatchSetup(t)
	f.e.Configure(fake.Behavior{Delivered: true, Recheck: true})
	nightly = f.send(t, &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, From: "system", Body: "［排程 code/nightly］夜間整理", IdempotencyKey: "job:code/nightly#1"})
	f.begin(t, nightly)
	f.want(t, nightly.ID, sdk.LifecycleReceived)
	until := f.h.Clock.Now().Add(20 * time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: until})
	f.want(t, nightly.ID, sdk.LifecycleQueued)
	f.h.Clock.Advance(until.Sub(f.h.Clock.Now()))
	f.untilState(t, nightly.ID, sdk.LifecycleReceived) // 引擎說已經在她的對話裡
	return f, nightly
}

// untilState：記回待命時也叫醒了派送器的背景迴圈，貼上可能是它做的：多 Tick 幾次等那次交給引擎的結果回來。
func (f *dispatchFixture) untilState(t *testing.T, id string, want sdk.Lifecycle) {
	t.Helper()
	for i := 0; i < 200 && f.get(t, id).Lifecycle != want; i++ {
		f.tick()
		time.Sleep(5 * time.Millisecond)
	}
	f.want(t, id, want)
}

// flushedWith 等狀態檔寫進去而且含 marker（背景的寫入迴圈可能正寫到一半：只叫一次 flushDirty 不保證檔案已經是新的）。
func (f *dispatchFixture) flushedWith(t *testing.T, name, marker string) {
	t.Helper()
	f.flushedUntil(t, name, func(data string) bool { return strings.Contains(data, marker) })
}

func (f *dispatchFixture) flushedUntil(t *testing.T, name string, ok func(string) bool) {
	t.Helper()
	path := platform.RuntimeDir(f.h.Dir, "sessions", name+".md")
	var data []byte
	for i := 0; i < 400; i++ {
		must(t, f.m.flushDirty())
		data, _ = os.ReadFile(path)
		if ok(string(data)) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("狀態檔一直不對：%s", data)
}

// storedTurn 讀狀態檔裡會存的那一輪。
func (f *dispatchFixture) storedTurn(name string) turn {
	f.m.mu.Lock()
	defer f.m.mu.Unlock()
	return f.m.entries[name].turn
}

// lastDelivery 是最近一次交給引擎的請求。
func (f *dispatchFixture) lastDelivery(t *testing.T) *sdk.EngineDeliverRequest {
	t.Helper()
	all := f.deliveries()
	if len(all) == 0 {
		t.Fatal("還沒有交給引擎過")
	}
	return all[len(all)-1]
}

// 額度恢復後補送的那句，引擎說已經在她的對話裡、平台卻沒有接著做（畫面停在輸入框，沒有開始也沒有結束事件）。
// 那一輪不能永遠掛著：期限到了作廢、那句退回排隊，請引擎一定重新貼一次（不接受「已經在對話裡」）。
// 重貼之後還是完全沒有事件，由 10 分鐘的保險記成沒送到（可以重送），排在後面的話送得進去。
func TestAdoptedTurnExpiresWhenPlatformNeverResumes(t *testing.T) {
	f, nightly := adoptedAfterLimit(t)
	if got := f.storedTurn("code"); !got.Adopted || got.Envelope != nightly.ID {
		t.Fatalf("替它開的那一輪要記在狀態檔上（含 adopted）：%+v", got)
	}
	second := f.send(t, ownerChat("早安，今天有什麼事"))
	n := len(f.deliveries())

	// 期限內：等平台自己接著做，什麼都不動。
	f.h.Clock.Advance(AdoptSettleAfter - time.Second)
	f.tick()
	f.want(t, nightly.ID, sdk.LifecycleReceived)
	f.want(t, second.ID, sdk.LifecycleQueued)
	if len(f.deliveries()) != n {
		t.Fatalf("期限內不重送：%d → %d", n, len(f.deliveries()))
	}

	// 期限到了、她待命：這一輪作廢，那句退回排隊，這次要引擎真的貼。
	f.h.Clock.Advance(time.Second)
	f.tick()
	f.untilState(t, nightly.ID, sdk.LifecycleNotified)
	if last := f.lastDelivery(t); len(f.deliveries()) != n+1 || last.EnvelopeID != nightly.ID || !last.Force {
		t.Fatalf("作廢後同一則要請引擎一定重貼一次：%d → %d %+v", n, len(f.deliveries()), last)
	}
	if got := f.get(t, nightly.ID); rescues(got) != 1 {
		t.Fatalf("歷程要留下作廢重送的那一筆：%+v", got.History)
	}
	if got := f.storedTurn("code"); got.Envelope != "" || got.Adopted {
		t.Fatalf("掛著的那一輪（含存檔的）要清掉：%+v", got)
	}
	f.want(t, second.ID, sdk.LifecycleQueued)

	// 重貼之後還是沒有任何事件：10 分鐘的保險接手，這一次補送已經自救過，不再重送，記沒送到；後面那句送進去。
	f.h.Clock.Advance(StuckAfter)
	f.tick()
	if got := f.get(t, nightly.ID); got.Lifecycle != sdk.LifecycleFailed || got.Reason != notStartedReason {
		t.Fatalf("重貼後還是沒開始要記沒送到（不是中斷）：%s %q %+v", got.Lifecycle, got.Reason, got.History)
	}
	f.untilState(t, second.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: second.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "早安"})
	f.want(t, second.ID, sdk.LifecycleReplied)
}

// 恢復時間到了、平台其實還在限額（連續兩輪）：到期的重貼讓平台再說一次額度用完，那句照額度的規則退回排隊等下一個恢復時間，
// 不能被記成中斷或沒送到；額度真的回來之後，排著的每一句都送達、各只做一次。
func TestAdoptedExpiryWhileStillLimitedWaitsInsteadOfInterrupting(t *testing.T) {
	f, nightly := adoptedAfterLimit(t)
	second := f.send(t, ownerChat("早安，今天有什麼事"))
	alive := func(step string) {
		t.Helper()
		for _, e := range []*sdk.Envelope{nightly, second} {
			if got := f.get(t, e.ID); got.Lifecycle == sdk.LifecycleInterrupted || got.Lifecycle == sdk.LifecycleFailed {
				t.Fatalf("%s：額度還沒回來，話不能被記成沒做完：%s %q %+v", step, got.Lifecycle, got.Reason, got.History)
			}
		}
	}
	for round := 1; round <= 2; round++ {
		f.h.Clock.Advance(AdoptSettleAfter)
		f.tick()
		f.untilState(t, nightly.ID, sdk.LifecycleNotified)
		if last := f.lastDelivery(t); last.EnvelopeID != nightly.ID || !last.Force {
			t.Fatalf("第 %d 輪：到期要請引擎一定重貼：%+v", round, last)
		}
		f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: nightly.Body})
		until := f.h.Clock.Now().Add(20 * time.Minute)
		f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: until}) // 平台：還在限額
		f.want(t, nightly.ID, sdk.LifecycleQueued)
		alive("還在限額")
		f.tickEvery(time.Minute, 19*time.Minute) // 等恢復時間的這一段什麼都不動
		f.want(t, nightly.ID, sdk.LifecycleQueued)
		f.want(t, second.ID, sdk.LifecycleQueued)
		f.h.Clock.Advance(until.Sub(f.h.Clock.Now()))
		f.untilState(t, nightly.ID, sdk.LifecycleReceived) // 補送：引擎又說已經在她的對話裡
		alive("補送")
	}
	// 額度真的回來了：到期重貼，她做完；後面那句接著送。
	f.h.Clock.Advance(AdoptSettleAfter)
	f.tick()
	f.untilState(t, nightly.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: nightly.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "夜間整理完成"})
	f.want(t, nightly.ID, sdk.LifecycleReplied)
	f.untilState(t, second.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: second.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "早安"})
	f.want(t, second.ID, sdk.LifecycleReplied)
	for _, e := range []*sdk.Envelope{nightly, second} {
		replied := 0
		for _, step := range f.get(t, e.ID).History {
			if step.State == sdk.LifecycleReplied {
				replied++
			}
		}
		if replied != 1 {
			t.Fatalf("每一句只做完一次：%+v", f.get(t, e.ID).History)
		}
	}
}

// 這一次補送已經自救過（貼上沒開始、重送時引擎說已經在她的對話裡），期限又到了：不再貼。先查對話記錄——
// 這句之後她有回話就記已回覆（開始／結束事件丟了，她其實做完了）；沒有才記中斷，原因不斷言她沒做。
func TestAdoptedGiveUpChecksTranscriptBeforeInterrupting(t *testing.T) {
	done := []sdk.ChatMessage{{Kind: sdk.ChatKindToolCall, Text: "看 CI"}, {Kind: sdk.ChatKindAssistant, Text: "CI 是綠的"}}
	// 做到一半停在工具上、或最後一則是平台的錯誤訊息（額度用完那一行）：都不算她回了話。
	halfway := []sdk.ChatMessage{{Kind: sdk.ChatKindAssistant, Text: "我來看"}, {Kind: sdk.ChatKindToolCall, Text: "看 CI"}}
	limited := []sdk.ChatMessage{{Kind: sdk.ChatKindAssistant, Text: "You've hit your limit", IsError: true}}
	for i, msgs := range [][]sdk.ChatMessage{done, nil, halfway, limited} {
		answered := i == 0
		f := dispatchSetup(t)
		b := fake.Behavior{Delivered: true, Recheck: true, Messages: msgs}
		f.e.Configure(b)
		e := f.send(t, &sdk.Envelope{Body: "幫我看一下 CI"})
		f.tick()
		f.want(t, e.ID, sdk.LifecycleNotified)
		f.h.Clock.Advance(StuckAfter) // 貼上沒開始：重送一次，引擎說已經在她的對話裡
		f.tick()
		f.untilState(t, e.ID, sdk.LifecycleReceived)
		f.h.Clock.Advance(AdoptSettleAfter)
		f.tick()
		got := f.get(t, e.ID)
		if answered && got.Lifecycle != sdk.LifecycleReplied {
			t.Fatalf("對話記錄裡她回了這句，要記已回覆：%s %q %+v", got.Lifecycle, got.Reason, got.History)
		}
		if !answered && (got.Lifecycle != sdk.LifecycleInterrupted || got.Reason != adoptGaveUpReason) {
			t.Fatalf("對話記錄裡沒有她的回話才記中斷：%s %q %+v", got.Lifecycle, got.Reason, got.History)
		}
		if n := len(f.deliveries()); n != 2 {
			t.Fatalf("不再貼第三次：%d", n)
		}
		if ds := f.deliveries(); ds[0].Resend || !ds[1].Resend {
			t.Fatalf("第一次送不說是重送、之後的要說（引擎不記得時才敢去對話記錄找）：%+v", ds)
		}
	}
}

// 核心停了一陣子再開：停著的那一段不算在期限裡。重開後第一輪不能因為「她已經待命很久」就馬上作廢重貼或報卡住，
// 從這次啟動起滿期限才動。
func TestDeadlinesStartFromCoreBootNotFromBeforeDowntime(t *testing.T) {
	f, nightly := adoptedAfterLimit(t)
	second := f.send(t, ownerChat("在嗎"))
	n := len(f.deliveries())
	f.h.Clock.Advance(2 * time.Hour) // 核心停著
	f.m.mu.Lock()
	f.m.bootAt = f.h.Clock.Now() // 這次啟動
	f.m.mu.Unlock()
	f.tick()
	f.want(t, nightly.ID, sdk.LifecycleReceived)
	f.want(t, second.ID, sdk.LifecycleQueued)
	if got := f.m.Stuck(context.Background()); len(got) != 0 || len(f.deliveries()) != n {
		t.Fatalf("重開後第一輪不誤報、不立刻自救：%+v %d → %d", got, n, len(f.deliveries()))
	}
	f.h.Clock.Advance(AdoptSettleAfter)
	f.tick()
	f.untilState(t, nightly.ID, sdk.LifecycleNotified)
}

// 作廢後重送，引擎這次真的貼進去了（平台沒說會自己接著做的那種）：照平常的收到、回覆。
func TestAdoptedTurnExpiryRedeliversAndCompletes(t *testing.T) {
	f, nightly := adoptedAfterLimit(t)
	f.e.Configure(fake.Behavior{Delivered: true}) // 之後同一個編號再交來照常貼
	f.h.Clock.Advance(AdoptSettleAfter)
	f.tick()
	f.untilState(t, nightly.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: nightly.Body})
	f.want(t, nightly.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "整理完了"})
	f.want(t, nightly.ID, sdk.LifecycleReplied)
}

// 重開服務（從狀態檔載回來）之後，掛著的那一輪照樣有期限；升級前存的狀態檔沒有 adopted 欄位也一樣（看那句的歷程）。
func TestAdoptedTurnStillExpiresAfterRestart(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		f, nightly := adoptedAfterLimit(t)
		second := f.send(t, ownerChat("在嗎"))
		f.flushedWith(t, "code", "adopted: true")
		f.m.dispatchMu.Lock()
		f.m.mu.Lock()
		err := f.m.restore() // 核心重開：記憶體裡的東西都從狀態檔讀回
		restored := f.m.turns["code"]
		ok := err == nil && restored != nil && restored.Envelope == nightly.ID && restored.Adopted
		if ok && legacy {
			restored.Adopted = false // 升級前的狀態檔：turn 沒有 adopted
			f.m.entries["code"].turn.Adopted = false
		}
		f.m.mu.Unlock()
		f.m.dispatchMu.Unlock()
		if !ok {
			t.Fatalf("重開後那一輪要帶著 adopted 讀回來：%v %+v", err, restored)
		}
		f.m.mu.Lock()
		f.m.bootAt = f.h.Clock.Now()
		f.m.mu.Unlock()
		f.h.Clock.Advance(AdoptSettleAfter)
		f.tick()
		f.untilState(t, nightly.ID, sdk.LifecycleNotified)
		if got := f.get(t, nightly.ID); rescues(got) != 1 || !f.lastDelivery(t).Force {
			t.Fatalf("legacy=%v 重開後期限到了也要作廢重貼：%+v", legacy, got.History)
		}
		f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: nightly.Body})
		f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "整理完了"})
		f.want(t, nightly.ID, sdk.LifecycleReplied)
		f.untilState(t, second.ID, sdk.LifecycleNotified)
	}
}

// 期限只在她待命時算：平台接著做了（忙著）、在等主人回答確認、又碰到額度用完，都不作廢。
func TestAdoptedTurnNotExpiredWhileBusyWaitingOrLimited(t *testing.T) {
	// 平台接著做了：這一輪照常走到結束。
	f, nightly := adoptedAfterLimit(t)
	f.h.Clock.Advance(time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "usage limit has reset, continue"})
	f.h.Clock.Advance(30 * time.Minute)
	f.tick()
	f.want(t, nightly.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "夜間整理完成"})
	f.want(t, nightly.ID, sdk.LifecycleReplied)

	// 又碰到額度用完：照額度的規則退回排隊，恢復前不動。
	f, nightly = adoptedAfterLimit(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: f.h.Clock.Now().Add(time.Hour)})
	f.want(t, nightly.ID, sdk.LifecycleQueued)
	f.h.Clock.Advance(AdoptSettleAfter + time.Minute)
	f.tick()
	got := f.get(t, nightly.ID)
	if got.Lifecycle != sdk.LifecycleQueued || rescues(got) != 0 {
		t.Fatalf("額度用完時不作廢：%s %+v", got.Lifecycle, got.History)
	}

	// 在等主人回答確認：不作廢。
	f, nightly = adoptedAfterLimit(t)
	go func() {
		_, _, _ = f.m.RequestApproval(context.Background(), &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindPermission, Tool: "Bash", Summary: "跑指令"})
	}()
	for i := 0; i < 200; i++ {
		if s, _ := f.m.Get(context.Background(), "code"); s != nil && s.Pending != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.m.drainEvents(context.Background())
	f.h.Clock.Advance(AdoptSettleAfter + time.Minute)
	f.tick()
	if got := f.get(t, nightly.ID); terminal(got.Lifecycle) || got.Lifecycle == sdk.LifecycleQueued || rescues(got) != 0 {
		t.Fatalf("等確認時不作廢：%s %+v", got.Lifecycle, got.History)
	}
}

// 那一輪作廢、那句收掉之後，平台才接著做：那一輪還是原寄件人（這裡是別位秘書）觸發的，不能記成主人在終端機打字
// （法律第 4 條）。中間別的話做過一輪也一樣（記在對話上，不跟著某一輪）。
func TestTurnResumedAfterAdoptedVoidedKeepsOriginalSender(t *testing.T) {
	f := dispatchSetup(t)
	f.e.Configure(fake.Behavior{Delivered: true, Recheck: true})
	a := f.send(t, &sdk.Envelope{Body: "幫我看一下 CI"}) // 秘書 work 傳的
	f.tick()
	f.h.Clock.Advance(StuckAfter)
	f.tick()
	f.untilState(t, a.ID, sdk.LifecycleReceived)
	f.h.Clock.Advance(AdoptSettleAfter)
	f.tick()
	f.want(t, a.ID, sdk.LifecycleInterrupted)
	b := f.send(t, ownerChat("在嗎"))
	f.untilState(t, b.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: b.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "在"})
	f.want(t, b.ID, sdk.LifecycleReplied)
	f.h.Clock.Advance(time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "continue"})
	if got := f.storedTurn("code"); got.Envelope != "" || got.From != "work" || got.Origin != "agent" {
		t.Fatalf("接著做的那一輪要記成原寄件人（秘書 work）：%+v", got)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "CI 是綠的"})
	f.h.Clock.Advance(time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "謝謝"})
	if got := f.storedTurn("code"); got.From != "owner(session)" || got.Origin != "owner" {
		t.Fatalf("只沿用一次：再下一輪是主人自己打的：%+v", got)
	}
}

// 作廢之後過了很久（超過 VoidedKeep）才有一輪沒對到封包的：是主人自己在她畫面打的，不沿用舊寄件人。
func TestOwnerTypingLongAfterVoidedTurnIsOwner(t *testing.T) {
	f := dispatchSetup(t)
	f.e.Configure(fake.Behavior{Delivered: true, Recheck: true})
	a := f.send(t, &sdk.Envelope{Body: "幫我看一下 CI"})
	f.tick()
	f.h.Clock.Advance(StuckAfter)
	f.tick()
	f.untilState(t, a.ID, sdk.LifecycleReceived)
	f.h.Clock.Advance(AdoptSettleAfter)
	f.tick()
	f.want(t, a.ID, sdk.LifecycleInterrupted)
	f.h.Clock.Advance(8 * time.Hour)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "早安，幫我看一下今天的行程"})
	if got := f.storedTurn("code"); got.From != "owner(session)" || got.Origin != "owner" {
		t.Fatalf("8 小時後主人直接打字不能記成舊寄件人：%+v", got)
	}
}

// 替「已經在她的對話裡」開的那一輪做完之後，存檔不再帶 adopted：核心重開讀回來，主人之後直接打的一輪是主人的。
func TestOwnerTypingAfterAdoptedTurnEndedAndRestartIsOwner(t *testing.T) {
	f, nightly := adoptedAfterLimit(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "done"})
	f.want(t, nightly.ID, sdk.LifecycleReplied)
	if got := f.storedTurn("code"); got.Adopted {
		t.Fatalf("這一輪收尾後不再是 adopted：%+v", got)
	}
	f.flushedUntil(t, "code", func(data string) bool { return !strings.Contains(data, "adopted: true") })
	f.m.dispatchMu.Lock()
	f.m.mu.Lock()
	err := f.m.restore()
	f.m.mu.Unlock()
	f.m.dispatchMu.Unlock()
	must(t, err)
	f.h.Clock.Advance(time.Hour)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "主人自己在她畫面打的一句"})
	if got := f.storedTurn("code"); got.From != "owner(session)" || got.Origin != "owner" {
		t.Fatalf("主人直接打字被記成舊寄件人：%+v", got)
	}
}

// 「自救過一次」照每一次補送算：到期重貼與「貼上沒開始」的重送都算；因為別的原因退回排隊（又碰到額度用完）之後是新的一次；
// 畫面開著確認框的退回不重新算。
func TestRescuesCountOnlyCurrentRedelivery(t *testing.T) {
	step := func(state sdk.Lifecycle, detail string) sdk.LifecycleStep {
		return sdk.LifecycleStep{State: state, Detail: detail}
	}
	expired, adopted := step(sdk.LifecycleQueued, adoptExpiredReason), step(sdk.LifecycleReceived, adoptedReason)
	for _, c := range []struct {
		name    string
		history []sdk.LifecycleStep
		want    int
	}{
		{"還沒自救過", []sdk.LifecycleStep{step(sdk.LifecycleQueued, ""), adopted}, 0},
		{"到期重貼過一次", []sdk.LifecycleStep{adopted, expired, adopted}, 1},
		{"貼上沒開始重送過一次", []sdk.LifecycleStep{step(sdk.LifecycleNotified, ""), step(sdk.LifecycleQueued, stuckRetryReason), adopted}, 1},
		{"中間開著確認框不重新算", []sdk.LifecycleStep{adopted, expired, step(sdk.LifecycleQueued, dialogHoldReason), adopted}, 1},
		{"之後又碰到額度用完是新的一次", []sdk.LifecycleStep{adopted, expired, adopted, step(sdk.LifecycleQueued, "額度用完"), adopted}, 0},
	} {
		if got := rescues(&sdk.Envelope{History: c.history}); got != c.want {
			t.Fatalf("%s：%d，要 %d", c.name, got, c.want)
		}
	}
}

// 到期重貼的那一輪照常開始了，平台卻又說額度用完；額度回來後平台自己接著做（沒對到封包的一輪）：
// 那一輪還是原寄件人（排程）觸發的，不能記成主人打字。
func TestTurnResumedAfterRepasteHitLimitAgainIsNotOwner(t *testing.T) {
	f, nightly := adoptedAfterLimit(t)
	f.h.Clock.Advance(AdoptSettleAfter)
	f.tick()
	f.untilState(t, nightly.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: nightly.Body})
	first := f.storedTurn("code")
	until := f.h.Clock.Now().Add(20 * time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: until})
	f.want(t, nightly.ID, sdk.LifecycleQueued)
	f.h.Clock.Advance(until.Sub(f.h.Clock.Now()) + time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "continue"})
	if got := f.storedTurn("code"); got.From != first.From || got.Origin != first.Origin || got.From == "owner(session)" {
		t.Fatalf("平台自己接著做的那一輪要記成原寄件人（排程），不是主人打字：%+v（原本 %+v）", got, first)
	}
}
