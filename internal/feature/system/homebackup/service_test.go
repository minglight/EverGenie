package homebackup

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

var (
	ownerActor = &sdk.Actor{Name: "owner(web)"}
	agentActor = &sdk.Actor{Name: "life"}
)

// 秘書可以做備份、看狀態、預覽；還原只有主人能做。主人還原：準備好之後請服務重開，重開時才換。
func TestServiceBackupThenOwnerRestores(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	m, clock, _, restarts := newModule(t, home, testNow)
	s := &service{m: m}
	ctx := context.Background()

	made, err := s.Create(ctx, &sdk.BackupCreateRequest{By: agentActor})
	if err != nil {
		t.Fatal(err)
	}
	if made.File.Download != "/home-backup/manual/"+made.File.Name || len(made.Sections) == 0 {
		t.Fatalf("create response: %+v", made)
	}
	st, err := s.GetStatus(ctx, &sdk.BackupGetStatusRequest{})
	if err != nil || len(st.Files) != 1 || !st.LastAt.Equal(made.File.CreatedAt) || st.Restoring || st.KeepCount != KeepCount {
		t.Fatalf("status: %+v %v", st, err)
	}
	pre, err := s.Preview(ctx, &sdk.BackupPreviewRequest{Kind: made.File.Kind, Name: made.File.Name})
	if err != nil || pre.HomeName != "h" || pre.OtherMachine || pre.Problem != "" || pre.Files == 0 || strings.Join(pre.Installed, ",") != "shared-pkg" {
		t.Fatalf("preview: %+v %v", pre, err)
	}
	if _, err := s.Preview(ctx, &sdk.BackupPreviewRequest{Kind: "manual", Name: "../../home.toml"}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("path traversal in name: %v", err)
	}

	req := &sdk.BackupRestoreRequest{Kind: made.File.Kind, Name: made.File.Name, By: agentActor}
	if _, err := s.Restore(ctx, req); codeOf(err) != sdk.ErrorCodeForbidden || HasPending(home) {
		t.Fatalf("a secretary must not restore: %v", err)
	}

	write(t, filepath.Join(home, "vault", "tasks", "T-9.md"), "備份之後加的\n")
	clock.Advance(1e9)
	req.By = ownerActor
	res, err := s.Restore(ctx, req)
	if err != nil || !res.Restarting || res.Safety == "" || *restarts != 1 {
		t.Fatalf("owner restore: %+v %v restarts=%d", res, err, *restarts)
	}
	if !exists(filepath.Join(home, "vault", "tasks", "T-9.md")) {
		t.Fatal("home must not change before the restart")
	}
	if st, _ := s.GetStatus(ctx, &sdk.BackupGetStatusRequest{}); !st.Restoring {
		t.Fatal("status should say a restore is waiting")
	}
	if _, err := s.Create(ctx, &sdk.BackupCreateRequest{By: agentActor}); codeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("no new backups while a restore waits: %v", err)
	}
	// 重開：serve 啟動時換上去。
	if r, err := ApplyPending(ApplyOptions{Home: home}); err != nil || !r.OK {
		t.Fatalf("apply: %+v %v", r, err)
	}
	if exists(filepath.Join(home, "vault", "tasks", "T-9.md")) {
		t.Fatal("restore should have replaced everything")
	}
	st, _ = s.GetStatus(ctx, &sdk.BackupGetStatusRequest{})
	if st.Restoring || st.LastRestore == nil || !st.LastRestore.OK || st.LastRestore.Safety != res.Safety {
		t.Fatalf("status after restore: %+v", st)
	}
	// 還原的結果通知主人一次。
	m2, clock2, notes, _ := newModule(t, home, testNow)
	m2.tick(ctx, clock2.Now())
	m2.tick(ctx, clock2.Now())
	if notes.count() != 1 || !strings.Contains(notes.sent[0].Title, "還原完成") {
		t.Fatalf("restore result notices: %+v", notes.sent)
	}
}

// codeOf 讀 API 回的錯誤碼（service 回的是 *sdk.Error）。
func codeOf(err error) sdk.ErrorCode {
	var e *sdk.Error
	if errors.As(err, &e) && e != nil {
		return e.Code
	}
	return ""
}

// 模組交出的服務掛得上 API（sdk 認得 backup 這個服務、每個方法都有實作）。
func TestRoutesMount(t *testing.T) {
	m, _, _, _ := newModule(t, fakeHome(t, "h", true, 18001), testNow)
	for _, r := range m.Routes() {
		if err := sdk.Mount(http.NewServeMux(), r.Service, r.Impl); err != nil {
			t.Fatal(err)
		}
	}
}
