package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 引擎回報的事實照發給業務模組（platform/session_events.go）：TURN_FAILED → session.turn_failed（種類 Normalize 過、平台原話）；
// 之後第一次正常結束 → session.recovered（只一次）；SESSION_ENDED → session.ended（附原因）。要不要推給主人由 notify 決定。
func TestEngineFactsPublished(t *testing.T) {
	f := dispatchSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	facts := make(chan *sdk.BusSubscribeResponse, 16)
	for _, topic := range []string{platform.TopicSessionTurnFailed, platform.TopicSessionRecovered, platform.TopicSessionEnded} {
		ch, err := f.b.Subscribe(ctx, topic)
		must(t, err)
		go func() {
			for ev := range ch {
				facts <- ev
			}
		}()
	}
	next := func(want string) map[string]any {
		t.Helper()
		select {
		case ev := <-facts:
			if ev.Topic != want {
				t.Fatalf("要 %s，收到 %s %+v", want, ev.Topic, ev.Payload)
			}
			return ev.Payload
		case <-time.After(2 * time.Second):
			t.Fatalf("等不到 %s", want)
		}
		return nil
	}
	none := func() {
		t.Helper()
		select {
		case ev := <-facts:
			t.Fatalf("不該再發：%s %+v", ev.Topic, ev.Payload)
		case <-time.After(100 * time.Millisecond):
		}
	}
	e := f.send(t, ownerChat("整理一下信件"))
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnFailed, Error: sdk.EngineErrorKindAuth, LastMessage: "Please run /login"})
	p := next(platform.TopicSessionTurnFailed)
	if p["name"] != "code" || p["agent"] != "code" || p["error"] != string(sdk.EngineErrorKindAuth) || p["last_message"] != "Please run /login" {
		t.Fatalf("turn_failed 的內容：%+v", p)
	}
	if _, ok := p["account"]; !ok {
		t.Fatalf("turn_failed 要帶帳號欄位：%+v", p)
	}
	// 帶上這位秘書現在的角色名（通知要寫出是誰）。
	if name, _ := p["agent_name"].(string); name == "" {
		t.Fatalf("turn_failed 要帶秘書的角色名：%+v", p)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	if p := next(platform.TopicSessionRecovered); p["name"] != "code" || p["after"] != string(sdk.EngineErrorKindAuth) {
		t.Fatalf("recovered 的內容：%+v", p)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	none()
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeSessionEnded, Reason: sdk.SessionEndReasonLoggedOut})
	if p := next(platform.TopicSessionEnded); p["name"] != "code" || p["reason"] != sdk.SessionEndReasonLoggedOut {
		t.Fatalf("ended 的內容：%+v", p)
	}
	// 登出後重新登入、第一輪正常做完：也算從登入失效恢復（notify 才收得掉「要重新登入」）。
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	if p := next(platform.TopicSessionRecovered); p["after"] != string(sdk.EngineErrorKindAuth) {
		t.Fatalf("登出後恢復要帶 after=auth：%+v", p)
	}
	// 先登入失效、又碰到平台忙，恢復時 after 仍是 auth（要主人出手的那種優先記著）。
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnFailed, Error: sdk.EngineErrorKindAuth})
	next(platform.TopicSessionTurnFailed)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnFailed, Error: sdk.EngineErrorKindOverloaded})
	if p := next(platform.TopicSessionTurnFailed); p["error"] != string(sdk.EngineErrorKindOverloaded) {
		t.Fatalf("turn_failed 照這一次的種類報：%+v", p)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	if p := next(platform.TopicSessionRecovered); p["after"] != string(sdk.EngineErrorKindAuth) {
		t.Fatalf("auth 之後又 overloaded，恢復時 after 仍是 auth：%+v", p)
	}
}
