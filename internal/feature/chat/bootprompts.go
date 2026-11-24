package chat

import (
	"context"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 看板回答啟動對話框（信任資料夾等）：引擎服務不開放給看板，這裡經 session 模組轉過去，人名由名冊補上。
type bootPromptAPI interface {
	bootPromptLister
	AnswerBootPrompt(ctx context.Context, session, id, choice, by string) error
}

// bootPromptsTimeout 是問引擎「有沒有卡在啟動對話框」的上限（全部 session 加起來）。
var bootPromptsTimeout = 2 * time.Second

type bootPromptService struct{ m *Module }

func (s *bootPromptService) GetBootPrompts(ctx context.Context, _ *sdk.WebGetBootPromptsRequest) (*sdk.WebGetBootPromptsResponse, error) {
	resp := &sdk.WebGetBootPromptsResponse{}
	api, ok := s.m.d.Sessions.(bootPromptAPI)
	if !ok {
		return resp, nil
	}
	// 這是當場去問引擎的（每個沒停的 session 問一次）：引擎不回時不能跟著等——看板每 15 秒問一次，
	// 等到看板的讀取時限才放手會一直佔著一條連線（WEB-175）。到時限就當成沒有提示，下一次再問。
	call, cancel := context.WithTimeout(ctx, bootPromptsTimeout)
	defer cancel()
	prompts, err := api.BootPrompts(call)
	if err != nil {
		if call.Err() != nil && ctx.Err() == nil {
			return resp, nil
		}
		return nil, s.m.fail(err)
	}
	if len(prompts) == 0 {
		return resp, nil
	}
	names := s.agentNames(ctx)
	for _, p := range prompts {
		resp.Prompts = append(resp.Prompts, sdk.WebBootPrompt{
			ID: p.ID, Session: p.Session, AgentName: names[p.Session], Title: p.Title, Choices: p.Choices,
		})
	}
	return resp, nil
}

func (s *bootPromptService) AnswerBootPrompt(ctx context.Context, req *sdk.WebAnswerBootPromptRequest) (*sdk.WebAnswerBootPromptResponse, error) {
	who, err := by(req.By)
	if err != nil {
		return nil, s.m.fail(err)
	}
	if !platform.IsOwner(who) {
		return nil, s.m.fail(platform.Forbidden("只有主人能回答啟動提示"))
	}
	api, ok := s.m.d.Sessions.(bootPromptAPI)
	if !ok {
		return nil, s.m.fail(platform.NotFound("這個啟動提示已經不在了"))
	}
	if err := api.AnswerBootPrompt(ctx, req.Session, req.ID, req.Choice, who); err != nil {
		return nil, s.m.fail(err)
	}
	return &sdk.WebAnswerBootPromptResponse{}, nil
}

// agentNames：session 名稱 → 秘書人名。名冊或 session 讀不到就少了名字，不擋住提示本身。
func (s *bootPromptService) agentNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s.m.d.Roster == nil {
		return out
	}
	agents, err := s.m.d.Roster.List(ctx)
	if err != nil {
		return out
	}
	display := map[string]string{}
	for _, a := range agents {
		if a != nil {
			name, _ := a.Fields["display_name"].(string)
			display[a.ID] = name
		}
	}
	sessions, err := s.m.d.Sessions.List(ctx, "", nil)
	if err != nil {
		return out
	}
	for _, x := range sessions {
		if x != nil {
			out[x.Name] = display[x.Agent]
		}
	}
	return out
}
