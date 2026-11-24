package main

import (
	"context"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/feature/agents/roster"
	"github.com/minglight/evergenie/internal/feature/packages/pkg"
	"github.com/minglight/evergenie/internal/feature/tasks/job"
	"github.com/minglight/evergenie/sdk"
)

// packageSync 把 pkg 的 `packages sync` 接到名冊（agent、agents 塊的名冊管理）與排程（tasks 塊的 job）：pkg 不直接依賴它們，由這裡組裝（D-28、REQ-174）。
// 這裡只轉手；規則（封存過的 id 不能重用、預設人設、套上樣板欄位）在 packages（2026-11-01 搬過去）。
type packageSync struct {
	home   string
	roster *agent.Module  // 名冊底層：檢查人設、寫官方職責
	admin  *roster.Module // 名冊管理（agents 塊的 roster）：建秘書、改欄位
	store  *vault.Module
	timing *job.Module
}

var _ pkg.SyncHost = packageSync{}

// DefaultPersona 是 home.toml 的 [defaults] persona。
func (s packageSync) DefaultPersona() string {
	h, err := platform.LoadHome(s.home)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(h.Defaults.Persona)
}

func (s packageSync) AgentDuties(ctx context.Context, id string) (string, bool, bool, error) {
	r, err := s.store.Get(ctx, "agent", id)
	if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	return r.Body, !r.Archived, r.Archived, nil
}

func (s packageSync) CreateAgent(ctx context.Context, id, persona, duty string, knowledge []string, by string) error {
	_, err := s.admin.Create(ctx, id, persona, "", duty, knowledge, by)
	return err
}

func (s packageSync) SetAgentField(ctx context.Context, id, field, value, by string) error {
	_, err := s.admin.SetField(ctx, id, field, value, by)
	return err
}

func (s packageSync) CheckPersona(persona string) error { return s.roster.CheckAssignable(persona) }

func (s packageSync) WriteDuties(ctx context.Context, id, body, by string) error {
	return s.roster.WriteDuties(ctx, id, body, by)
}

func (s packageSync) JobIDs(ctx context.Context, agentID string) ([]string, error) {
	jobs, err := s.timing.ListJobs(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(jobs))
	for _, j := range jobs {
		ids = append(ids, j.ID)
	}
	return ids, nil
}

func (s packageSync) SaveJob(ctx context.Context, j *sdk.Job, by string) error {
	_, err := s.timing.SaveJob(ctx, j, by)
	return err
}
