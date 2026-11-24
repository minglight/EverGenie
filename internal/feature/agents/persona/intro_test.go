package persona

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// 新角色來打招呼（DEV-115、PRS-13）。

type fakeNotifier struct {
	sent []*sdk.Notification
	fail error
}

func (f *fakeNotifier) Notify(_ context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	f.sent = append(f.sent, n)
	return n, nil
}

const lunaCard = `---
schema: 1
name: 露娜
---
## 個性
- 一句話：月光下安靜的圖書館員，話不多但每句都算數。
- 原型：溫柔 80%

## 說話
- 語氣：輕聲。
- 開場白：「晚安……你來啦。」；早上：「早。」
`

func taipei(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// 預定時間落在家的時區 10:00–21:00 之間、而且在現在之後；過了 20:00 就排隔天。亂數取最小、最大與真的亂數都要在窗內。
func TestIntroTimeFallsInDaytimeWindowOrNextDay(t *testing.T) {
	loc := taipei(t)
	old := introRand
	t.Cleanup(func() { introRand = old })
	day := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, loc) }
	cases := []struct {
		name       string
		now        time.Time
		from, till time.Time // 允許的範圍 [from, till)
	}{
		{"早上還沒到窗：當天 10 點起", day(4, 7, 30), day(4, 10, 0), day(4, 21, 0)},
		{"下午：現在之後到 21 點", day(4, 15, 30), day(4, 15, 31), day(4, 21, 0)},
		{"19:59 還排得進當天", day(4, 19, 59), day(4, 20, 0), day(4, 21, 0)},
		{"20:00 排隔天", day(4, 20, 0), day(5, 10, 0), day(5, 21, 0)},
		{"深夜排隔天", day(4, 23, 50), day(5, 10, 0), day(5, 21, 0)},
		{"過了午夜就是當天", day(5, 0, 10), day(5, 10, 0), day(5, 21, 0)},
	}
	for _, c := range cases {
		for _, pick := range []func(int) int{func(int) int { return 0 }, func(n int) int { return n - 1 }, old} {
			introRand = pick
			for i := 0; i < 20; i++ {
				got := introAt(c.now.UTC(), loc)
				if got.Before(c.from) || !got.Before(c.till) || !got.After(c.now) || got.Second() != 0 {
					t.Fatalf("%s：%v 不在 [%v, %v)", c.name, got.In(loc), c.from, c.till)
				}
			}
		}
	}
	// 有日光節約的時區，切換那天也照牆上的鐘點：10:00 起、21:00 前。
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("沒有時區資料")
	}
	introRand = func(int) int { return 0 }
	if got := introAt(time.Date(2026, 3, 8, 7, 0, 0, 0, ny), ny).In(ny); got.Hour() != 10 || got.Minute() != 0 {
		t.Fatalf("日光節約那天的窗起點要是 10:00：%v", got)
	}
	introRand = func(n int) int { return n - 1 }
	if got := introAt(time.Date(2026, 3, 8, 7, 0, 0, 0, ny), ny).In(ny); got.Hour() != 20 || got.Minute() != 59 {
		t.Fatalf("日光節約那天的最後一分鐘要是 20:59：%v", got)
	}
}

// 沒有 intro 時用「開場白」的第一句＋「我是<name>，<一句話>。」組；有 intro 就用它；兩樣都沒有只說「我是<name>。」。
func TestIntroComposedFromCardWhenNoIntroField(t *testing.T) {
	if got := composeIntro("露娜", strings.SplitN(lunaCard, "---\n", 3)[2]); got != "晚安……你來啦。我是露娜，月光下安靜的圖書館員，話不多但每句都算數。" {
		t.Fatalf("組出來的：%q", got)
	}
	if got := composeIntro("阿光", "## 說話\n- 開場白：嗨～今天也一起加油\n"); got != "嗨～今天也一起加油。我是阿光。" {
		t.Fatalf("沒有一句話、開場白沒引號：%q", got)
	}
	if got := composeIntro("小白", "隨便寫的內文\n"); got != "我是小白。" {
		t.Fatalf("什麼都沒有：%q", got)
	}

	home := homefixture.New(t).Dir
	m := New(Deps{})
	m.SetHome(home)
	write := func(slug, data string) {
		t.Helper()
		must(t, os.MkdirAll(filepath.Join(home, "personas"), 0o755))
		must(t, os.WriteFile(filepath.Join(home, "personas", slug+".md"), []byte(data), 0o644))
	}
	write("luna", lunaCard)
	write("sol", "---\nschema: 1\nname: 索爾\nintro: 嘿！我是索爾，太陽一出來我就醒了。要曬棉被、排行程都找我。\n---\n## 個性\n- 一句話：太陽\n")
	write("legacy", "+++\nname = \"老派\"\n+++\n## 說話\n- 開場白：「哈囉。」\n")
	if n, ok := m.introNotice("sol"); !ok || n.Context != "嘿！我是索爾，太陽一出來我就醒了。要曬棉被、排行程都找我。" {
		t.Fatalf("有 intro 就照用：%+v", n)
	}
	if n, ok := m.introNotice("luna"); !ok || !strings.HasPrefix(n.Context, "晚安……你來啦。我是露娜") {
		t.Fatalf("沒有 intro 用組的：%+v", n)
	}
	if n, ok := m.introNotice("legacy"); !ok || n.Context != "哈囉。我是老派。" {
		t.Fatalf("舊 TOML 檔頭也讀得到：%+v", n)
	}
	if _, ok := m.introNotice("nobody"); ok {
		t.Fatal("卡不在就不送")
	}
}

// 招呼是主人訊息：opening、秘書那道、她的頭像與名字，按鈕開到「設定 → 角色庫」那張卡。
func TestIntroNoticeCarriesAvatarAndLibraryButton(t *testing.T) {
	home := homefixture.New(t).Dir
	m := New(Deps{})
	m.SetHome(home)
	must(t, os.MkdirAll(filepath.Join(home, "personas"), 0o755))
	must(t, os.WriteFile(filepath.Join(home, "personas", "luna.md"), []byte(lunaCard), 0o644))
	n, ok := m.introNotice("luna")
	if !ok {
		t.Fatal("要組得出來")
	}
	if n.Category != sdk.CategoryOpening || n.Lane != sdk.NotifyLaneAgent || n.Title != "露娜 來打招呼" || n.Speaker != "露娜" ||
		n.Image != "/avatars/persona/luna.png" || len(n.Options) != 0 || n.Source != "" {
		t.Fatalf("訊息欄位：%+v", n)
	}
	if n.Link == nil || n.Link.Label != "去角色頁看看" || n.Link.URL != "/personas/luna" {
		t.Fatalf("按鈕：%+v", n.Link)
	}
}

// 待送清單存檔：重開（新的 Module）讀得回來；還沒到時間不送，到了（或早就過了）第一輪就送，送完拿掉，不重送；
// 送失敗留著下一輪再試；卡不在了直接拿掉。同一位排兩次只算一次。
func TestQueuedIntrosSurviveRestartAndSendOnce(t *testing.T) {
	h := homefixture.New(t)
	loc := taipei(t)
	old := introRand
	t.Cleanup(func() { introRand = old })
	introRand = func(int) int { return 30 } // 窗起點後 30 分鐘
	must(t, os.MkdirAll(filepath.Join(h.Dir, "personas"), 0o755))
	must(t, os.WriteFile(filepath.Join(h.Dir, "personas", "luna.md"), []byte(lunaCard), 0o644))
	syncAt := time.Date(2026, 11, 4, 9, 0, 0, 0, loc) // 早上同步 → 10:30 送
	must(t, queueIntros(h.Dir, []string{"luna", "ghost"}, syncAt))
	must(t, queueIntros(h.Dir, []string{"luna"}, syncAt.Add(time.Hour))) // 同一位不重排

	var q introQueue
	data, err := os.ReadFile(introPath(h.Dir))
	must(t, err)
	must(t, json.Unmarshal(data, &q))
	if len(q.Intros) != 2 || !q.Intros[0].At.Equal(time.Date(2026, 11, 4, 10, 30, 0, 0, loc)) {
		t.Fatalf("清單：%+v", q.Intros)
	}

	boot := func(now time.Time, n Notifier) *Module {
		clock := &platform.FakeClock{}
		clock.Set(now)
		m := New(Deps{Clock: clock})
		m.SetHome(h.Dir)
		if n != nil {
			m.SetNotifier(n)
		}
		return m
	}
	// 還沒到時間：不送。
	early := &fakeNotifier{}
	must(t, boot(syncAt.Add(30*time.Minute), early).sendDueIntros(context.Background()))
	if len(early.sent) != 0 {
		t.Fatalf("還沒到不送：%d", len(early.sent))
	}
	// 通知還沒接上：清單不動（ghost 在上一輪已因卡不在拿掉，剩 luna）。
	before, err := os.ReadFile(introPath(h.Dir))
	must(t, err)
	must(t, boot(syncAt.Add(5*time.Hour), nil).sendDueIntros(context.Background()))
	after, err := os.ReadFile(introPath(h.Dir))
	must(t, err)
	if string(before) != string(after) || !strings.Contains(string(after), `"luna"`) || strings.Contains(string(after), "ghost") {
		t.Fatalf("通知沒接上時清單不動：\n%s\n→\n%s", before, after)
	}
	// 送失敗：留著。
	broken := &fakeNotifier{fail: errors.New("notify 掛了")}
	if err := boot(syncAt.Add(5*time.Hour), broken).sendDueIntros(context.Background()); err == nil {
		t.Fatal("送失敗要回錯（寫進 jobs log）")
	}
	// 重開機、早就過了時間：第一輪就送 luna（ghost 的卡不在，拿掉），再跑一輪不重送。
	ok := &fakeNotifier{}
	m := boot(syncAt.Add(30*time.Hour), ok)
	must(t, m.sendDueIntros(context.Background()))
	must(t, m.sendDueIntros(context.Background()))
	must(t, boot(syncAt.Add(31*time.Hour), ok).sendDueIntros(context.Background()))
	if len(ok.sent) != 1 || ok.sent[0].Speaker != "露娜" {
		t.Fatalf("只送一次：%+v", ok.sent)
	}
	data, err = os.ReadFile(introPath(h.Dir))
	must(t, err)
	q = introQueue{}
	must(t, json.Unmarshal(data, &q))
	if len(q.Intros) != 0 {
		t.Fatalf("送完、卡不在的都要拿掉：%+v", q.Intros)
	}

	// 排進來超過 7 天還沒送出去（通知一直失敗）：放棄、拿掉，不送。
	must(t, queueIntros(h.Dir, []string{"luna"}, syncAt))
	late := &fakeNotifier{}
	must(t, boot(syncAt.Add(8*24*time.Hour), late).sendDueIntros(context.Background()))
	data, err = os.ReadFile(introPath(h.Dir))
	must(t, err)
	q = introQueue{}
	must(t, json.Unmarshal(data, &q))
	if len(late.sent) != 0 || len(q.Intros) != 0 {
		t.Fatalf("超過 7 天放棄：sent %d、清單 %+v", len(late.sent), q.Intros)
	}
}

// 指令與定期再試的同步都走 syncPack：有新角色就排進待送清單，沒有就不建清單；定時工作 personas.intros 每分鐘看一次。
func TestPackSyncQueuesIntrosForNewCards(t *testing.T) {
	home := homefixture.New(t).Dir
	old := packSync
	t.Cleanup(func() { packSync = old })
	packSync = func(_ context.Context, o personapack.SyncOptions) (*personapack.SyncResult, error) {
		return &personapack.SyncResult{Status: personapack.StatusSynced}, nil
	}
	now := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	_, err := syncPack(context.Background(), personapack.SyncOptions{Home: home, Now: now})
	must(t, err)
	if _, err := os.Stat(introPath(home)); !os.IsNotExist(err) {
		t.Fatal("沒有新角色就不建清單")
	}
	packSync = func(_ context.Context, o personapack.SyncOptions) (*personapack.SyncResult, error) {
		return &personapack.SyncResult{Status: personapack.StatusSynced, NewCards: []string{"luna"}}, nil
	}
	_, err = syncPack(context.Background(), personapack.SyncOptions{Home: home, Now: now})
	must(t, err)
	data, err := os.ReadFile(introPath(home))
	if err != nil || !strings.Contains(string(data), `"slug": "luna"`) {
		t.Fatalf("要排進清單：%v %s", err, data)
	}
	// 檔案換進來、紀錄還沒寫完（回錯誤）也要排：下一次同步不會再算她新（PRS-15）。
	packSync = func(_ context.Context, o personapack.SyncOptions) (*personapack.SyncResult, error) {
		return &personapack.SyncResult{Status: personapack.StatusFailed, Installed: []string{"cards/sol.md"}, NewCards: []string{"sol"}}, errors.New("紀錄待補")
	}
	if _, err := syncPack(context.Background(), personapack.SyncOptions{Home: home, Now: now}); err == nil {
		t.Fatal("同步的錯誤照樣回")
	}
	if data, _ := os.ReadFile(introPath(home)); !strings.Contains(string(data), `"slug": "sol"`) {
		t.Fatalf("紀錄待補也要排招呼：%s", data)
	}
	m := New(Deps{})
	var found bool
	for _, j := range m.Jobs() {
		if j.Name == "personas.intros" {
			found = j.Every == time.Minute && j.Run != nil
		}
	}
	if !found {
		t.Fatal("要登記 personas.intros，每分鐘一次")
	}
}
