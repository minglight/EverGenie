package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 改角色代號（home tidy、personas promote）的紀錄還沒收尾時，home relayout 搬家與退回都要擋下（DEV-154）：
// 中間換佈局，改代號就退不回去了。收尾（紀錄改名成備份）之後就可以。
func TestRelayoutWaitsForPendingRecode(t *testing.T) {
	home, state := newRelayoutDirs(t)
	tidy := TidyJournalPath(home)
	writeFile(t, tidy, "{}\n")
	p, err := PlanRelayout(home, state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(p.Problems, ""), "settings/home-tidy.json") {
		t.Fatalf("有 home tidy 紀錄要擋搬家：%v", p.Problems)
	}
	if err := os.Rename(tidy, BackupName(tidy, time.Now(), "done")); err != nil {
		t.Fatal(err)
	}
	if p, err = PlanRelayout(home, state); err != nil || len(p.Problems) > 0 {
		t.Fatalf("收尾後要能搬：%v %v", err, p.Problems)
	}
	if err := ApplyRelayout(p, time.Date(2026, 11, 13, 0, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}

	promote := filepath.Join(PromoteJournalDir(home), "my_x.json")
	writeFile(t, promote, "{}\n")
	writeFile(t, filepath.Join(PromoteJournalDir(home), "my_y.json.bak-20261101-done"), "{}\n")
	if got := PendingRecodeJournals(home); len(got) != 1 || got[0] != "settings/persona-promote/my_x.json" {
		t.Fatalf("只算還沒收尾的紀錄：%v", got)
	}
	rp, err := PlanRevert(home)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rp.Problems, ""), "settings/persona-promote/my_x.json") {
		t.Fatalf("有 personas promote 紀錄要擋退回：%v", rp.Problems)
	}
	if err := RevertRelayout(home, time.Now()); err == nil || !exists(filepath.Join(state, "runtime")) {
		t.Fatalf("擋下時不能退回：%v", err)
	}
	if err := os.Remove(promote); err != nil {
		t.Fatal(err)
	}
	if err := RevertRelayout(home, time.Now()); err != nil {
		t.Fatalf("沒有紀錄後要能退回：%v", err)
	}
}
