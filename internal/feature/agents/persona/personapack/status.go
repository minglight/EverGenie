package personapack

import (
	"encoding/json"
	"os"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Last 是上次同步的結果（runtime/personas-pack/last.json）。
type Last struct {
	SyncResult
	At string `json:"at"`
}

// State 是角色包的現況：登記的版號、這一版指標的版號、有沒有待補、上次結果。
type State struct {
	Registered string   // 登記的版號；空＝還沒同步過
	Latest     string   // 這一版核心指標的版號；空＝這一版沒有角色包
	Source     bool     // 角色包的來源（[admin] public_personas 有寫）：不同步
	Pending    *Pending // 沒補上的
	Last       *Last    // 上次同步的結果；沒同步過是 nil
}

// Status 讀現況（不連網路、不改任何東西）。
func Status(home, pointerPath string) (*State, error) {
	h, err := platform.LoadHome(home)
	if err != nil {
		return nil, err
	}
	st := &State{Source: h.PersonaPackSource(), Pending: ReadPending(home)}
	reg, err := LoadRegistry(home)
	if err != nil {
		return nil, err
	}
	st.Registered = reg.Version
	if ptr, err := ReadPointer(pointerPath); err == nil && ptr != nil {
		st.Latest = ptr.Version
	}
	if data, err := os.ReadFile(lastPath(home)); err == nil {
		var l Last
		if json.Unmarshal(data, &l) == nil && l.Status != "" {
			st.Last = &l
		}
	}
	return st, nil
}

// PendingNote 是 doctor 用的：有沒補上的包就回白話一句，沒有回空字串（角色包的來源不算）。
func PendingNote(home string) string {
	if h, err := platform.LoadHome(home); err != nil || h.PersonaPackSource() {
		return ""
	}
	if p := ReadPending(home); p != nil {
		return "角色包還沒補上：" + p.Reason + "，有網路時會自動再試"
	}
	return ""
}
