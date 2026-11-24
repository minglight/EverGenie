package app

import (
	"context"
	"fmt"
)

// NoticeSource 是一種「由業務模組自己處理回答」的通知來源（Goal 確認、權限申請、認領職位…）。業務模組在 Send 時把
// 通知的 source 標成 Name；notify 只認登記過的 Name（API 的 Send 不帶 source，秘書標不了），這種通知的回答
// 不轉進秘書的對話、不追蹤送達（session 看 notice.answered 的 relay）。
//
// 有「那件事還在等主人」概念的來源再給 Changed 與 Waiting：notify 收到 Changed 這個 bus 事件、以及每隔幾秒的
// 安全網，都問 Waiting 一次，把這個來源還沒回、但那件事已經不等了的通知收掉（NTF-49）。
type NoticeSource struct {
	Name    string
	Changed string // 那件事變了時業務模組發的 bus 事件（例 goal.changed）；空字串＝不靠事件
	// Waiting 回這個來源的資料指到的通知：known 是有人指到的（不論狀態），waiting 是那件事還在等主人的。
	// 沒給就不收（只標 source）。讀不到回錯，notify 就這次不判斷，免得誤收。
	Waiting func(ctx context.Context) (waiting, known map[string]bool, err error)
}

// NoticeSourceProvider 由自己處理通知回答的業務模組實作。
type NoticeSourceProvider interface {
	NoticeSources() []NoticeSource
}

// NoticeConsumer 由收通知來源的模組（notify）實作：Assemble 把合併後的 NoticeSources() 交給它（在 Start 之前）。
type NoticeConsumer interface {
	ConsumeNoticeSources(sources []NoticeSource) error
}

// NoticeSources 收集所有 NoticeSourceProvider 登記的通知來源（依模組順序）；Name 空的或重複就回錯。
func (a *App) NoticeSources() ([]NoticeSource, error) {
	var all []NoticeSource
	owner := map[string]string{}
	for _, m := range a.mods {
		p, ok := m.(NoticeSourceProvider)
		if !ok {
			continue
		}
		for _, s := range p.NoticeSources() {
			if s.Name == "" {
				return nil, fmt.Errorf("模組 %s 登記了沒有名稱的通知來源", m.Name())
			}
			if prev, dup := owner[s.Name]; dup {
				return nil, fmt.Errorf("通知來源 %s 重複登記：%s、%s", s.Name, prev, m.Name())
			}
			owner[s.Name] = m.Name()
			all = append(all, s)
		}
	}
	return all, nil
}
