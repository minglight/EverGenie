package agent

// PersonaSource 是名冊讀角色的唯一入口（personas-1）。
// 名冊（roster／manage／context／problems）要人設卡與說話層，一律經這個小介面，不直接碰角色庫的檔案與內部型別；
// 角色庫是業務模組 internal/feature/agents/persona（personas-3 搬走），由 registry 呼叫 SetPersonaSource 注入；沒接時一律回「角色庫還沒接上」。

import (
	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
)

// PersonaCardInfo 是名冊需要知道的一張人設卡（型別在 personaref，名冊與角色庫共用）。
type PersonaCardInfo = personaref.CardInfo

// PersonaSource 是名冊需要的最小方法。
type PersonaSource interface {
	// Card 讀一張人設卡（同代號照角色庫的順序：官方角色資料夾＞我的＞跟著程式的公開卡，agents 的 PRS-27；代號是 <套件id>.<slug> 時找套件）；不管有沒有上線。
	// 代號無效、卡不存在或讀不了回 invalid；缺 name 回 invalid（此時 info 仍帶讀到的內容）。
	Card(slug string) (PersonaCardInfo, error)
	// Style 回現在生效的說話層：強度、全文、自己的說話層（personas/_style.md）在不在。
	// 設了 custom 但檔案不見時退回 c 的全文，level 仍是 custom、customExists 是 false（doctor 據此報）。
	Style() (level, text string, customExists bool, err error)
}

// noPersonas 是沒接角色庫時的預設：讀不到任何卡（registry 一定會接 SetPersonaSource；沒接是組裝錯誤，講白話不 panic）。
type noPersonas struct{}

func (noPersonas) Card(string) (PersonaCardInfo, error) {
	return PersonaCardInfo{}, platform.Unavailable("角色庫還沒接上")
}

func (noPersonas) Style() (string, string, bool, error) {
	return "", "", false, platform.Unavailable("角色庫還沒接上")
}
