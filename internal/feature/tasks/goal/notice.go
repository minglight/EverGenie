package goal

import (
	"context"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

var _ app.NoticeSourceProvider = (*Module)(nil)

// NoticeSources 登記 Goal 確認通知（source goals）：回答由 goals 自己處理（聽 notice.answered），不轉進秘書的對話；
// Goal 不再是草稿（goal.changed）時 notify 問 waiting，把不用回的確認通知收掉（NTF-49）。
func (m *Module) NoticeSources() []app.NoticeSource {
	return []app.NoticeSource{{Name: sdk.NotifySourceGoals, Changed: "goal.changed", Waiting: m.waitingNotices}}
}

// waitingNotices：每個 Goal 指到的確認通知（known），還是草稿的那些在等主人（waiting）。
// notify 持有自己的鎖時呼叫，所以只讀 vault、不拿 m.mu（goals 拿著 m.mu 時會叫 notify.Send）。
func (m *Module) waitingNotices(ctx context.Context) (waiting, known map[string]bool, err error) {
	rows, err := m.d.Vault.List(ctx, "goal", vault.Query{})
	if err != nil {
		return nil, nil, err
	}
	waiting, known = map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		id := strings.TrimSuffix(strings.TrimPrefix(field(r, "notice"), "[["), "]]")
		if id == "" {
			continue
		}
		known[id] = true
		if field(r, "state") == string(sdk.GoalStateDraft) {
			waiting[id] = true
		}
	}
	return waiting, known, nil
}
