package pkg

import (
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

var _ app.NoticeSourceProvider = (*Module)(nil)

// NoticeSources 登記認領職位的通知（source package，NTF-48）：回答由 packages 自己處理（claim.go 聽 notice.answered），
// 不轉進秘書的對話；認領通知沒有「那件事不在了就收」，所以沒有 Changed／Waiting。
func (m *Module) NoticeSources() []app.NoticeSource {
	return []app.NoticeSource{{Name: sdk.NotifySourcePackage}}
}
