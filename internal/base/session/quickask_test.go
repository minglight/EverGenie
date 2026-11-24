package session

import (
	"context"
	"testing"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

// 語音入口的一次性提問：家的預設帳號、帶模型與力度、不開 session（SES-69）。
func TestQuickAskUsesDefaultAccountWithoutSession(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.e.Configure(fake.Behavior{Delivered: true})
	got, err := f.m.QuickAsk(ctx, "sonnet", "low", "只回 JSON", "給誰", 45)
	must(t, err)
	if got != "[fake] 給誰" {
		t.Fatalf("回覆：%q", got)
	}
	for _, c := range f.e.Calls() {
		switch c.Method {
		case "Ask":
			r := c.Request.(*sdk.EngineAskRequest)
			if r.Account != "main" || r.Model != "sonnet" || r.Effort != "low" || r.TimeoutSeconds != 45 || r.Instructions != "只回 JSON" || r.Cwd == "" {
				t.Errorf("Ask 的內容：%+v", r)
			}
		case "Start", "Deliver":
			t.Errorf("一次性提問不能開 session：%s", c.Method)
		}
	}
	if _, err := f.m.QuickAsk(ctx, "", "", "", "  ", 0); err == nil {
		t.Fatal("空的訊息要擋")
	}
}
