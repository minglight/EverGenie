package persona

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
)

// fakePackSync 換掉真的同步：記下每次的參數，照 results 依序回結果（用完之後回最後一個）。
func fakePackSync(t *testing.T, results ...*personapack.SyncResult) *[]personapack.SyncOptions {
	t.Helper()
	var mu sync.Mutex
	calls := &[]personapack.SyncOptions{}
	old := packSync
	t.Cleanup(func() { packSync = old })
	packSync = func(_ context.Context, o personapack.SyncOptions) (*personapack.SyncResult, error) {
		mu.Lock()
		defer mu.Unlock()
		*calls = append(*calls, o)
		r := results[min(len(*calls), len(results))-1]
		if r.Status == personapack.StatusFailed {
			return r, context.DeadlineExceeded
		}
		return r, nil
	}
	return calls
}

// 角色包的定期再試是 Jobs() 登記的定時工作：啟動 2 分鐘後第一次、之後每 30 秒醒一次看「到了沒」；同步用 registry 接上的指標檔路徑與這個家的時鐘。
// Guards: REQ-283, PRS-6
func TestJobsRetryPack(t *testing.T) {
	home := homefixture.New(t).Dir
	clock := platform.NewFakeClock(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC))
	m := New(Deps{Clock: clock})
	m.SetHome(home)
	m.SetPackPointer(func(h string) string { return h + "/pointer" })
	jobs := m.Jobs()
	if len(jobs) != 3 || jobs[0].Name != "personas.pack_retry" || jobs[0].Every != packRetryTick || jobs[0].First != packRetryFirst {
		t.Fatalf("%+v", jobs)
	}
	calls := fakePackSync(t, &personapack.SyncResult{Status: personapack.StatusSynced})
	if err := jobs[0].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].Home != home || (*calls)[0].PointerPath != home+"/pointer" || !(*calls)[0].Now.Equal(clock.Now()) {
		t.Fatalf("同步參數：%+v", *calls)
	}
}

// 同步好了（或沒網路、已是最新）之後 6 小時內定時工作醒來也不做；紀錄還沒寫（套件服務還沒準備好）就 30 秒、1 分、2 分…加倍再試，
// 補上之後回到 6 小時。指令與看板經 SyncPack 同步也一樣排下一次（服務剛裝好時第一次同步沒寫成紀錄，不用等 6 小時）。
// Guards: PRS-6
func TestPackRetryQuickWhenRecordsLater(t *testing.T) {
	home := homefixture.New(t).Dir
	clock := platform.NewFakeClock(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC))
	m := New(Deps{Clock: clock})
	m.SetHome(home)
	later := &personapack.SyncResult{Status: personapack.StatusFailed, RecordsLater: true, Reason: "包裡的 2 筆紀錄這次沒寫：套件服務還沒啟動"}
	done := &personapack.SyncResult{Status: personapack.StatusSynced}
	calls := fakePackSync(t, later, later, later, done, done)
	retry := m.Jobs()[0].Run
	tick := func(d time.Duration) int {
		clock.Advance(d)
		_ = retry(context.Background())
		return len(*calls)
	}
	// 安裝程式經 API 請服務同步：紀錄還沒寫。
	if res, err := m.SyncPack(context.Background(), PackSyncOptions{}); err == nil || !res.RecordsLater {
		t.Fatalf("第一次：%v %+v", err, res)
	}
	if n := tick(29 * time.Second); n != 1 {
		t.Fatalf("30 秒還沒到不再試：%d", n)
	}
	if n := tick(time.Second); n != 2 {
		t.Fatalf("30 秒後再試：%d", n)
	}
	if n := tick(59 * time.Second); n != 2 {
		t.Fatalf("第二次之後隔 1 分鐘：%d", n)
	}
	if n := tick(time.Second); n != 3 {
		t.Fatalf("1 分鐘後再試：%d", n)
	}
	if n := tick(2 * time.Minute); n != 4 {
		t.Fatalf("2 分鐘後再試（這次補上了）：%d", n)
	}
	if n := tick(6*time.Hour - time.Second); n != 4 {
		t.Fatalf("補上之後 6 小時內不再做：%d", n)
	}
	if n := tick(time.Second); n != 5 {
		t.Fatalf("6 小時後再看一次：%d", n)
	}
}

// 同步成功過的家（有登記的版號），沒網路、驗證失敗（不是「紀錄還沒寫」）照舊 6 小時後再試，不會每 30 秒連一次網路；間隔加倍到 6 小時就不再變大。
// Guards: PRS-6
func TestPackRetryBackoff(t *testing.T) {
	home := homefixture.New(t).Dir
	clock := platform.NewFakeClock(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC))
	m := New(Deps{Clock: clock})
	m.SetHome(home)
	calls := fakePackSync(t, &personapack.SyncResult{Status: personapack.StatusFailed, Registered: "2026.11.1", Reason: "抓不到角色包的清單（連不上下載的網址）"})
	retry := m.Jobs()[0].Run
	_ = retry(context.Background())
	clock.Advance(packRetryEvery - time.Second)
	_ = retry(context.Background())
	if len(*calls) != 1 {
		t.Fatalf("沒網路 6 小時內不再試：%d", len(*calls))
	}
	m.pack.mu.Lock()
	m.pack.backoff = 4 * time.Hour
	m.scheduleNext(&personapack.SyncResult{RecordsLater: true})
	got := m.pack.backoff
	m.scheduleNext(&personapack.SyncResult{RecordsLater: true})
	again := m.pack.backoff
	m.pack.mu.Unlock()
	if got != packRetryEvery || again != packRetryEvery {
		t.Fatalf("間隔最多 6 小時：%v %v", got, again)
	}
}

// 這個家從來沒同步成功過（登記的版號是空的）時，任何原因失敗（含沒網路）都 30 秒、1 分、2 分…加倍再試，不等 6 小時；
// 補上之後回到 6 小時，之後再失敗（已經有登記的版號）也照 6 小時。
// Guards: PRS-6
func TestPackRetryQuickBeforeFirstSuccess(t *testing.T) {
	home := homefixture.New(t).Dir
	clock := platform.NewFakeClock(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC))
	m := New(Deps{Clock: clock})
	m.SetHome(home)
	offline := &personapack.SyncResult{Status: personapack.StatusFailed, Reason: "抓不到角色包的清單（連不上下載的網址）"}
	done := &personapack.SyncResult{Status: personapack.StatusSynced, Registered: "2026.11.1"}
	offlineLater := &personapack.SyncResult{Status: personapack.StatusFailed, Registered: "2026.11.1", Reason: "抓不到角色包的清單（連不上下載的網址）"}
	calls := fakePackSync(t, offline, offline, offline, done, offlineLater, offlineLater)
	retry := m.Jobs()[0].Run
	tick := func(d time.Duration) int {
		clock.Advance(d)
		_ = retry(context.Background())
		return len(*calls)
	}
	if n := tick(0); n != 1 {
		t.Fatalf("第一次：%d", n)
	}
	if n := tick(29 * time.Second); n != 1 {
		t.Fatalf("30 秒還沒到不再試：%d", n)
	}
	if n := tick(time.Second); n != 2 {
		t.Fatalf("沒同步成功過、沒網路：30 秒後再試：%d", n)
	}
	if n := tick(59 * time.Second); n != 2 {
		t.Fatalf("第二次之後隔 1 分鐘：%d", n)
	}
	if n := tick(time.Second); n != 3 {
		t.Fatalf("1 分鐘後再試：%d", n)
	}
	if n := tick(2 * time.Minute); n != 4 {
		t.Fatalf("2 分鐘後再試（這次同步好了）：%d", n)
	}
	if n := tick(6*time.Hour - time.Second); n != 4 {
		t.Fatalf("同步好之後 6 小時內不再做：%d", n)
	}
	if n := tick(time.Second); n != 5 {
		t.Fatalf("6 小時後再看一次（這次沒網路）：%d", n)
	}
	if n := tick(6*time.Hour - time.Second); n != 5 {
		t.Fatalf("同步成功過之後沒網路：照舊 6 小時內不再試：%d", n)
	}
}
