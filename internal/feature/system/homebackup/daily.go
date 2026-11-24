package homebackup

import (
	"context"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// restartRetryAfter：還原準備好之後這麼久服務還沒重開，就再請一次。
const restartRetryAfter = 5 * time.Minute

// made 是做好一份備份的結果。
type made struct {
	created *Created
}

// makeBackup 做一份備份、清舊的。備份只放在這台電腦（法律第 7 條；HBK-3）。
func (m *Module) makeBackup(home, kind string, now time.Time) (*made, error) {
	m.work.Lock()
	defer m.work.Unlock()
	if HasPending(home) {
		return nil, platform.Conflict("有一份還原在等服務重開，現在先不做備份")
	}
	c, err := Create(CreateOptions{Home: home, Kind: kind, Now: now})
	if err != nil {
		return nil, err
	}
	Prune(home, now)
	return &made{created: c}, nil
}

// tick 是定時工作：通知還原的結果、收掉沒用的上傳檔、照保留規則清舊備份（以前每天自動的那些全部收掉，HBK-2）。
// 不再每天自動做整個家的備份（2026-11-21 作者定：預設只留每次更新前的最新一份，system UPD 的規則）。
func (m *Module) tick(ctx context.Context, now time.Time) {
	home := m.homeDir()
	if home == "" {
		return
	}
	if _, err := platform.LoadHome(home); err != nil {
		return
	}
	m.notifyRestore(ctx, home)
	cleanUploads(home, now)
	// 有還原在等重開：先不動備份。等了超過 restartRetryAfter 還沒重開（重開的訊號當時沒送成），再請一次重開。
	if HasPending(home) {
		if p, err := loadPending(home); err == nil && p.Phase == phaseReady && now.Sub(p.StagedAt) > restartRetryAfter {
			m.closeSessions(ctx) // 等的這段時間可能又開了對話
			m.restart()
		}
		return
	}
	m.work.Lock()
	n := Prune(home, now)
	m.work.Unlock()
	if n > 0 {
		m.logger().Info("old backups pruned", "removed", n)
	}
}

// notifyRestore：上一次還原的結果還沒告訴主人就通知一次（還原是重開服務時換的，主人可能已經關掉那一頁）。
func (m *Module) notifyRestore(ctx context.Context, home string) {
	r := readResult(home)
	if r == nil || r.Notified {
		return
	}
	title, todo := "還原完成", ""
	if !r.OK {
		title = "還原沒有做成，你的家維持原樣"
	}
	if len(r.Todo) > 0 {
		todo = "還有幾件事要你做，列在「設定 → 電腦 → 備份」"
	}
	if !m.send(ctx, true, title, r.Message, todo) {
		return
	}
	r.Notified = true
	markNotified(home, r)
}

// send 發一則系統那道的通知；urgent：備份或還原沒做成主人要馬上知道。
// 沒有 notify 或送不出去回 false（下次再試）。
func (m *Module) send(ctx context.Context, urgent bool, title, detail, todo string) bool {
	if m.d.Notify == nil {
		return false
	}
	_, err := m.d.Notify.Send(ctx, &sdk.Notification{
		Category: sdk.CategoryDigest, Title: title, Context: detail, Todo: todo, URL: "/settings/machine",
		Agent: "system", Lane: sdk.NotifyLaneSystem, Urgent: urgent,
	})
	if err != nil {
		m.logger().Warn("backup notice not sent", "error", err)
		return false
	}
	return true
}
