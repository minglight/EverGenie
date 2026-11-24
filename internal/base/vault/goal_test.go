package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Guards: REQ-1
func TestGoalPathRecognized(t *testing.T) {
	for _, path := range []string{"goals/GOAL-1.md", "goals/archive/2026-10/GOAL-1.md"} {
		kind, id, archived := inferIdentity(path, Record{})
		if kind != "goal" || id != "GOAL-1" || archived != (path != "goals/GOAL-1.md") {
			t.Fatalf("path not recognized: %s", path)
		}
	}
	if got := defaultPath("goal", "GOAL-1"); got != "vault/goals/GOAL-1.md" {
		t.Fatal(got)
	}
	if _, ok := linkID("[[GOAL-1]]", "goal", nil); !ok {
		t.Fatal("goal link rejected")
	}
	if _, ok := linkID("[[G-1]]", "goal", nil); ok {
		t.Fatal("wrong prefix accepted")
	}
}

// Guards: REQ-1, REQ-2
func TestGoalNextIDRestoresFromRecords(t *testing.T) {
	m, home := openStore(t)
	// openStore 已經註冊了 goal 這個種類。
	ctx := context.Background()
	id, err := m.NextID(ctx, "goal")
	if err != nil || id != "GOAL-1" {
		t.Fatalf("NextID: %s %v", id, err)
	}
	if _, err := m.Put(ctx, Record{Kind: "goal", ID: "GOAL-8", Fields: map[string]any{}}, "system", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "vault/goals/GOAL-8.md")); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	id, err = m.NextID(ctx, "goal")
	if err != nil || id != "GOAL-9" {
		t.Fatalf("restored NextID: %s %v", id, err)
	}
}

// 權限申請單（vault/policy/P-<n>.md）的檔頭有自己的 kind 欄位（rule／guidance）：重開 vault 之後照資料夾認成 policy_request，不被 kind 欄位帶走。
func TestPolicyRequestFoundAfterReopen(t *testing.T) {
	for _, path := range []string{"policy/P-1.md", "policy/archive/2026-10/P-1.md"} {
		if kind, id, _ := inferIdentity(path, Record{Kind: "guidance", ID: "P-1"}); kind != "policy_request" || id != "P-1" {
			t.Fatalf("%s 認成 %s/%s", path, kind, id)
		}
	}
	m, home := openStore(t)
	ctx := context.Background()
	m.RegisterSchema(Spec{Kind: "policy_request", Path: func(id string) string { return "vault/policy/" + id + ".md" }})
	if _, err := m.Put(ctx, Record{Kind: "policy_request", ID: "P-1", Fields: map[string]any{"kind": "guidance", "status": "applied"}}, "system", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "vault/policy/P-1.md")); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(home); err != nil {
		t.Fatal(err)
	}
	r, err := m.Get(ctx, "policy_request", "P-1")
	if err != nil || r.Fields["kind"] != "guidance" {
		t.Fatalf("重開後讀不到申請單：%v %+v", err, r.Fields)
	}
	if rs, err := m.List(ctx, "policy_request", Query{}); err != nil || len(rs) != 1 {
		t.Fatalf("List：%v %d", err, len(rs))
	}
}
