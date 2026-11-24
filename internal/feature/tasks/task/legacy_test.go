package task

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 舊的九種狀態讀進來就是新的六種（2026-11-01）：review→等你（proposed 留著、failed 的建議→不做了）、
// failed／shelved→不做了（原因寫在結果前面）、idea 還是待做；驗收、退回、清單、等你處理都照新狀態。
func TestLegacyStatusesReadAsNew(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	now := h.Clock.Now().Format(time.RFC3339Nano)
	h.WithRecords(
		vault.Record{Kind: "task", ID: "SEC-50", Fields: map[string]any{"title": "交付了", "type": "task", "agent": "code", "status": "review", "waiting_since": now, "result": "做好了", "verify": "verified", "verify_how": "測試", "proposed": "failed", "site": "office"}},
		vault.Record{Kind: "task", ID: "SEC-51", Fields: map[string]any{"title": "做不到", "type": "ask", "agent": "code", "status": "failed", "result": "沒有權限", "closed_at": now}},
		vault.Record{Kind: "task", ID: "SEC-52", Fields: map[string]any{"title": "先放著", "type": "task", "agent": "code", "status": "shelved", "result": "等明年"}},
		vault.Record{Kind: "task", ID: "SEC-53", Fields: map[string]any{"title": "想法", "type": "task", "status": "idea"}},
	)
	get := func(id string) Task {
		t.Helper()
		got, err := m.Get(ctx, id)
		must(t, err)
		return got
	}
	if got := get("SEC-50"); got.Status != sdk.TaskStatusWaitingUser || got.Proposed != sdk.TaskStatusCancelled || got.Ask != reviewAsk {
		t.Fatal(got)
	}
	if got := get("SEC-51"); got.Status != sdk.TaskStatusCancelled || got.Result != "無法完成：沒有權限" {
		t.Fatal(got)
	}
	if got := get("SEC-52"); got.Status != sdk.TaskStatusCancelled || got.Result != "擱置：等明年" || got.ClosedAt.IsZero() {
		t.Fatal(got)
	}
	if got := get("SEC-53"); got.Status != sdk.TaskStatusIdea {
		t.Fatal(got)
	}
	// 用舊值查也查得到（review→等你、shelved→不做了）。
	for q, want := range map[sdk.TaskStatus]int{"review": 1, sdk.TaskStatusWaitingUser: 1, "shelved": 2, sdk.TaskStatusCancelled: 2} {
		rows, err := m.List(ctx, &sdk.TasksListRequest{Statuses: []sdk.TaskStatus{q}})
		must(t, err)
		if len(rows) != want {
			t.Fatalf("%s: %d", q, len(rows))
		}
	}
	waiting, err := m.Waiting(ctx, h.Clock.Now())
	must(t, err)
	if len(waiting) != 1 || waiting[0].Task.ID != "SEC-50" {
		t.Fatal(waiting)
	}
	// 舊的 review 照樣能驗收；寫回去的檔案就是新狀態、沒有 site。
	got, err := m.Accept(ctx, "SEC-50", "owner(web)")
	must(t, err)
	if got.Status != sdk.TaskStatusCancelled || got.AcceptedBy != "owner(web)" {
		t.Fatal(got)
	}
	raw, err := h.Vault.Get(ctx, "task", "SEC-50")
	must(t, err)
	if raw.Fields["status"] != "cancelled" || raw.Fields["site"] != nil {
		t.Fatal(raw.Fields)
	}
}

// 交付等驗收＝等你＋proposed：主人手動改回進行中、或秘書改問一件事，就不再是交付（驗收、退回都不行）。
func TestDeliveryEndsWhenLeavingOrAsking(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	r := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Submit(ctx, r.ID, submission(), "code")
	must(t, err)
	got, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusDoing)}, "owner(web)")
	must(t, err)
	if got.Proposed != "" || got.Ask != "" {
		t.Fatal(got)
	}
	_, err = m.Submit(ctx, r.ID, submission(), "code")
	must(t, err)
	got, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser), Ask: ptr("A 還是 B？")}, "code")
	must(t, err)
	if got.Status != sdk.TaskStatusWaitingUser || got.Proposed != "" || got.Ask != "A 還是 B？" {
		t.Fatal(got)
	}
	if _, err := m.Accept(ctx, r.ID, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal(err)
	}
	if _, err := m.Reject(ctx, r.ID, "不對", "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal(err)
	}
	// 建議的結局只能是完成或不做了；舊的 failed 當成不做了。
	s := submission()
	s.Proposed = sdk.TaskStatusDoing
	if _, err := m.Submit(ctx, r.ID, s, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
	s.Proposed = "failed"
	got, err = m.Submit(ctx, r.ID, s, "code")
	must(t, err)
	if got.Proposed != sdk.TaskStatusCancelled {
		t.Fatal(got)
	}
	// 問答類不用送驗收。
	ask := makeTask(t, m, sdk.TaskTypeAsk)
	if _, err := m.Submit(ctx, ask.ID, submission(), "code"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal(err)
	}
}

// 問答秘書可以自己結成「不做了」，但要在結果寫原因（原本的「無法完成」）。
func TestAgentCancelsAskWithReason(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	r := makeTask(t, m, sdk.TaskTypeAsk)
	if _, err := m.Close(ctx, r.ID, &sdk.TasksCloseRequest{Status: sdk.TaskStatusCancelled}, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
	got, err := m.Close(ctx, r.ID, &sdk.TasksCloseRequest{Status: "failed", Result: "查不到資料"}, "code")
	must(t, err)
	if got.Status != sdk.TaskStatusCancelled || got.Result != "查不到資料" {
		t.Fatal(got)
	}
}

const legacyHeader = "---\nschema: 1\nkind: task\nid: %s\ncreated_at: 2026-10-01T09:00:00+08:00\nupdated_at: 2026-10-20T09:00:00+08:00\nby: code\n"

// 家的遷移：vault/tasks（含 archive）每張舊狀態的單改寫成新狀態、拿掉 site；補的欄位讓舊版也讀得懂；重跑不再改。
func TestMigrateLegacyStatusesRewritesFiles(t *testing.T) {
	home := t.TempDir()
	write := func(rel, id, fields string) string {
		t.Helper()
		p := filepath.Join(home, "vault", "tasks", filepath.FromSlash(rel))
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(strings.Replace(legacyHeader, "%s", id, 1)+fields+"---\n內文\n"), 0o644))
		return p
	}
	review := write("SEC-1.md", "SEC-1", "title: 交付\ntype: task\nagent: code\nstatus: review\nwaiting_since: 2026-10-19T09:00:00+08:00\nnudges: 0\nresult: 好了\nverify: verified\nverify_how: 測試\nproposed: done\nsite: office\n")
	shelved := write("SEC-2.md", "SEC-2", "title: 擱著\ntype: task\nagent: code\nstatus: shelved\nnudges: 0\nresult: 先不做\n")
	failed := write("archive/2026-10/SEC-3.md", "SEC-3", "title: 失敗\ntype: task\nagent: code\nstatus: failed\nnudges: 0\nresult: 壞了\nproposed: failed\nclosed_at: 2026-10-02T09:00:00+08:00\naccepted_by: owner(web)\naccepted_at: 2026-10-02T09:00:00+08:00\n")
	doing := write("SEC-4.md", "SEC-4", "title: 做著\ntype: task\nagent: code\nstatus: doing\nnudges: 0\n")
	before, _ := os.ReadFile(doing)

	must(t, MigrateLegacyStatuses(home))
	read := func(p string) map[string]any {
		t.Helper()
		data, err := os.ReadFile(p)
		must(t, err)
		r, _, err := vault.Decode(data)
		must(t, err)
		if r.Body != "內文" {
			t.Fatalf("內文不見了：%q", r.Body)
		}
		return r.Fields
	}
	if f := read(review); f["status"] != "waiting_user" || f["ask"] != reviewAsk || f["proposed"] != "done" || f["site"] != nil {
		t.Fatal(f)
	}
	if f := read(shelved); f["status"] != "cancelled" || f["result"] != "擱置：先不做" || f["closed_at"] != "2026-10-20T09:00:00+08:00" || f["accepted_by"] != LegacyAcceptedBy || f["accepted_at"] != f["closed_at"] {
		t.Fatal(f)
	}
	if f := read(failed); f["status"] != "cancelled" || f["result"] != "無法完成：壞了" || f["proposed"] != "cancelled" || f["accepted_by"] != "owner(web)" {
		t.Fatal(f)
	}
	if after, _ := os.ReadFile(doing); !bytes.Equal(before, after) {
		t.Fatal("新狀態的單不該被改寫")
	}
	// 轉完的單每一張都過得了新的 schema（舊版要的 ask、closed_at、accepted_* 也都在）。
	sc := taskSpec()
	for _, p := range []string{review, shelved, failed} {
		data, _ := os.ReadFile(p)
		r, _, err := vault.Decode(data)
		must(t, err)
		if err := sc.Validate(r); err != nil {
			t.Fatal(p, err)
		}
	}
	// 重跑：不再改。
	snap := map[string][]byte{}
	for _, p := range []string{review, shelved, failed, doing} {
		snap[p], _ = os.ReadFile(p)
	}
	must(t, MigrateLegacyStatuses(home))
	for p, b := range snap {
		if after, _ := os.ReadFile(p); !bytes.Equal(b, after) {
			t.Fatal("重跑改了 " + p)
		}
	}
	// 沒有 vault/tasks 的家：什麼都不做、不算錯。
	must(t, MigrateLegacyStatuses(t.TempDir()))
}
