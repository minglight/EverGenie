package main

import (
	"bytes"
	"strings"
	"testing"
)

const kindHead = "package x\nimport \"context\"\ntype store interface{ Get(context.Context, string, string) (any, error); List(context.Context, string, any) (any, error); Patch(context.Context, string, string, any, any, string) (any, error); Watch(context.Context, ...string) (any, error) }\nvar vault store\nvar ctx context.Context\n"

// Guards: REQ-145
// 每個 vault 種類只有擁有者直接讀寫（architecture.md「資料種類的擁有者」）；別的模組讀寫、表上沒有的種類都要失敗。
func TestKindOwnershipViolations(t *testing.T) {
	cases := map[string]struct{ mod, body, want string }{
		"feature reads other feature": {"feature/inbox/notify", "func f() { vault.Get(ctx, \"goal\", \"GOAL-1\") }\n", "inbox 直接讀 tasks 的資料"},
		"const kind":                  {"feature/inbox/notify", "const k = \"task\"\nfunc f() { vault.List(ctx, k, nil) }\n", "inbox 直接讀 tasks 的資料"},
		"watch other kind":            {"feature/trust/requests", "func f() { vault.Watch(ctx, \"job\", \"reminder\") }\n", "trust 直接讀 tasks 的資料"},
		"reader cannot write":         {"feature/tasks/job", "func f() { vault.Patch(ctx, \"agent\", \"code\", nil, nil, \"x\") }\n", "tasks 直接寫 agent 的資料"},
		"record literal":              {"feature/inbox/notify", "import \"github.com/minglight/evergenie/internal/base/vault\"\nvar _ = vault.Record{Kind: \"task\"}\n", "inbox 直接寫 tasks 的資料"},
		"base module":                 {"base/session", "func f() { vault.Get(ctx, \"task\", \"SEC-1\") }\n", "session 直接讀 tasks 的資料"},
		"unknown kind":                {"feature/tasks/task", "func f() { vault.Get(ctx, \"mystery\", \"x\") }\n", "\"mystery\" 沒有登記擁有者"},
		// message 只給 inbox/notify（S8-5）：同塊的 overview、menu 也要經 notify。
		"same block other sub-package":      {"feature/inbox/overview", "func f() { vault.List(ctx, \"message\", nil) }\n", "inbox/overview 直接讀 inbox/notify 的資料"},
		"block root is not the sub-package": {"feature/inbox", "func f() { vault.Get(ctx, \"message\", \"M-1\") }\n", "inbox 直接讀 inbox/notify 的資料"},
		"other block to sub-package":        {"feature/tasks/goal", "func f() { vault.Patch(ctx, \"message\", \"M-1\", nil, nil, \"x\") }\n", "tasks 直接寫 inbox/notify 的資料"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			src := strings.Replace(kindHead, "package x", "package "+c.mod[strings.LastIndex(c.mod, "/")+1:], 1)
			if strings.HasPrefix(c.body, "import") {
				src = strings.Replace(src, "import \"context\"\n", "import \"context\"\n"+strings.SplitN(c.body, "\n", 2)[0]+"\n", 1)
				c.body = strings.SplitN(c.body, "\n", 2)[1]
			}
			var out bytes.Buffer
			if code := check(fixture(t, c.mod, src+c.body), &out); code != 1 || !strings.Contains(out.String(), c.want) {
				t.Fatalf("%d: %s", code, &out)
			}
		})
	}
}

// Guards: REQ-145
// 擁有者讀寫自己的（擁有者是塊時同一塊的子套件都算，是子套件時只有它）、「也可以讀」的模組只讀、算出來的種類不判斷，都不報錯。
func TestKindOwnershipAllowed(t *testing.T) {
	cases := map[string]struct{ mod, body string }{
		"owner":     {"feature/tasks/task", "func f() { vault.Get(ctx, \"task\", \"SEC-1\"); vault.Patch(ctx, \"task\", \"SEC-1\", nil, nil, \"x\") }\n"},
		"reader":    {"feature/tasks/job", "func f() { vault.Get(ctx, \"agent\", \"code\") }\n"},
		"dynamic":   {"feature/tasks/task", "func f(k string) { vault.Get(ctx, k, \"x\") }\n"},
		"not vault": {"feature/tasks/task", "type c struct{}\nfunc (c) Get(context.Context, string, string) {}\nfunc f() { c{}.Get(ctx, \"goal\", \"x\") }\n"},
		// S8-2 起擁有者是塊：同一塊（tasks）的子套件共用那塊的資料。
		"same block": {"feature/tasks/goal", "func f() { vault.Get(ctx, \"task\", \"SEC-1\"); vault.Patch(ctx, \"reminder\", \"R-1\", nil, nil, \"x\") }\n"},
		// 擁有者是子套件時，那個子套件讀寫自己的。
		"sub-package owner":       {"feature/inbox/notify", "func f() { vault.Get(ctx, \"message\", \"M-1\"); vault.Patch(ctx, \"message\", \"M-1\", nil, nil, \"x\") }\n"},
		"deeper package of owner": {"feature/inbox/notify/inner", "func f() { vault.Get(ctx, \"message\", \"M-1\") }\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			src := strings.Replace(kindHead, "package x", "package "+c.mod[strings.LastIndex(c.mod, "/")+1:], 1)
			var out bytes.Buffer
			if code := check(fixture(t, c.mod, src+c.body), &out); code != 0 {
				t.Fatalf("%d: %s", code, &out)
			}
		})
	}
}
