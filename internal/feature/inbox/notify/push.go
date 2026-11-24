package notify

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/inbox/push"
	"github.com/minglight/evergenie/sdk"
)

// 傳送與端點管理在 internal/feature/inbox/push；這裡只放通知這一側的規則：誰能管裝置、測試通知長什麼樣、沙盒家不推、開發家的標題加前綴。

// subscriptions 讀目前的訂閱端點。推播通道（Deps.Push）一定有：沒接的話 Start 就報錯，不會走到這裡。
func (m *Module) subscriptions() ([]push.Subscription, error) {
	return m.d.Push.Subscriptions()
}

// pushFailure 是推播失敗時可以寫進 log、回給呼叫者的一句話：只有 platform 錯誤是整理過的白話（通道的契約），
// 其他錯誤（注入的轉接器可能夾帶任何輸入）一律換成固定的句子。
func pushFailure(err error) string {
	var pe *platform.Err
	if errors.As(err, &pe) {
		return pe.Message
	}
	return "推播送出失敗"
}

// requireOwner：推播裝置只有使用者本人能管理（REQ-28）；秘書經 API 呼叫會被擋。
func requireOwner(by string) error {
	if !platform.IsOwner(by) {
		return platform.Forbidden("只有主人能管理推播裝置")
	}
	return nil
}

// pushTitle：開發家（[runtime] env = "dev"）推播的標題前加「開發｜」，手機上一眼分得出是哪個家；只改推出去的那一份，看板與存檔的標題不變。
func (m *Module) pushTitle(title string) string {
	if m.config != nil && m.config.IsDev() {
		return "開發｜" + title
	}
	return title
}

// homeLabel 是推播內容的 home：這台的標籤（platform 的 Label()），看板連多台時 sw.js 照它開到那台（S3）。
func (m *Module) homeLabel() string {
	if m.config == nil {
		return ""
	}
	return m.config.Label()
}

// PushKey 回 Web Push 的公開金鑰與目前的訂閱數。金鑰沒設定（或壞了）一律回同一句白話，不帶欄位名。
func (m *Module) PushKey(ctx context.Context) (*sdk.NotifyPushKeyResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	key, err := m.d.Push.PublicKey()
	if err != nil {
		return nil, platform.Unavailable(pushKeyMissing)
	}
	subs, err := m.subscriptions()
	if err != nil {
		return nil, err
	}
	return &sdk.NotifyPushKeyResponse{PublicKey: key, Subscriptions: len(subs)}, nil
}

// SubscribePush 照 endpoint 新增或更新這台裝置的訂閱，回目前的訂閱數。
func (m *Module) SubscribePush(ctx context.Context, endpoint, p256dh, auth, by string) (int, error) {
	sub := push.Subscription{Endpoint: endpoint, P256dh: p256dh, Auth: auth}
	if err := requireOwner(by); err != nil {
		return 0, err
	}
	if err := push.Validate(sub); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return 0, err
	}
	return m.d.Push.Subscribe(sub)
}

// UnsubscribePush 照 endpoint 刪掉訂閱；沒有這筆也算成功。刪到 0 筆就寫空檔（不刪檔）。
func (m *Module) UnsubscribePush(ctx context.Context, endpoint, by string) (int, error) {
	if err := requireOwner(by); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return 0, err
	}
	return m.d.Push.Unsubscribe(endpoint)
}

// PushTest 只送一則測試通知給 endpoint 相同的那台裝置：不存檔、不寫投遞紀錄、不發 bus 事件。
func (m *Module) PushTest(ctx context.Context, endpoint, by string) error {
	if err := requireOwner(by); err != nil {
		return err
	}
	m.mu.Lock()
	if err := m.ready(ctx); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.config.Runtime.Sandbox {
		m.mu.Unlock()
		return platform.Unavailable("這是沙盒家（開發用），不推播")
	}

	subs, err := m.subscriptions()
	channel := m.d.Push
	m.mu.Unlock()
	if err != nil {
		return err
	}
	var target *push.Subscription
	for i := range subs {
		if subs[i].Endpoint == endpoint {
			target = &subs[i]
			break
		}
	}
	if target == nil {
		return platform.NotFound("這台裝置還沒開啟推播")
	}
	payload, err := json.Marshal(&sdk.Notification{ID: "test", Category: sdk.CategoryDone, Title: m.pushTitle("測試通知"), Context: "這台裝置收得到推播", Home: m.homeLabel()})
	if err != nil {
		return platform.Internal("通知無法編碼")
	}
	if err := channel.Send(ctx, *target, payload); err != nil {
		var pe *platform.Err
		if errors.As(err, &pe) {
			return err
		}
		return platform.Unavailable(pushFailure(err))
	}
	return nil
}
