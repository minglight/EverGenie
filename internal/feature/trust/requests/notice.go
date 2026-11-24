package requests

import (
	"context"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

var _ app.NoticeSourceProvider = (*Module)(nil)

// NoticeSources 登記權限申請的審核通知（source policy）：回答由 policy 自己處理，不轉進秘書的對話；
// 申請不再待審（policy.changed）時 notify 問 waiting，把不用回的通知收掉（NTF-49）。
func (m *Module) NoticeSources() []app.NoticeSource {
	return []app.NoticeSource{{Name: sdk.NotifySourcePolicy, Changed: "policy.changed", Waiting: m.waitingNotices}}
}

// waitingNotices：每張申請指到的通知（known），還在待審的那些在等主人（waiting）。
// notify 持有自己的鎖時呼叫，所以只讀 vault、不拿 m.mu（policy 拿著 m.mu 時會叫 notify.Send）。
func (m *Module) waitingNotices(ctx context.Context) (waiting, known map[string]bool, err error) {
	rows, err := m.d.Vault.List(ctx, kind, vault.Query{})
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
		if field(r, "status") == string(sdk.PolicyStatusPending) {
			waiting[id] = true
		}
	}
	return waiting, known, nil
}
