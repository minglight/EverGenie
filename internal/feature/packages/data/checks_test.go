package data

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault"
)

type badVault struct {
	VaultStore
	bad []vault.InvalidRecord
}

func (v badVault) InvalidRecords(context.Context) ([]vault.InvalidRecord, error) { return v.bad, nil }

// data.invalid：每筆壞資料一條紅字（種類、編號、路徑、欄位、原因），超過 20 筆併成「另外還有 n 筆」；都符合給綠字；
// vault 沒有掃描能力就不登記。
// Guards: REQ-117
func TestInvalidRecordsCheck(t *testing.T) {
	var bad []vault.InvalidRecord
	for i := 0; i < 22; i++ {
		bad = append(bad, vault.InvalidRecord{Kind: "task", ID: fmt.Sprintf("SEC-%d", i), Path: "vault/tasks/x.md", Field: "status", Reason: "不認得"})
	}
	cs := New(Deps{Vault: badVault{bad: bad}}).Checks()
	if len(cs) != 1 || cs[0].Name != "data.invalid" {
		t.Fatalf("登記：%+v", cs)
	}
	got := cs[0].RunAll(context.Background())
	if len(got) != 21 || got[0].Status != app.StatusError || !strings.Contains(got[0].Message, "task SEC-0（vault/tasks/x.md），欄位 status：不認得") ||
		!strings.Contains(got[20].Message, "另外還有 2 筆") {
		t.Fatalf("逐條列 20 筆，其餘併一條：%+v", got)
	}
	ok := New(Deps{Vault: badVault{}}).Checks()[0].RunAll(context.Background())
	if len(ok) != 1 || ok[0].Status != app.StatusOK {
		t.Fatalf("都符合：%+v", ok)
	}
	if len(New(Deps{}).Checks()) != 0 {
		t.Fatal("沒有掃描能力不登記")
	}
}
