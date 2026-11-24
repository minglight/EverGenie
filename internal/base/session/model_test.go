package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 這段對話現在用的模型與思考力度（SES-60、D-46）：開的時候照設定，引擎說得出就換成引擎的，主人的 /model、/effort 做成了馬上換；
// 核心只存給它的字，不知道就空著。

func (f *fixture) session(t *testing.T) *sdk.Session {
	t.Helper()
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	return s
}

// withAgentModel 改這位秘書的模型與力度設定（空字串＝沒寫）。
func withAgentModel(t *testing.T, f *fixture, model, effort string) {
	t.Helper()
	ctx := context.Background()
	cur, err := f.h.Vault.Get(ctx, "agent", "code")
	must(t, err)
	for k, v := range map[string]string{"model": model, "effort": effort} {
		if v == "" {
			delete(cur.Fields, k)
		} else {
			cur.Fields[k] = v
		}
	}
	_, err = f.h.Vault.Put(ctx, cur, "system", cur.Revision)
	must(t, err)
}

func wantModel(t *testing.T, s *sdk.Session, model, label, effort string) {
	t.Helper()
	if s.Model != model || s.ModelLabel != label || s.Effort != effort {
		t.Fatalf("model=%q label=%q effort=%q，要 %q %q %q", s.Model, s.ModelLabel, s.Effort, model, label, effort)
	}
}

func TestSessionModelStartsFromLaunchConfigAndEngineLabel(t *testing.T) {
	f := setup(t)
	withAgentModel(t, f, "sonnet", "high")
	f.open(t, "code")
	// 設定交給引擎、引擎回報它開起來用的（假引擎照設定、給名字「Fake Sonnet」）。
	reqs := f.startRequests()
	if len(reqs) == 0 || reqs[len(reqs)-1].Model != "sonnet" || reqs[len(reqs)-1].Effort != "high" {
		t.Fatalf("開的時候要帶設定：%+v", reqs)
	}
	wantModel(t, f.session(t), "sonnet", "Fake Sonnet", "high")
}

func TestSessionModelUnknownPartsStayEmpty(t *testing.T) {
	f := setup(t)
	withAgentModel(t, f, "", "") // 模型沿用名冊的預設，力度沒寫
	f.e.Configure(fake.Behavior{Delivered: true, Unlabeled: true})
	f.open(t, "code")
	// 力度沒設定、引擎也說不出：空的（看板不畫）；引擎沒給名字：名字空的（看板顯示 model 原字）。不猜。
	s := f.session(t)
	if s.Effort != "" || s.ModelLabel != "" {
		t.Fatalf("不知道的要空著：label=%q effort=%q", s.ModelLabel, s.Effort)
	}
}

func TestSessionModelCommandUpdatesImmediately(t *testing.T) {
	f := idleSession(t)
	ctx := context.Background()
	_, err := f.m.Command(ctx, "code", sdk.SessionCommandModel, "opus", "owner(web)")
	must(t, err)
	wantModel(t, f.session(t), "opus", "Fake Opus", "")
	_, err = f.m.Command(ctx, "code", sdk.SessionCommandEffort, "max", "owner(web)")
	must(t, err)
	wantModel(t, f.session(t), "opus", "Fake Opus", "max")
}

func TestSessionModelCommandWithoutEngineLabelUsesArgAndDropsOldLabel(t *testing.T) {
	f := setup(t)
	withAgentModel(t, f, "sonnet", "")
	f.open(t, "code")
	f.event(t, sdk.EngineEventTypeSessionStarted)
	wantModel(t, f.session(t), "sonnet", "Fake Sonnet", "")
	f.e.Configure(fake.Behavior{Delivered: true, Unlabeled: true})
	_, err := f.m.Command(context.Background(), "code", sdk.SessionCommandModel, "haiku", "owner(web)")
	must(t, err)
	// 引擎沒給名字：用這次的參數，舊的名字（Fake Sonnet）不能留著配上新的模型。
	wantModel(t, f.session(t), "haiku", "", "")
}

func TestSessionModelFailedCommandKeepsOldValues(t *testing.T) {
	f := setup(t)
	withAgentModel(t, f, "sonnet", "low")
	f.open(t, "code")
	f.event(t, sdk.EngineEventTypeSessionStarted)
	f.e.Configure(fake.Behavior{Delivered: true, CommandReason: sdk.CommandBadArg})
	if _, err := f.m.Command(context.Background(), "code", sdk.SessionCommandModel, "gpt", "owner(web)"); err == nil {
		t.Fatal("引擎說沒有這個選項要回錯")
	}
	wantModel(t, f.session(t), "sonnet", "Fake Sonnet", "low")
}

func TestSessionModelFollowsEngineEvents(t *testing.T) {
	f := setup(t)
	withAgentModel(t, f, "opus", "high")
	f.open(t, "code")
	ctx := context.Background()
	report := func(ev sdk.EngineEvent) {
		t.Helper()
		ev.Session, ev.At = "code", f.h.Clock.Now()
		must(t, f.m.ReportEvent(ctx, &ev))
	}
	// 引擎看到這一輪實際用的模型（帳號的預設在別的對話被改過時，跟設定不一樣）：換成引擎的。
	report(sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, Model: "vendor-opus-9", ModelLabel: "Opus 9"})
	wantModel(t, f.session(t), "vendor-opus-9", "Opus 9", "high")
	// 沒帶的事件不改。
	report(sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted})
	wantModel(t, f.session(t), "vendor-opus-9", "Opus 9", "high")
	// 只帶力度：只換力度。
	report(sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, Effort: "xhigh"})
	wantModel(t, f.session(t), "vendor-opus-9", "Opus 9", "xhigh")
	// 換了模型沒給名字：名字清掉（看板顯示原字）。
	report(sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, Model: "vendor-sonnet-9"})
	wantModel(t, f.session(t), "vendor-sonnet-9", "", "xhigh")
}

func TestSessionModelChangeReachesWatchers(t *testing.T) {
	f := idleSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := f.m.Watch(ctx, "code")
	must(t, err)
	_, err = f.m.Command(context.Background(), "code", sdk.SessionCommandModel, "opus", "owner(web)")
	must(t, err)
	// 看板靠 session.watch 的推送更新標題欄（3 秒規則）：指令做成了就要推出去。
	for {
		select {
		case r := <-ch:
			if r != nil && r.Session.Model == "opus" && r.Session.ModelLabel == "Fake Opus" {
				return
			}
		case <-ctx.Done():
			t.Fatal("換了模型沒有推給看板")
		}
	}
}

func TestSessionModelPersistsAndClearedValuesDoNotComeBack(t *testing.T) {
	f := setup(t)
	withAgentModel(t, f, "sonnet", "high")
	f.open(t, "code")
	f.event(t, sdk.EngineEventTypeSessionStarted)
	must(t, f.m.flushDirty())
	r := readState(t, f)
	if text(r.Fields, "model") != "sonnet" || text(r.Fields, "model_label") != "Fake Sonnet" || text(r.Fields, "effort") != "high" {
		t.Fatalf("狀態檔要記下來：%+v", r.Fields)
	}
	// 換了模型、引擎沒給名字：檔裡舊的名字也要拿掉（不然重開核心又讀回「Fake Sonnet」）。
	ev := sdk.EngineEvent{Session: "code", Type: sdk.EngineEventTypeTurnEnded, At: f.h.Clock.Now(), Model: "haiku"}
	must(t, f.m.ReportEvent(context.Background(), &ev))
	f.m.mu.Lock()
	data, err := f.m.encodeState(f.m.entries["code"])
	f.m.mu.Unlock()
	must(t, err)
	got, _, err := vault.Decode(data)
	must(t, err)
	if text(got.Fields, "model") != "haiku" || got.Fields["model_label"] != nil {
		t.Fatalf("model=%v model_label=%v", got.Fields["model"], got.Fields["model_label"])
	}
}
