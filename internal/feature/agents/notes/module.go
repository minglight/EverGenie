// Package notes — 說明卡 ai_docs/modules/agents.md（AGS 的主題筆記段）、規格 ai_docs/schemas/note.md。
// 業務模組：全家共用的主題筆記（DEV-161）的 HTTP（sdk notes 服務）、`evergenie notes` 指令與 doctor 的黃字。
// 讀寫規則（誰能寫、行尾標記、索引產生）在底層 internal/base/agent，這裡只是它的入口。
package notes

import (
	"context"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Notes 是底層 agent 提供的主題筆記寫入與健檢（由 cmd/evergenie/registry.go 注入，MOD-2）。by 是操作者名稱，權限由 agent 判斷。
type Notes interface {
	AddNote(ctx context.Context, req *sdk.NotesAddRequest, by string) (*sdk.NotesAddResponse, error)
	SetNote(ctx context.Context, req *sdk.NotesSetRequest, by string) (*sdk.NotesSetResponse, error)
	ArchiveNote(ctx context.Context, req *sdk.NotesArchiveRequest, by string) (*sdk.NotesArchiveResponse, error)
	AddNoteCategory(ctx context.Context, req *sdk.NotesAddCategoryRequest, by string) (*sdk.NotesAddCategoryResponse, error)
	// 梗本（DEV-164）：開場那一節的全文、記下提過哪一條。
	NoteQuips(ctx context.Context, req *sdk.NotesQuipsRequest, by string) (*sdk.NotesQuipsResponse, error)
	QuipUsed(ctx context.Context, req *sdk.NotesQuipUsedRequest, by string) (*sdk.NotesQuipUsedResponse, error)
	NoteProblems(ctx context.Context) []string
}

// Module 沒有自己的資料與背景工作。
type Module struct{ n Notes }

// New 建模組；n 可以是 nil（API 回 unavailable、不登記健檢）。
func New(n Notes) *Module { return &Module{n: n} }

func (m *Module) Name() string                    { return "notes" }
func (m *Module) Start(ctx context.Context) error { return ctx.Err() }
func (m *Module) Stop(ctx context.Context) error  { return nil }

// Routes 登記 sdk 的 notes 服務。
func (m *Module) Routes() []app.Mount { return []app.Mount{{Service: "notes", Impl: &service{m: m}}} }

// Checks 登記 doctor 的黃字：筆記檔頭不合、索引跟筆記不符、記憶核心超過上限（報告、不擋）。
func (m *Module) Checks() []app.Check {
	if m.n == nil {
		return nil
	}
	return []app.Check{{Name: "config.notes", RunAll: m.checkNotes}}
}

func (m *Module) checkNotes(ctx context.Context) []app.CheckResult {
	problems := m.n.NoteProblems(ctx)
	if len(problems) == 0 {
		return []app.CheckResult{{Status: app.StatusOK, Message: "主題筆記、索引與記憶核心都對得上"}}
	}
	out := make([]app.CheckResult, 0, len(problems))
	for _, p := range problems {
		out = append(out, app.CheckResult{Status: app.StatusWarn, Message: p, Fix: "請秘書用 evergenie notes 整理（規格 evergenie docs show schemas/note.md）；索引由核心重產，不手改"})
	}
	return out
}

// service 實作 sdk.NotesAPI：驗操作者、轉給底層 agent、把錯誤轉成 API 錯誤。
type service struct{ m *Module }

var _ sdk.NotesAPI = (*service)(nil)

func (s *service) call(actor *sdk.Actor) (Notes, string, error) {
	who, err := platform.ActorName(actor)
	if err != nil {
		return nil, "", err
	}
	if s.m.n == nil {
		return nil, "", platform.Unavailable("主題筆記還沒接上")
	}
	return s.m.n, who, nil
}

// fail 把錯誤轉成對外的 API 錯誤（內部錯誤不外洩原文）。
func fail(err error) error { return platform.SDKError(err) }

func (s *service) Add(ctx context.Context, req *sdk.NotesAddRequest) (*sdk.NotesAddResponse, error) {
	n, who, err := s.call(req.By)
	if err != nil {
		return nil, fail(err)
	}
	out, err := n.AddNote(ctx, req, who)
	if err != nil {
		return nil, fail(err)
	}
	return out, nil
}

func (s *service) Set(ctx context.Context, req *sdk.NotesSetRequest) (*sdk.NotesSetResponse, error) {
	n, who, err := s.call(req.By)
	if err != nil {
		return nil, fail(err)
	}
	out, err := n.SetNote(ctx, req, who)
	if err != nil {
		return nil, fail(err)
	}
	return out, nil
}

func (s *service) Archive(ctx context.Context, req *sdk.NotesArchiveRequest) (*sdk.NotesArchiveResponse, error) {
	n, who, err := s.call(req.By)
	if err != nil {
		return nil, fail(err)
	}
	out, err := n.ArchiveNote(ctx, req, who)
	if err != nil {
		return nil, fail(err)
	}
	return out, nil
}

func (s *service) AddCategory(ctx context.Context, req *sdk.NotesAddCategoryRequest) (*sdk.NotesAddCategoryResponse, error) {
	n, who, err := s.call(req.By)
	if err != nil {
		return nil, fail(err)
	}
	out, err := n.AddNoteCategory(ctx, req, who)
	if err != nil {
		return nil, fail(err)
	}
	return out, nil
}

func (s *service) Quips(ctx context.Context, req *sdk.NotesQuipsRequest) (*sdk.NotesQuipsResponse, error) {
	n, who, err := s.call(req.By)
	if err != nil {
		return nil, fail(err)
	}
	out, err := n.NoteQuips(ctx, req, who)
	if err != nil {
		return nil, fail(err)
	}
	return out, nil
}

func (s *service) QuipUsed(ctx context.Context, req *sdk.NotesQuipUsedRequest) (*sdk.NotesQuipUsedResponse, error) {
	n, who, err := s.call(req.By)
	if err != nil {
		return nil, fail(err)
	}
	out, err := n.QuipUsed(ctx, req, who)
	if err != nil {
		return nil, fail(err)
	}
	return out, nil
}

var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
	_ app.CheckProvider = (*Module)(nil)
)
