package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 單筆紀錄操作只碰 agent 擁有的種類：排程（job）的擁有者是 jobs，名冊不讀不寫。
func TestRecordOpsOnlyOwnedKinds(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	if _, err := m.Record(ctx, "job", "code/x"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("讀 job：%v", err)
	}
	if err := m.ArchiveRecord(ctx, "job", "code/x", owner); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("封存 job：%v", err)
	}
	if _, err := m.MoveRecord(ctx, "task", "SEC-1", "SEC-2", nil, owner); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("搬 task：%v", err)
	}
	if err := m.SetBody(ctx, "reminder", "R-1", "x", owner); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("寫 reminder：%v", err)
	}
}

// Records 列出名下還沒封存、agent 擁有的資料：agent 在第一個，掛在底下的（日誌…）在後，別人（life）的不列；排程不在裡面（jobs 的）。
func TestRecordsListsOwnBelongings(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	must(t, m.WriteMemory(ctx, "code", "記憶", "code"))
	must(t, m.AppendJournal(ctx, "code", "日誌"))
	must(t, m.AppendJournal(ctx, "life", "日誌"))
	rows, err := m.Records(ctx, "code")
	must(t, err)
	var got []string
	for _, r := range rows {
		got = append(got, r.Kind+"/"+r.ID)
	}
	joined := strings.Join(got, " ")
	if len(got) != 3 || got[0] != "agent/code" || got[1] != "memory/code" || !strings.HasPrefix(got[2], "journal/code/") {
		t.Fatalf("名下資料：%s", joined)
	}
	must(t, m.ArchiveRecord(ctx, "memory", "code", owner))
	if rows, _ := m.Records(ctx, "code"); len(rows) != 2 {
		t.Fatalf("封存過的不再列：%d", len(rows))
	}
}
