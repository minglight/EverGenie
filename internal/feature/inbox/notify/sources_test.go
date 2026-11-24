package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// testSources 模擬 goals、policy、packages 登記的通知來源（真的登記在各業務模組的 NoticeSources()）：
// Goal 是草稿、權限申請待審時那則通知還在等；認領職位只標 source、不收。
func testSources(v *vault.Module) []app.NoticeSource {
	waiting := func(kind, field, live string) func(context.Context) (map[string]bool, map[string]bool, error) {
		return func(ctx context.Context) (map[string]bool, map[string]bool, error) {
			rows, err := v.List(ctx, kind, vault.Query{})
			if err != nil {
				return nil, nil, err
			}
			w, k := map[string]bool{}, map[string]bool{}
			for _, r := range rows {
				id, _ := r.Fields["notice"].(string)
				id = strings.TrimSuffix(strings.TrimPrefix(id, "[["), "]]")
				if id == "" {
					continue
				}
				k[id] = true
				if s, _ := r.Fields[field].(string); s == live {
					w[id] = true
				}
			}
			return w, k, nil
		}
	}
	return []app.NoticeSource{
		{Name: sdk.NotifySourceGoals, Changed: "goal.changed", Waiting: waiting("goal", "state", string(sdk.GoalStateDraft))},
		{Name: sdk.NotifySourcePolicy, Changed: "policy.changed", Waiting: waiting("policy_request", "status", string(sdk.PolicyStatusPending))},
		{Name: sdk.NotifySourcePackage},
	}
}

// 只有登記過的來源留得住 source；notice.answered 的 relay 只有一般要轉回對話的回答是 true（NTF-48，2026-11-01 起由登記決定）。
func TestRegisteredSourcesDecideOwnershipAndRelay(t *testing.T) {
	m, h, b, _ := fixture(t)
	linked(t, h, "task", "SEC-1", "waiting_user")
	owned := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "確認主人的 Goal：x", Todo: "請確認", Agent: "work", Source: sdk.NotifySourceGoals})
	stray := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "亂標的", Todo: "請確認", Agent: "work", Source: "nobody"})
	room := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "要不要", Todo: "請確認", Agent: "code", Room: "code"})
	task := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "單在等你", Todo: "請回", Agent: "code", Task: "SEC-1"})
	// 等你決定的任務問句（typed，TSK-27）：回答的內容要送回秘書（D-57），轉（DEV-156 U-05）。
	asked := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "要日式還是義式？", Todo: "要日式還是義式？", Typed: true, Agent: "code", Task: "SEC-1"})
	if owned.Source != sdk.NotifySourceGoals || stray.Source != "" {
		t.Fatalf("登記過的留著、沒登記的清掉：%q %q", owned.Source, stray.Source)
	}
	for _, n := range []*sdk.Notification{owned, stray, room, task, asked} {
		if _, err := m.Answer(ctx, n.ID, "好", sdk.AnswerViaButton, "owner(web)"); err != nil {
			t.Fatal(err)
		}
	}
	relay := map[any]any{}
	for _, ev := range b.published("notice.answered") {
		relay[ev.Payload["id"]] = ev.Payload["relay"]
	}
	if relay[owned.ID] != false || relay[stray.ID] != true || relay[room.ID] != true || relay[task.ID] != false || relay[asked.ID] != true {
		t.Fatalf("relay：%v", relay)
	}
}
