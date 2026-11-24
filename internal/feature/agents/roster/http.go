package roster

import (
	"context"
	"errors"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "agents"，實作是下面的 service；
// 另外 web 服務裡的 get_roster、get_agents（看板的名冊與設定頁，board.go）用 Mount.Methods 只登記這兩個方法。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{
		{Service: "agents", Impl: &service{m: m}},
		{Service: "web", Impl: &webService{m: m}, Methods: []string{"get_roster", "get_agents"}},
	}
}

// service 實作 sdk.AgentsAPI：驗操作者、呼叫名冊管理（manage.go）、把錯誤轉成 API 錯誤。
type service struct{ m *Module }

var _ sdk.AgentsAPI = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && s.m.log != nil && !errors.Is(err, context.Canceled) {
		s.m.log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) roster() (manager, error) {
	if s.m.ops == nil {
		return nil, platform.Unavailable("名冊管理還沒接上")
	}
	return s.m.ops, nil
}

// call 取操作者與名冊管理；操作者的權限由名冊管理（manage.go）判斷。
func (s *service) call(actor *sdk.Actor) (manager, string, error) {
	who, err := platform.ActorName(actor)
	if err != nil {
		return nil, "", err
	}
	r, err := s.roster()
	return r, who, err
}

func (s *service) Create(ctx context.Context, req *sdk.AgentsCreateRequest) (*sdk.AgentsCreateResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := a.Create(ctx, req.ID, req.Persona, req.DisplayName, req.Duty, req.Knowledge, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsCreateResponse{Agent: r}, nil
}

func (s *service) Archive(ctx context.Context, req *sdk.AgentsArchiveRequest) (*sdk.AgentsArchiveResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	done, restore, err := a.Archive(ctx, req.ID, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsArchiveResponse{Archived: done, Restore: restore}, nil
}

func (s *service) Rename(ctx context.Context, req *sdk.AgentsRenameRequest) (*sdk.AgentsRenameResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, moved, mentions, err := a.Rename(ctx, req.ID, req.NewID, req.DisplayName, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsRenameResponse{Agent: r, Moved: moved, Mentions: mentions}, nil
}

func (s *service) Assign(ctx context.Context, req *sdk.AgentsAssignRequest) (*sdk.AgentsAssignResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if err := a.Assign(ctx, req.ID, req.Persona, who); err != nil {
		return nil, s.fail(err)
	}
	if s.m.d.Vault == nil {
		return nil, s.fail(platform.Unavailable("資料庫還沒接上"))
	}
	r, err := s.m.d.Vault.Get(ctx, "agent", req.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsAssignResponse{Agent: &r}, nil
}

func (s *service) Swap(ctx context.Context, req *sdk.AgentsSwapRequest) (*sdk.AgentsSwapResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	rows, err := a.Swap(ctx, req.A, req.B, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsSwapResponse{Agents: values(rows)}, nil
}

func (s *service) SetAccount(ctx context.Context, req *sdk.AgentsSetAccountRequest) (*sdk.AgentsSetAccountResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := a.SetAccount(ctx, req.ID, req.Account, req.Model, req.Effort, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsSetAccountResponse{Agent: r}, nil
}

func (s *service) SetField(ctx context.Context, req *sdk.AgentsSetFieldRequest) (*sdk.AgentsSetFieldResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	r, err := a.SetField(ctx, req.ID, req.Field, req.Value, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsSetFieldResponse{Agent: r}, nil
}

// Link：預演誰都可以看；真的寫（apply）會改各帳號的資料夾，只有主人能做。
func (s *service) Link(ctx context.Context, req *sdk.AgentsLinkRequest) (*sdk.AgentsLinkResponse, error) {
	if req.Apply {
		who, err := platform.ActorName(req.By)
		if err != nil {
			return nil, s.fail(err)
		}
		if !platform.IsOwner(who) {
			return nil, s.fail(platform.Forbidden("只有主人能寫入各帳號的秘書檔案；秘書可以先預演"))
		}
	}
	a, err := s.roster()
	if err != nil {
		return nil, s.fail(err)
	}
	if s.m.d.Cards == nil {
		return nil, s.fail(platform.Unavailable("引擎服務還沒接上"))
	}
	cards, userName, err := a.Cards(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	rows, err := s.m.d.Cards.WriteAgentCards(ctx, cards, userName, req.Apply)
	if err != nil {
		return nil, s.fail(err)
	}
	if req.Apply && s.m.log != nil {
		s.m.log.Info("秘書檔案已寫入各帳號", "accounts", len(rows))
	}
	return &sdk.AgentsLinkResponse{Applied: req.Apply, Accounts: rows}, nil
}

func (s *service) AddRule(ctx context.Context, req *sdk.AgentsAddRuleRequest) (*sdk.AgentsAddRuleResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	body, err := a.AddRule(ctx, req.ID, req.Section, req.Rule, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsAddRuleResponse{Addendum: body}, nil
}

func (s *service) Customizations(ctx context.Context, req *sdk.AgentsCustomizationsRequest) (*sdk.AgentsCustomizationsResponse, error) {
	a, err := s.roster()
	if err != nil {
		return nil, s.fail(err)
	}
	duties, addendum, err := a.Customizations(ctx, req.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsCustomizationsResponse{Duties: duties, Addendum: addendum}, nil
}

// WriteMemory：只有本人或 system（agent 模組判斷）。
func (s *service) WriteMemory(ctx context.Context, req *sdk.AgentsWriteMemoryRequest) (*sdk.AgentsWriteMemoryResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if err := a.WriteMemory(ctx, req.ID, req.Body, who); err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsWriteMemoryResponse{}, nil
}

// WriteNote：做事筆記只有本人（或 system）寫自己的（agent 模組判斷）。
func (s *service) WriteNote(ctx context.Context, req *sdk.AgentsWriteNoteRequest) (*sdk.AgentsWriteNoteResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	name, created, rev, err := a.WriteNote(ctx, req.ID, req.Name, req.Body, req.Revision, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsWriteNoteResponse{Name: name, Created: created, Revision: rev}, nil
}

// AppendJournal：日誌只能追加，而且只有本人（或 system）寫自己的。
func (s *service) AppendJournal(ctx context.Context, req *sdk.AgentsAppendJournalRequest) (*sdk.AgentsAppendJournalResponse, error) {
	who, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if who != req.ID && who != "system" {
		return nil, s.fail(platform.Forbidden("只能寫自己的日誌"))
	}
	if strings.ContainsAny(req.Line, "\r\n") || strings.TrimSpace(req.Line) == "" {
		return nil, s.fail(platform.Invalid("line", "日誌每次追加一行，不能空白也不能換行"))
	}
	a, err := s.roster()
	if err != nil {
		return nil, s.fail(err)
	}
	if err := a.AppendJournal(ctx, req.ID, req.Line); err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsAppendJournalResponse{}, nil
}

// Knowledge：誰都可以看（只讀）。
func (s *service) Knowledge(ctx context.Context, req *sdk.AgentsKnowledgeRequest) (*sdk.AgentsKnowledgeResponse, error) {
	a, err := s.roster()
	if err != nil {
		return nil, s.fail(err)
	}
	blocks, err := a.Knowledge(ctx, req.Agent)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsKnowledgeResponse{Agent: req.Agent, Blocks: blocks}, nil
}

// SetKnowledge：只有主人（名冊管理判斷）。
func (s *service) SetKnowledge(ctx context.Context, req *sdk.AgentsSetKnowledgeRequest) (*sdk.AgentsKnowledgeResponse, error) {
	a, who, err := s.call(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	blocks, err := a.SetKnowledge(ctx, req.Agent, req.Enabled, who)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.AgentsKnowledgeResponse{Agent: req.Agent, Blocks: blocks}, nil
}

// values 把指標切片換成值切片（回應用值，nil 項目略過）。
func values(in []*sdk.Record) []sdk.Record {
	out := make([]sdk.Record, 0, len(in))
	for _, v := range in {
		if v != nil {
			out = append(out, *v)
		}
	}
	return out
}
