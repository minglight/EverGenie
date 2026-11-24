package rules

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// Show 列出權限的現況（evergenie trust …，Q5）。只讀設定；snapshot 為真時只寫 runtime/trust/snapshots/。
func (m *Module) Show(ctx context.Context, req *sdk.SystemTrustRequest) (*sdk.SystemTrustResponse, error) {
	home := m.homeDir()
	if home == "" {
		return nil, platform.Unavailable("還不知道家在哪裡")
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return nil, err
	}
	out := &sdk.SystemTrustResponse{Policy: Policy(h.Trust), DefaultLevel: DefaultFor(h.Trust)}
	out.Agents = m.agentLevels(ctx, h.Trust)
	out.Sources = sources(h.Trust, out.Agents)
	days := req.DeniedDays
	if days <= 0 {
		days = 7
	}
	if out.Denied, err = ReadDenied(home, m.now().AddDate(0, 0, -int(days))); err != nil {
		return nil, err
	}
	if req.Snapshot {
		if out.SnapshotPath, out.Changes, err = m.snapshot(out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SetLevels 改權限等級；只有主人可以（秘書要改權限走申請，Q7）。by 是已驗過的操作者名稱。
func (m *Module) SetLevels(ctx context.Context, by string, def sdk.PermissionLevel, levels []sdk.AgentPermission) (*sdk.SystemSetPermissionLevelsResponse, error) {
	if !platform.IsOwner(by) {
		return nil, platform.Forbidden("只有主人能改權限等級；秘書請送權限申請")
	}
	home := m.homeDir()
	if home == "" {
		return nil, platform.Unavailable("還不知道家在哪裡")
	}
	backup, err := setLevels(home, def, levels, m.now())
	if err != nil {
		return nil, err
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return nil, err
	}
	if log := m.logger(); log != nil {
		log.Info("權限等級已更新", "by", by, "backup", backup)
	}
	return &sdk.SystemSetPermissionLevelsResponse{Agents: m.agentLevels(ctx, h.Trust), Backup: backup}, nil
}

// agentLevels 是名冊上每位秘書＋trust.toml 寫到的職位，依 id 排序。
func (m *Module) agentLevels(ctx context.Context, t platform.Trust) []sdk.AgentPermission {
	ids := map[string]bool{}
	if m.d.Vault != nil {
		if agents, err := m.d.Vault.List(ctx, "agent", vault.Query{}); err == nil {
			for _, a := range agents {
				ids[a.ID] = true
			}
		}
	}
	for id := range t.Agents {
		ids[id] = true
	}
	out := make([]sdk.AgentPermission, 0, len(ids))
	for id := range ids {
		l, src := LevelSource(t, id)
		out = append(out, sdk.AgentPermission{Agent: id, Level: l, Source: src})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

func sources(t platform.Trust, agents []sdk.AgentPermission) []sdk.PermissionSource {
	const src = "settings/trust.toml"
	var out []sdk.PermissionSource
	if t.DefaultLevel != "" {
		out = append(out, sdk.PermissionSource{Scope: "整個家", Source: src, Kind: "default_level", Value: string(DefaultFor(t))})
	}
	for _, a := range agents {
		source := src
		if a.Source == "builtin" {
			source = "（內建預設）"
		}
		out = append(out, sdk.PermissionSource{Scope: "秘書 " + a.Agent, Source: source, Kind: "level", Value: string(a.Level)})
	}
	p := Policy(t)
	for _, r := range p.Repos {
		v := r.Path
		if r.Push {
			v += "（可以 push）"
		}
		if len(r.Writers) > 0 {
			v += "；可寫：" + strings.Join(r.Writers, "、")
		}
		out = append(out, sdk.PermissionSource{Scope: "repo", Source: src, Kind: "repo", Value: v})
	}
	for _, a := range p.Allow {
		out = append(out, sdk.PermissionSource{Scope: "整個家", Source: src, Kind: "allow", Value: a})
	}
	for _, d := range p.Deny {
		out = append(out, sdk.PermissionSource{Scope: "整個家", Source: src, Kind: "deny", Value: d})
	}
	return out
}

// snapshot 把權限來源存成 runtime/trust/snapshots/<時間>.json，回傳相對路徑與跟上一份的差異。
func (m *Module) snapshot(r *sdk.SystemTrustResponse) (string, []string, error) {
	rel, prev, err := snapshotTrust(m.homeDir(), m.now(), r.Sources)
	if err != nil || prev == nil {
		return rel, nil, err
	}
	return rel, diffSources(prev, r.Sources), nil
}

func diffSources(old, cur []sdk.PermissionSource) []string {
	key := func(p sdk.PermissionSource) string { return p.Scope + "\x00" + p.Kind + "\x00" + p.Value }
	label := func(p sdk.PermissionSource) string { return fmt.Sprintf("%s 的 %s：%s", p.Scope, p.Kind, p.Value) }
	had, has := map[string]bool{}, map[string]bool{}
	for _, p := range old {
		had[key(p)] = true
	}
	for _, p := range cur {
		has[key(p)] = true
	}
	var out []string
	for _, p := range old {
		if !has[key(p)] {
			out = append(out, "拿掉 "+label(p))
		}
	}
	for _, p := range cur {
		if !had[key(p)] {
			out = append(out, "新增 "+label(p))
		}
	}
	return out
}
