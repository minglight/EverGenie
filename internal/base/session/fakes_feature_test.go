package session

import (
	"fmt"
	"slices"
	"sync"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 這個檔放業務模組（trust、goals）的假物件：registry 正式接的是 internal/feature/trust/rules、internal/feature/tasks/goal，
// 底層 session 的測試不 import 業務模組（depcheck 擋，測試檔也算）。假物件只做 session 測試需要的最小規則；
// 業務模組自己的規則在業務模組自己的測試。

// testTrust 實作 TrustLevels 與 DenialRecorder：等級照 trust.toml 的 [agent.<id>]、default_level，最後是內建的 full；
// 被擋的動作記在記憶體。
type testTrust struct {
	mu     sync.Mutex
	denied []sdk.DeniedAction
}

func newTestTrust() *testTrust { return &testTrust{} }

func (*testTrust) LevelFor(t platform.Trust, agent string) sdk.PermissionLevel {
	if a, ok := t.Agents[agent]; ok {
		if l, ok := platform.ParseLevel(a.Level); ok {
			return l
		}
	}
	if l, ok := platform.ParseLevel(t.DefaultLevel); ok {
		return l
	}
	return sdk.PermissionLevelFull
}

func (f *testTrust) AppendDenied(_ string, d sdk.DeniedAction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.denied = append(f.denied, d)
	return nil
}

func (f *testTrust) Denied() []sdk.DeniedAction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sdk.DeniedAction(nil), f.denied...)
}

// testGoals 實作 GoalsAccess，跟 goals 業務模組同一個規則：主人的回合照常；秘書傳來的話照常，
// 「一律問你」的動作（這裡只有 push）要問主人；認不出是誰的那一輪只能讀（2026-11-01：傳話不掛 Goal）。
type testGoals struct {
	vault VaultStore
	home  string
}

// goalAlwaysAsk 是秘書傳來的話仍一律問主人的動作。
var goalAlwaysAsk = []sdk.ActionClass{sdk.ActionClassPush}

func (*testGoals) Decide(origin, from string, class sdk.ActionClass) (sdk.ActionVerdict, string) {
	if origin == "owner" {
		return sdk.ActionVerdictAllow, ""
	}
	if origin == "agent" {
		if slices.Contains(goalAlwaysAsk, class) {
			return sdk.ActionVerdictAsk, "這個動作要先問主人（一律問你）"
		}
		return sdk.ActionVerdictAllow, ""
	}
	if class == sdk.ActionClassRead {
		return sdk.ActionVerdictAllow, ""
	}
	return sdk.ActionVerdictDeny, fmt.Sprintf("認不出這一輪是誰觸發的（%s）；這個限制只針對這一則。", from)
}

// goalSpec 是測試家裡 goal 資料種類的最小 schema（正式的由 goals 業務模組啟動時註冊）。
var goalSpec = vault.Spec{Kind: "goal"}
