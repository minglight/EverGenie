package session

import (
	"context"
	"errors"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func isBootNotFound(err error) bool {
	var e *platform.Err
	return errors.As(err, &e) && e.Code == sdk.ErrorCodeNotFound
}

type bootEngine struct {
	EngineAccess
	prompts  []*sdk.BootPrompt
	answered []string
}

func (b *bootEngine) BootPrompts(context.Context, string) ([]*sdk.BootPrompt, error) {
	return b.prompts, nil
}

func (b *bootEngine) AnswerBootPrompt(_ context.Context, id, choice, by string) error {
	b.answered = append(b.answered, id+"|"+choice+"|"+by)
	return nil
}

type bootRegistry struct {
	EngineRegistry
	engine *bootEngine
}

func (r bootRegistry) For(account string) (EngineAccess, error) {
	real, err := r.EngineRegistry.For(account)
	if err != nil {
		return nil, err
	}
	r.engine.EngineAccess = real
	return r.engine, nil
}

func TestAnswerBootPromptChecksPrompt(t *testing.T) {
	f := dispatchSetup(t)
	eng := &bootEngine{prompts: []*sdk.BootPrompt{{ID: "code#trust", Session: "code"}}}
	f.m.d.Engine = bootRegistry{f.m.d.Engine, eng}
	ctx := context.Background()

	if err := f.m.AnswerBootPrompt(ctx, "code", "code#other", "信任", "owner(web)"); !isBootNotFound(err) {
		t.Fatalf("提示不在要 NotFound：%v", err)
	}
	if err := f.m.AnswerBootPrompt(ctx, "nobody", "code#trust", "信任", "owner(web)"); !isBootNotFound(err) {
		t.Fatalf("沒這個 session 要 NotFound：%v", err)
	}
	if len(eng.answered) != 0 {
		t.Fatalf("不該按鍵：%v", eng.answered)
	}
	if err := f.m.AnswerBootPrompt(ctx, "code", "code#trust", "信任", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if len(eng.answered) != 1 || eng.answered[0] != "code#trust|信任|owner(web)" {
		t.Fatalf("answered = %v", eng.answered)
	}
}
