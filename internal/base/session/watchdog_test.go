package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 秘書的設定改了（agent.changed）：帳號巡檢在下一次 Tick 就跑，不等 WatchdogEvery（30 秒）——設定頁換帳號，
// 畫面上她換到新帳號重開的時間是 1 秒內，不是 30 秒（SES-53）。沒有事件時照舊 30 秒一次。
// Guards: REQ-389
func TestAgentChangedRunsWatchdogNow(t *testing.T) {
	f := setupWith(t, "[accounts.second]\nengine=\"test-engine\"\n")
	f.open(t, "code")
	f.event(t, sdk.EngineEventTypeSessionStarted)
	f.state(t, sdk.SessionStateIdle)
	f.tick() // 這一輪巡檢過（記下時間）
	_, err := f.a.PatchAgent(context.Background(), "code", map[string]any{"account": "second"}, nil, "owner(web)")
	must(t, err)
	f.h.Clock.Advance(time.Second)
	f.tick()
	if n := len(f.startRequests()); n != 1 {
		t.Fatalf("沒有事件時照 WatchdogEvery 巡檢，1 秒後還不換：Start %d 次", n)
	}
	must(t, f.m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "agent.changed", Payload: map[string]any{"id": "code", "type": "updated"}}))
	f.h.Clock.Advance(time.Second)
	f.tick()
	rs := f.startRequests()
	if len(rs) != 2 || rs[1].Account != "second" {
		t.Fatalf("agent.changed 之後下一次 Tick 就換新帳號重開：%+v", rs)
	}
}

// 電腦設定（servers/*.toml）改了並套用了（node 模組發 servers.changed）：這台該開哪些秘書的主要 session 馬上重算，
// 不用重開 serve（DEV-53：秘書加到另一台電腦之後 hub 還以為在本機）。
// Guards: REQ-396
