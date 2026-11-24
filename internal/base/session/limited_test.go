package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

// thread 讀這串現在的討論串狀態。
func (f *dispatchFixture) thread(t *testing.T, name string) ThreadSummary {
	t.Helper()
	list, _, err := f.m.Threads(context.Background(), "", true)
	must(t, err)
	for _, th := range list {
		if th.Name == name {
			return th
		}
	}
	t.Fatalf("沒有 %s 這串", name)
	return nil
}

// limitedSetup：主人說一句、她開始處理就碰到額度用完（實際遇過的樣子：收到後只回「You've hit your 5-hour limit」）。
func limitedSetup(t *testing.T) (f *dispatchFixture, first *sdk.Envelope, until time.Time) {
	t.Helper()
	f = dispatchSetup(t)
	first = f.send(t, ownerChat("幫我看一下備份"))
	f.begin(t, first)
	f.want(t, first.ID, sdk.LifecycleReceived)
	until = f.h.Clock.Now().Add(70 * time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: until})
	return f, first, until
}

// 額度用完：limited（不是卡住、不是待命），那一則不算收到、退回排隊；之後的話等著不送、不改送別的對話；
// 泡泡的原因是 limited＋恢復時間；清單灰色那一種（limited＋limited_until）；不發系統提醒、不開事件單。
// Guards: REQ-378
func TestRateLimitedRequeuesAndWaits(t *testing.T) {
	f, first, until := limitedSetup(t)
	alarms := alarmEvents(t, f)
	if st, _ := f.m.State("code"); st != sdk.SessionStateLimited {
		t.Fatalf("額度用完要記成 limited：%s", st)
	}
	got := f.get(t, first.ID)
	if got.Lifecycle != sdk.LifecycleQueued || got.History[len(got.History)-1].Detail == "" {
		t.Fatalf("那一則不算收到，退回排隊並寫原因：%+v", got.History)
	}
	second := f.send(t, ownerChat("還有一件事"))
	n := len(f.deliveries())
	for i := 0; i < 10; i++ {
		f.tick()
		f.h.Clock.Advance(time.Second)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint}) // 平台照樣提示閒著：不能蓋成待命
	f.tick()
	if len(f.deliveries()) != n {
		t.Fatalf("額度用完時不送：%d → %d", n, len(f.deliveries()))
	}
	f.want(t, first.ID, sdk.LifecycleQueued)
	f.want(t, second.ID, sdk.LifecycleQueued)
	if st, _ := f.m.State("code"); st != sdk.SessionStateLimited {
		t.Fatalf("閒著的提示不能把 limited 蓋掉：%s", st)
	}
	for _, m := range f.messagesOf(t, "code") {
		if m.Wait != sdk.MessageWaitLimited || !m.WaitUntil.Equal(until) {
			t.Fatalf("泡泡的原因要是額度用完＋恢復時間：%+v", m)
		}
	}
	th := f.thread(t, "code")
	if th.Status != sdk.ThreadStatusLimited || !th.LimitedUntil.Equal(until) {
		t.Fatalf("討論串要是 limited、帶恢復時間：%+v", th)
	}
	// 等多久都一樣：恢復前狀態不變。
	f.h.Clock.Advance(11 * time.Minute)
	f.tick()
	if st, _ := f.m.State("code"); st != sdk.SessionStateLimited {
		t.Fatalf("恢復前不變：%s", st)
	}
	f.want(t, second.ID, sdk.LifecycleQueued)
	noAlarm(t, alarms)
}

// 已經動手做了一部分才碰到額度（working）：不重送（免得做兩次），記中斷；秘書直接傳的也記中斷。
// Guards: REQ-378
func TestRateLimitedMidTurnNotResent(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, ownerChat("整理一下"))
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, Activity: "Bash"})
	f.want(t, e.ID, sdk.LifecycleWorking)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: f.h.Clock.Now().Add(time.Hour)})
	f.want(t, e.ID, sdk.LifecycleInterrupted)
}

// 恢復後照常：過了恢復時間記回待命、排著的話照順序送；她開始處理就是平常的收到→回覆。
// Guards: REQ-379
func TestRateLimitedResumesAfterReset(t *testing.T) {
	f, first, until := limitedSetup(t)
	second := f.send(t, ownerChat("還有一件事"))
	n := len(f.deliveries())
	f.h.Clock.Advance(until.Sub(f.h.Clock.Now()))
	f.tick()
	if st, _ := f.m.State("code"); st != sdk.SessionStateIdle {
		t.Fatalf("過了恢復時間記回待命：%s", st)
	}
	f.tick()
	f.want(t, first.ID, sdk.LifecycleNotified)
	f.want(t, second.ID, sdk.LifecycleQueued)
	if len(f.deliveries()) != n+1 {
		t.Fatalf("恢復後先送排第一的那則：%d → %d", n, len(f.deliveries()))
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: first.Body})
	f.want(t, first.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "備份正常"})
	f.want(t, first.ID, sdk.LifecycleReplied)
	f.tick()
	f.want(t, second.ID, sdk.LifecycleNotified)

	// 說不出恢復時間：從最後一次說額度用完起 LimitRetryAfter 再試；補送又碰到額度就從那一刻重新算，不會每秒重送。
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: second.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited})
	f.want(t, second.ID, sdk.LifecycleQueued)
	n = len(f.deliveries())
	f.h.Clock.Advance(LimitRetryAfter - time.Second)
	f.tick()
	f.tick()
	if len(f.deliveries()) != n {
		t.Fatal("說不出恢復時間時，LimitRetryAfter 內不送")
	}
	f.h.Clock.Advance(time.Second)
	f.tick()
	f.tick()
	f.want(t, second.ID, sdk.LifecycleNotified)
}

// 額度用完那一輪話已經貼進她的對話；恢復後補送時引擎說「已經在了」，平台過一會兒自己接著做那一輪
// （DEV-99：code-job-nightly 05:10 補送、05:10:36 才自己接著做）。期限（AdoptSettleAfter）內不收尾；
// 這段時間主人說的下一句排著、原因是「等前一句」；平台接著做完那一輪（或只說她閒著，S5-2 當這一輪結束）
// 才收尾記已回覆、再送下一句；全程不發系統提醒。期限到了平台還沒動見 adopt_test.go（DEV-162）。
func TestRateLimitedRecheckWaitsForPlatformResume(t *testing.T) {
	for _, resumes := range []bool{true, false} {
		f := dispatchSetup(t)
		f.e.Configure(fake.Behavior{Delivered: true, Recheck: true})
		alarms := alarmEvents(t, f)
		first := f.send(t, ownerChat("夜間整理"))
		f.begin(t, first)
		until := f.h.Clock.Now().Add(90 * time.Minute)
		f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: until})
		f.want(t, first.ID, sdk.LifecycleQueued)
		f.h.Clock.Advance(until.Sub(f.h.Clock.Now()))
		// 記回待命時也叫醒了派送器的背景迴圈，補送可能是它做的：等那次交給引擎的結果回來。
		for i := 0; i < 100 && f.get(t, first.ID).Lifecycle != sdk.LifecycleReceived; i++ {
			f.tick()
			time.Sleep(10 * time.Millisecond)
		}
		f.want(t, first.ID, sdk.LifecycleReceived) // 引擎說已經在她的對話裡
		second := f.send(t, ownerChat("順便看一下 CI"))
		n := len(f.deliveries())
		for i := 0; i < 2; i++ { // 還在期限內
			f.h.Clock.Advance(time.Minute)
			f.tick()
		}
		f.want(t, first.ID, sdk.LifecycleReceived)
		f.want(t, second.ID, sdk.LifecycleQueued)
		if len(f.deliveries()) != n {
			t.Fatalf("前一句還沒結束，下一句先不貼：%d → %d", n, len(f.deliveries()))
		}
		if w := f.waitOf(t, "code", second.ID); w != sdk.MessageWaitBusy {
			t.Fatalf("等前一句接著做時，後面那句要說得出原因（busy）：%q", w)
		}
		if resumes {
			f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "usage limit has reset, continue"})
			f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "夜間整理完成"})
			f.want(t, first.ID, sdk.LifecycleReplied)
		} else {
			f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint})
			f.want(t, first.ID, sdk.LifecycleReplied)
		}
		f.tick()
		f.want(t, second.ID, sdk.LifecycleNotified)
		noAlarm(t, alarms)
	}
}

// 秘書傳來的話碰到額度用完、恢復後平台自己接著做：那一輪還是那位秘書的（不是主人在終端機打字），
// 權限照秘書傳話判斷、不記成主人剛說過話；做完記已回覆（DEV-99，法律第 4 條）。
func TestRateLimitedPlatformResumeKeepsSender(t *testing.T) {
	f := dispatchSetup(t)
	f.e.Configure(fake.Behavior{Delivered: true, Recheck: true})
	first := f.send(t, &sdk.Envelope{From: "work", Kind: sdk.EnvelopeKindSay, Body: "幫我整理週報"})
	f.begin(t, first)
	until := f.h.Clock.Now().Add(30 * time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: until})
	f.want(t, first.ID, sdk.LifecycleQueued)
	f.h.Clock.Advance(until.Sub(f.h.Clock.Now()))
	for i := 0; i < 100 && f.get(t, first.ID).Lifecycle != sdk.LifecycleReceived; i++ {
		f.tick()
		time.Sleep(10 * time.Millisecond)
	}
	f.want(t, first.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "usage limit has reset, continue"})
	f.m.mu.Lock()
	got := f.m.entries["code"].turn
	last := f.m.entries["code"].lastOwner
	f.m.mu.Unlock()
	if got.Origin != "agent" || got.From != "work" || got.Envelope != first.ID {
		t.Fatalf("接著做的那一輪是 work 傳來的那句：%+v", got)
	}
	if !last.IsZero() {
		t.Fatalf("不是主人打字，不記 last_owner：%v", last)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "週報整理好了"})
	f.want(t, first.ID, sdk.LifecycleReplied)
}
