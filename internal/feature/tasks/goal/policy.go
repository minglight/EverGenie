package goal

import (
	"fmt"
	"slices"

	"github.com/minglight/evergenie/sdk"
)

func validClass(c sdk.ActionClass) bool {
	switch c {
	case sdk.ActionClassRead, sdk.ActionClassWrite, sdk.ActionClassCommit, sdk.ActionClassPush, sdk.ActionClassDelete, sdk.ActionClassSendExternal, sdk.ActionClassSpend, sdk.ActionClassSettings:
		return true
	}
	return false
}
func sender(from string) string {
	if from == "" {
		return "別的秘書"
	}
	return from
}

// Decide uses the policy loaded at Start; it performs no I/O and never changes it.
func (m *Module) Decide(origin, from string, class sdk.ActionClass) (sdk.ActionVerdict, string) {
	m.mu.Lock()
	policy := append([]sdk.ActionClass(nil), m.alwaysAsk...)
	m.mu.Unlock()
	return decide(policy, origin, from, class)
}

// decide：主人的回合照常；秘書傳來的話就跟打字給她一樣照常做，只有「一律問你」的動作要問主人
// （法律第 4 條 v1.4，2026-11-01 作者：傳話不掛 Goal、不標注傳話）。
func decide(alwaysAsk []sdk.ActionClass, origin, from string, class sdk.ActionClass) (sdk.ActionVerdict, string) {
	if !validClass(class) {
		return sdk.ActionVerdictDeny, "動作分類不正確，請確認後重試"
	}
	if origin == "owner" {
		return sdk.ActionVerdictAllow, ""
	}
	if origin == "agent" {
		if !slices.Contains(alwaysAsk, class) {
			return sdk.ActionVerdictAllow, ""
		}
		return sdk.ActionVerdictAsk, "這個動作要先問主人（一律問你）"
	}
	if class == sdk.ActionClassRead {
		return sdk.ActionVerdictAllow, ""
	}
	return sdk.ActionVerdictDeny, fmt.Sprintf("［核心］認不出這一輪是誰觸發的（%s），只能查和回答，這個動作先不做；這個限制只針對這一則。", sender(from))
}
