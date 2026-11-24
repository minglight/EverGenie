package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

type fakeSessions struct {
	list []*sdk.Session
	err  error
}

func (f fakeSessions) List(context.Context, string, []sdk.SessionState) ([]*sdk.Session, error) {
	return f.list, f.err
}

type fakeHealthRoster struct {
	list  []*sdk.Record
	names map[string]string
}

func (f fakeHealthRoster) List(context.Context) ([]*sdk.Record, error) { return f.list, nil }
func (f fakeHealthRoster) PersonaName(_ context.Context, id string) (string, error) {
	if n, ok := f.names[id]; ok {
		return n, nil
	}
	return "", errors.New("沒有人設")
}

func healthRoster() fakeHealthRoster {
	rec := func(id string, f map[string]any) *sdk.Record { return &sdk.Record{ID: id, Fields: f} }
	return fakeHealthRoster{
		list: []*sdk.Record{
			rec("life", map[string]any{"enabled": true, "mode": "resident"}),
			rec("code", map[string]any{"enabled": true}),
			rec("fitness", map[string]any{"enabled": false, "mode": "resident"}),
			rec("art", map[string]any{"enabled": true, "mode": "on_demand"}),
		},
		names: map[string]string{"code": "Builder", "life": "Helper", "fitness": "Coach"},
	}
}

func pushCheck(status app.Status, msg string) app.Check {
	return app.Check{Name: "push.status", Run: func(context.Context) app.CheckResult { return app.CheckResult{Status: status, Message: msg} }}
}

func healthOf(t *testing.T, m *Module) []sdk.HealthItem {
	t.Helper()
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	code, raw := postJSON(t, mux, "/api/web/get_health", struct{}{})
	var res sdk.WebGetHealthResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil {
		t.Fatalf("get_health：%d %s", code, raw)
	}
	return res.Items
}

// 看板的健康頁（/api/web/get_health，由 doctor 登記）：看板、核心服務、推播排前面，接著每位啟用的常駐秘書（人設名、白話狀態，依 id 排）；
// 停用的、不是常駐的不列。
// Guards: REQ-110
func TestHealthItems(t *testing.T) {
	m := New(Deps{Roster: healthRoster(), Sessions: fakeSessions{list: []*sdk.Session{
		{Name: "code", Agent: "code", Kind: sdk.SessionKindResident, State: sdk.SessionState("stuck")}, // S5 拿掉的狀態：說「狀態不明」，不說「卡住」
		{Name: "life", Agent: "life", Kind: sdk.SessionKindResident, State: sdk.SessionStateIdle},
	}}})
	consume(t, m, stubChecks(pushCheck(app.StatusOK, "2 台裝置")))
	items := healthOf(t, m)
	names := []string{}
	for _, it := range items {
		names = append(names, it.Name)
	}
	if len(items) != 5 || items[0].Name != "看板" || !items[0].Ok || items[1].Name != "核心服務" || !items[1].Ok || items[2].Name != "推播" {
		t.Fatalf("items = %v %+v", names, items)
	}
	if b := items[3]; b.Name != "Builder" || b.Ok || b.Detail != "狀態不明" {
		t.Fatalf("code = %+v", b)
	}
	if h := items[4]; h.Name != "Helper" || !h.Ok || h.Detail != "待命中" {
		t.Fatalf("life = %+v", h)
	}
}

// 推播一項照 doctor 的 push 群組（D-14）；核心連不上時推播與每位秘書都說「無法確認」，但整個請求成功。
// Guards: REQ-110
func TestHealthPushItem(t *testing.T) {
	t.Run("推播正常", func(t *testing.T) {
		m := New(Deps{Roster: healthRoster(), Sessions: fakeSessions{}})
		consume(t, m, stubChecks(pushCheck(app.StatusOK, "2 台裝置")))
		if it := healthOf(t, m)[2]; it.Name != "推播" || !it.Ok || it.Detail != "2 台裝置" {
			t.Fatalf("item = %+v", it)
		}
	})
	t.Run("還沒有裝置", func(t *testing.T) {
		m := New(Deps{Roster: healthRoster(), Sessions: fakeSessions{}})
		consume(t, m, stubChecks(pushCheck(app.StatusWarn, "還沒有裝置開啟推播")))
		if it := healthOf(t, m)[2]; it.Ok || it.Detail != "還沒有裝置開啟推播" {
			t.Fatalf("item = %+v", it)
		}
	})
	t.Run("核心連不上", func(t *testing.T) {
		m := New(Deps{Roster: healthRoster(), Sessions: fakeSessions{err: errors.New("連不上")}})
		consume(t, m, stubChecks(pushCheck(app.StatusOK, "2 台裝置")))
		items := healthOf(t, m)
		if items[1].Ok || items[2].Ok || items[2].Detail != "核心服務連不上，無法確認" {
			t.Fatalf("items = %+v", items)
		}
		for _, it := range items[3:] {
			if it.Ok || it.Detail != "核心服務連不上，無法確認" {
				t.Fatalf("秘書 = %+v", it)
			}
		}
	})
}

type stuckSessions struct {
	fakeSessions
	stuck []platform.SessionStuck
}

func (s stuckSessions) Stuck(context.Context) []platform.SessionStuck { return s.stuck }

// 話送不進去的秘書（DEV-162）：狀態是「待命中」也不算正常，寫出幾則、多久（她任何一個對話卡住都算）；別位照舊。
func TestHealthMarksSecretaryWhoseMessagesAreStuck(t *testing.T) {
	m := New(Deps{Roster: healthRoster(), Sessions: stuckSessions{
		fakeSessions: fakeSessions{list: []*sdk.Session{
			{Name: "code", Agent: "code", Kind: sdk.SessionKindResident, State: sdk.SessionStateIdle},
			{Name: "life", Agent: "life", Kind: sdk.SessionKindResident, State: sdk.SessionStateIdle},
		}},
		stuck: []platform.SessionStuck{{Name: "code", Agent: "code", Count: 2, Minutes: 14}, {Name: "code-sec-1", Agent: "code", Count: 1, Minutes: 15}},
	}})
	consume(t, m, stubChecks(pushCheck(app.StatusOK, "2 台裝置")))
	items := healthOf(t, m)
	if b := items[3]; b.Name != "Builder" || b.Ok || b.Detail != "有 3 則話送不進去，已經等了 15 分鐘；請重開她的對話" {
		t.Fatalf("code = %+v", b)
	}
	if h := items[4]; h.Name != "Helper" || !h.Ok || h.Detail != "待命中" {
		t.Fatalf("life = %+v", h)
	}
}
