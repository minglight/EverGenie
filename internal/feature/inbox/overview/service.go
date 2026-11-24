package overview

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

func unavailable(msg string) error {
	return &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: msg}
}

// computed 是 overview 自己算的欄位，業務模組不能登記。
const computed = "waiting_total"

// slot 是一段登記好的首頁資料：Key 對到總覽回應的哪個欄位（照 sdk.WebGetOverviewResponse 的 json 名稱）。
type slot struct {
	key   string
	field int
	load  func(ctx context.Context) (any, error)
}

// fields 是總覽回應每個欄位的 json 名稱 → 欄位位置。overview 不認得業務模組的業務，只認得 sdk 的回應長什麼樣。
func fields() map[string]int {
	t := reflect.TypeOf(sdk.WebGetOverviewResponse{})
	out := map[string]int{}
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out[name] = i
		}
	}
	return out
}

// ConsumeBoard 收下各業務模組登記的首頁資料段（app.BoardConsumer；app 組裝時呼叫）。Key 要是總覽回應的欄位名稱
// （json 名稱，例如 approvals、waiting_tasks）；不認得的 Key 或 overview 自己算的 waiting_total 都回錯，啟動前就擋下。
func (m *Module) ConsumeBoard(parts []app.BoardPart) error {
	byName := fields()
	out := make([]slot, 0, len(parts))
	for _, p := range parts {
		i, ok := byName[p.Key]
		if !ok || p.Key == computed {
			return fmt.Errorf("首頁資料段「%s」對不到總覽的欄位（要是 sdk.WebGetOverviewResponse 的 json 名稱，%s 由 overview 自己算）", p.Key, computed)
		}
		out = append(out, slot{key: p.Key, field: i, load: p.Load})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slots, m.ready = out, true
	return nil
}

// Overview 算出首頁總覽：各業務模組登記的資料段（要你同意的通知、等你處理的任務單、等你回覆的討論串、未讀數、今天的提醒…）
// 填進對應欄位，再算等你處理的總數（要你同意的通知 ＋ 等你處理的任務單，定案 9）。
// 任何一段讀取失敗，整個總覽就失敗；錯誤原樣回（sdk.Error 或底層錯誤），看板那邊轉成白話。還沒組裝就是「核心服務尚未啟動」。
func (m *Module) Overview(ctx context.Context) (*sdk.WebGetOverviewResponse, error) {
	m.mu.Lock()
	slots, ready := m.slots, m.ready
	m.mu.Unlock()
	if !ready {
		return nil, unavailable("核心服務尚未啟動")
	}
	out := &sdk.WebGetOverviewResponse{}
	rv := reflect.ValueOf(out).Elem()
	for _, s := range slots {
		v, err := s.load(ctx)
		if err != nil {
			return nil, err
		}
		if v == nil {
			continue
		}
		f := rv.Field(s.field)
		val := reflect.ValueOf(v)
		if !val.Type().AssignableTo(f.Type()) {
			return nil, fmt.Errorf("首頁資料段「%s」回的型別是 %s，總覽要 %s", s.key, val.Type(), f.Type())
		}
		f.Set(val)
	}
	out.WaitingTotal = int32(len(out.Approvals) + len(out.WaitingTasks))
	return out, nil
}
