package chat

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

type bootSessions struct {
	Sessions
	prompts  []*sdk.BootPrompt
	answered string
}

func (b *bootSessions) List(context.Context, string, []sdk.SessionState) ([]*sdk.Session, error) {
	return []*sdk.Session{{Name: "s1", Agent: "admin"}}, nil
}
func (b *bootSessions) BootPrompts(context.Context) ([]*sdk.BootPrompt, error) { return b.prompts, nil }
func (b *bootSessions) AnswerBootPrompt(_ context.Context, session, id, choice, by string) error {
	b.answered = session + "|" + id + "|" + choice + "|" + by
	return nil
}

func TestBootPromptAnswerOnlyOwner(t *testing.T) {
	fake := &bootSessions{}
	m := &Module{}
	m.d.Sessions = fake
	s := &bootPromptService{m: m}
	req := &sdk.WebAnswerBootPromptRequest{Session: "s1", ID: "s1#trust", Choice: "信任", By: &sdk.Actor{Name: "code"}}
	if _, err := s.AnswerBootPrompt(context.Background(), req); err == nil || fake.answered != "" {
		t.Fatalf("秘書不該能回答：err=%v answered=%q", err, fake.answered)
	}
	req.By = &sdk.Actor{Name: "owner(web)"}
	if _, err := s.AnswerBootPrompt(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if fake.answered != "s1|s1#trust|信任|owner(web)" {
		t.Fatalf("answered = %q", fake.answered)
	}
}

func TestGetBootPromptsListsPrompt(t *testing.T) {
	m := &Module{}
	m.d.Sessions = &bootSessions{prompts: []*sdk.BootPrompt{{ID: "s1#trust", Session: "s1", Title: "信任？", Choices: []string{"是"}}}}
	res, err := (&bootPromptService{m: m}).GetBootPrompts(context.Background(), &sdk.WebGetBootPromptsRequest{})
	if err != nil || len(res.Prompts) != 1 || res.Prompts[0].ID != "s1#trust" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// stuckBootSessions：引擎不回——BootPrompts 卡到呼叫的人放棄為止。
type stuckBootSessions struct{ bootSessions }

func (b *stuckBootSessions) BootPrompts(ctx context.Context) ([]*sdk.BootPrompt, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// 引擎不回時，看板問啟動提示不跟著等：到時限就回「沒有提示」，不是錯誤、也不卡住（WEB-175）。
func TestGetBootPromptsGivesUpWhenEngineNeverAnswers(t *testing.T) {
	old := bootPromptsTimeout
	bootPromptsTimeout = 50 * time.Millisecond
	defer func() { bootPromptsTimeout = old }()
	m := &Module{}
	m.d.Sessions = &stuckBootSessions{}
	done := make(chan struct{})
	var res *sdk.WebGetBootPromptsResponse
	var err error
	go func() {
		defer close(done)
		res, err = (&bootPromptService{m: m}).GetBootPrompts(context.Background(), &sdk.WebGetBootPromptsRequest{})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("引擎不回時 GetBootPrompts 跟著卡住")
	}
	if err != nil || res == nil || len(res.Prompts) != 0 {
		t.Fatalf("要回沒有提示：res=%+v err=%v", res, err)
	}
}
