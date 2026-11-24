package notify

import (
	"context"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 通知跟狀態只有一份正本（DEV-49 S2、NTF-49）：訊息頁上「要你回覆」的通知都綁一件活的事；
// 那件事不在了，通知就收掉（封存，理由「已經不用回了」），清單、未讀、徽章一起少。
// 走事件（3 秒內）：業務模組登記的通知來源（app.NoticeSource）發它的 Changed 事件（例 goal.changed、policy.changed）時
// 問它的 Waiting；每 3 秒再對一次當安全網。系統提醒沒綁東西，主人按「知道了」或刪掉才收（S5 拿掉了「狀況恢復就收掉同標題」那條事件）；
// 例外是引擎回報的「要重新登入／帳單」：同一個帳號的對話又正常做完一輪（session.recovered），那則就收掉（S5-5）。
// 綁在對話上的（Room）由 session 判斷，經 Retire 收掉。

// RetireReason 是「那件事不在了」收掉通知時記在回答裡的理由（看板據此顯示「這件已經處理了」）。
const RetireReason = "已經不用回了"

// SweepEvery：安全網的對帳週期（作者 10-30「最低 3 秒鐘就要更新到最新狀態」；只讀 vault 的記憶體索引）。
const SweepEvery = 3 * time.Second

// unreferencedGrace：Goal、權限申請是先發通知、再把通知編號存回去；剛發出來還沒有人指到它的，等一下再判斷。
const unreferencedGrace = time.Minute

// Retire 把還沒回答的通知收掉（記回答 auto「reason」並封存）；已經回答、封存或找不到的略過。
// 給 session 這類知道「那件事還在不在」的模組用（綁在對話上的通知）。
func (m *Module) Retire(ctx context.Context, ids []string, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	for _, id := range ids {
		if err := m.retire(ctx, id, reason); err != nil {
			return err
		}
	}
	return nil
}

// retire：呼叫者持有 m.mu。先記成已回答（發 notice.answered，via auto：各模組都不當成主人的回答），再封存。
func (m *Module) retire(ctx context.Context, id, reason string) error {
	if reason == "" {
		reason = RetireReason
	}
	n, err := m.get(ctx, id)
	if err != nil {
		if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			return nil
		}
		return err
	}
	if m.records[id].Archived || n.State != sdk.NotificationStateUnread && n.State != sdk.NotificationStateRead {
		return nil
	}
	n, err = m.answer(ctx, id, reason, sdk.AnswerViaAuto, "system")
	if err != nil {
		return err
	}
	if err := m.d.Vault.ArchiveNow(ctx, "message", id, "system"); err != nil {
		return err
	}
	delete(m.records, id)
	m.changed(ctx, n)
	return nil
}

// unreadCount：選單「訊息」徽章與今天頁的未讀數（NTF-57）：秘書那道的未讀，加上系統那道還要主人回答的與 urgent 的；
// 討論串內提問（Inline）不算（DEV-49 S2）。規則在 countsInBadge（list 讀出來時已算進 Badge）。呼叫者持有 m.mu。
func (m *Module) unreadCount(ctx context.Context) (int, error) {
	ns, err := m.list(ctx, []sdk.NotificationState{sdk.NotificationStateUnread}, "", false)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range ns {
		if ns[i].Badge {
			n++
		}
	}
	return n, nil
}

// sweepOrphans：安全網，每 SweepEvery 對每個登記了 Waiting 的通知來源對一次（事件漏掉、重開、舊資料）。呼叫者持有 m.mu。
func (m *Module) sweepOrphans(ctx context.Context, now time.Time) {
	for _, src := range m.sources {
		if err := m.retireSettled(ctx, src, now); err != nil {
			m.warn("無法收掉已經不用回的通知")
		}
	}
}

// retireSettled：這個來源還沒回的確認通知，那件事已經不等主人了（有人指到、但不在 waiting 裡）就收掉；
// 沒有人指到的剛發出來（業務模組先發通知、再把編號存回去），等 unreferencedGrace 再判斷。
// 讀不到（Waiting 回錯）就不判斷，免得誤收。呼叫者持有 m.mu；Waiting 由業務模組保證不拿會回頭叫 notify 的鎖。
func (m *Module) retireSettled(ctx context.Context, src app.NoticeSource, now time.Time) error {
	if src.Waiting == nil {
		return nil
	}
	open, err := m.list(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
	if err != nil {
		return err
	}
	var owned []sdk.Notification
	for _, n := range open {
		if n.Category == sdk.CategoryApprove && n.Source == src.Name {
			owned = append(owned, n)
		}
	}
	if len(owned) == 0 {
		return nil
	}
	waiting, known, err := src.Waiting(ctx)
	if err != nil {
		return nil
	}
	for _, n := range owned {
		if waiting[n.ID] || !known[n.ID] && now.Sub(n.CreatedAt) < unreferencedGrace {
			continue
		}
		if err := m.retire(ctx, n.ID, RetireReason); err != nil {
			return err
		}
	}
	return nil
}
