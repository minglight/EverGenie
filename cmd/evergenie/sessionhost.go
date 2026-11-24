package main

import (
	"context"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/engine"
	"github.com/minglight/evergenie/internal/base/session"
	"github.com/minglight/evergenie/internal/feature/agents/persona"
	"github.com/minglight/evergenie/internal/feature/tasks/task"
	"github.com/minglight/evergenie/sdk"
)

// 底層 session 要的名冊、引擎、任務單（介面由 session 定義），registry 把別的模組包成它要的形狀。

// sessionAgents 是 session 看到的名冊：agent 的名冊方法加角色庫的 ShowPersona、StyleInfo（測口吻用；tone.go 留在 session）。
type sessionAgents struct {
	*agent.Module
	library *persona.Module
}

func (a sessionAgents) ShowPersona(ctx context.Context, slug string) (sdk.PersonaCard, string, string, error) {
	return a.library.ShowPersona(ctx, slug)
}

// OfficialCardFile 是官方卡在哪（角色庫的共用找卡入口，PRS-25；測口吻的報告寫卡片位置用）。
func (a sessionAgents) OfficialCardFile(slug string) (string, bool) {
	return a.library.OfficialCardFile(slug)
}

func (a sessionAgents) StyleInfo(ctx context.Context) (string, string, bool, []sdk.PersonaStyleLevel, error) {
	return a.library.StyleInfo(ctx)
}

// sessionEngines narrows the registry result to the session-owned interface.
type sessionEngines struct{ engine *engine.Module }

func (r sessionEngines) For(account string) (session.EngineAccess, error) {
	return r.engine.For(account)
}

// sessionTasks 將本機任務單接到工作 session（讀單、改單）。
type sessionTasks struct{ *task.Module }

var _ session.TaskAccess = sessionTasks{}
