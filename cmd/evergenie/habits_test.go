package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// buildPackageBinaries 把官方附程式套件的行程編到同一個暫存資料夾（核心找發版附的執行檔：EVERGENIE_PACKAGE_BIN_DIR）。
func buildPackageBinaries(t *testing.T, ids ...string) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	dir := t.TempDir()
	for _, id := range ids {
		cmd := exec.Command(goBin, "build", "-o", filepath.Join(dir, "evergenie-"+id), "github.com/minglight/evergenie/packages/official/"+id+"/cmd/evergenie-"+id)
		cmd.Dir = filepath.Join("..", "..")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("編 %s 行程失敗：%v\n%s", id, err, out)
		}
	}
	return dir
}

// buildHabitsBinary 編 habits 套件的行程。
func buildHabitsBinary(t *testing.T) string { return buildPackageBinaries(t, "habits") }

func waitUntil(t *testing.T, what string, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等不到：%s（%s）", what, timeout)
}

// TestHabitsPackageRunsOnFixtureHome：真 serve＋fixture 家：官方 habits 套件被編譯好的行程啟動、報到、狀態 running；
// 用 data add 種一個習慣和兩週的打卡，幾秒內 habits.status 與 habits.wallet 出現、earned_points = 1，並通知一次；
// fixture 家是新家：第一個習慣約好另外送起手禮 1 點（HAB-20），錢包共 2 點、起手禮也通知一次；
// 這個測試家只裝了習慣、沒有衣櫃套件，所以兩則通知都連回習慣頁、不提展示間（HAB-9；衣櫃開著時連展示間由 e2e closet-habits.spec 驗）；
// 再打一筆撤銷後本週進度回去（程式去抖重算），資料種類註冊完整，停用的欄位被擋。
// Guards: REQ-124
func TestHabitsPackageRunsOnFixtureHome(t *testing.T) {
	t.Setenv("EVERGENIE_PACKAGE_BIN_DIR", buildHabitsBinary(t))
	_, sock := startFixtureAPIWith(t, func(dir string) { copyOfficialPackage(t, dir, "habits") })
	t.Setenv("EVERGENIE_AGENT", "code")

	kinds := hgOK(t, sock, "data", "kinds")
	for _, want := range []string{"habits.habit", "H-<數字>", "habits.checkin", "C-<數字>", "habits.status", "habits.wallet", "enum(daily|weekly)", "enum(cumulative|streak)"} {
		if !strings.Contains(kinds, want) {
			t.Errorf("data kinds 少了「%s」：\n%s", want, kinds)
		}
	}
	// 缺必填被擋；cadence 不在列舉裡被擋。
	if code, _, errOut := hg(t, sock, "", "data", "add", "habits.habit", "--json", `{"name":"x","agent":"code","cadence":"monthly","mode":"cumulative","start":"2026-10-05","status":"active"}`); code == 0 || !strings.Contains(errOut, "cadence") {
		t.Fatalf("cadence 不合要被擋：%d %q", code, errOut)
	}
	if code, _, errOut := hg(t, sock, "", "data", "add", "habits.checkin", "--json", `{"date":"2026-10-05"}`); code == 0 || !strings.Contains(errOut, "habit") {
		t.Fatalf("缺 habit 要被擋：%d %q", code, errOut)
	}

	// 行程起來：報到後狀態是 running。
	pkgs := sdk.NewPackageClient(sdk.Dial(sock))
	waitUntil(t, "habits 套件 running", 20*time.Second, func() bool {
		r, err := pkgs.Get(context.Background(), &sdk.PackageGetRequest{ID: "habits"})
		if err != nil {
			t.Fatal(err)
		}
		if r.Package.State == sdk.PackageStateFailed || r.Package.State == sdk.PackageStateRejected {
			t.Fatalf("habits 套件壞了：%s %s", r.Package.State, r.Package.Error)
		}
		return r.Package.State == sdk.PackageStateRunning
	})

	// 一個每週 1 次的習慣，從前天算起兩週前的那個週一開始；前兩週各打一次卡 → 兩週達標 → 1 點。
	now := time.Now()
	monday := now.AddDate(0, 0, -((int(now.Weekday())+6)%7)-14)
	d1, d2 := monday.Format("2006-01-02"), monday.AddDate(0, 0, 7).Format("2006-01-02")
	habit := `{"name":"重訓","agent":"code","cadence":"weekly","target":1,"mode":"cumulative","start":"` + d1 + `","status":"active"}`
	id := strings.TrimSpace(hgOK(t, sock, "data", "add", "habits.habit", "--json", habit))
	// 編號不一定從 1 開始（vault 才檢查得到的錯已經領了號）。
	if !strings.HasPrefix(id, "H-") {
		t.Fatalf("習慣編號：%q", id)
	}
	sid := strings.ToLower(id)
	for _, d := range []string{d1, d2} {
		hgOK(t, sock, "data", "add", "habits.checkin", "--json", `{"habit":"`+id+`","date":"`+d+`","source":"manual","key":"manual:`+id+`:`+d+`:"}`)
	}

	var status map[string]any
	waitUntil(t, "habits.status 算出 1 點", 20*time.Second, func() bool {
		code, out, _ := hg(t, sock, "", "data", "get", "habits.status", sid, "--json")
		if code != 0 {
			return false
		}
		status = jsonLine(t, out)["fields"].(map[string]any)
		return status["earned_points"] == float64(1)
	})
	if status["habit"] != id || status["achieved_weeks"] != float64(2) || status["streak_weeks"] != float64(2) || status["next_need_weeks"] != float64(4) {
		t.Fatalf("status：%v", status)
	}
	// wallet 在 status 之後才寫：等它也算好。
	var wallet map[string]any
	waitUntil(t, "habits.wallet 算出 2 點（習慣 1 點＋起手禮 1 點）", 20*time.Second, func() bool {
		code, out, _ := hg(t, sock, "", "data", "get", "habits.wallet", "main", "--json")
		if code != 0 {
			return false
		}
		wallet = jsonLine(t, out)["fields"].(map[string]any)
		return wallet["earned"] == float64(2)
	})
	if wallet["spent"] != float64(0) || wallet["points"] != float64(2) || wallet["starter"] != "given" || wallet["starter_habit"] != id {
		t.Fatalf("wallet：%v", wallet)
	}
	// 賺到點通知一次（分類 done、標題「賺到 1 點」）。
	// 通知在錢包寫好之後才送：等它出現，再確認只有一則。
	titled := func(title string) []sdk.Notification {
		list, err := sdk.NewNotifyClient(sdk.Dial(sock)).List(context.Background(), &sdk.NotifyListRequest{})
		if err != nil {
			t.Fatal(err)
		}
		var out []sdk.Notification
		for _, n := range list.Notifications {
			if n.Title == title {
				out = append(out, n)
			}
		}
		return out
	}
	waitUntil(t, "賺到點的通知", 10*time.Second, func() bool { return len(titled("賺到 1 點")) > 0 })
	time.Sleep(500 * time.Millisecond)
	got := titled("賺到 1 點")
	if len(got) != 1 {
		t.Fatalf("賺到點的通知 %d 則", len(got))
	}
	// fixture 家沒有衣櫃：點數換不到衣服（錢包 outfits_ready = false，HAB-23），通知不提換衣服、按鈕到習慣頁（HAB-9）。
	if wallet["outfits_ready"] != false {
		t.Errorf("沒有衣櫃的家 outfits_ready 要是 false：%v", wallet)
	}
	toHabits := func(n sdk.Notification) bool {
		return n.Link != nil && n.Link.Label == "到習慣頁" && n.Link.URL == "/p/habits/list"
	}
	if n := got[0]; n.Category != sdk.CategoryDone || n.Agent != "code" || !strings.Contains(n.Context, "重訓 達標累積 2 週，現在有 2 點") || strings.Contains(n.Context, "換衣服") || !toHabits(n) {
		t.Errorf("通知內容：%+v（按鈕 %+v）", n, n.Link)
	}
	if gift := titled("送你 1 點"); len(gift) != 1 || !strings.Contains(gift[0].Context, "約好第一個習慣「重訓」，送你 1 點，之後有衣服可以換時用得到。") || strings.Contains(gift[0].Context, "展示間") || !toHabits(gift[0]) {
		t.Errorf("起手禮的通知：%+v", gift)
	}

	// 今天打卡 → 本週進度變成 1；撤銷 → 回到 0（程式 watch 到變更後去抖重算）。
	today := now.Format("2006-01-02")
	hgOK(t, sock, "data", "add", "habits.checkin", "--json", `{"habit":"`+id+`","date":"`+today+`","source":"manual","key":"manual:`+id+`:`+today+`:"}`)
	thisWeekDone := func() any {
		out := hgOK(t, sock, "data", "get", "habits.status", sid, "--json")
		return jsonLine(t, out)["fields"].(map[string]any)["this_week"].(map[string]any)["done"]
	}
	waitUntil(t, "打卡之後本週進度是 1", 20*time.Second, func() bool { return thisWeekDone() == float64(1) })
	hgOK(t, sock, "data", "add", "habits.checkin", "--json", `{"habit":"`+id+`","date":"`+today+`","source":"manual","undo":true}`)
	waitUntil(t, "撤銷之後本週進度回到 0", 20*time.Second, func() bool { return thisWeekDone() == float64(0) })
}

// TestHabitsGuideReachesSecretaries：habits 套件經 prompt_append 給秘書的補充（HAB-11、HAB-21）——新家每位秘書的指令都有「習慣怎麼約」
// 與「第一次介紹」；秘書寫了 habits.welcome main（第二次寫被擋，第二位秘書就不會再講）之後下次開對話就沒有「第一次介紹」；
// 套件停用兩段都沒有。升級前就在用的家（錢包在、沒有 starter）：程式一起來就記「不用介紹」，秘書的指令裡沒有那段。
func TestHabitsGuideReachesSecretaries(t *testing.T) {
	t.Setenv("EVERGENIE_PACKAGE_BIN_DIR", buildHabitsBinary(t))
	const guide, welcome = "主人可以跟你約習慣", "這個家還沒有人跟主人介紹過習慣"
	waitRunning := func(sock string) {
		pkgs := sdk.NewPackageClient(sdk.Dial(sock))
		waitUntil(t, "habits 套件 running", 20*time.Second, func() bool {
			r, err := pkgs.Get(context.Background(), &sdk.PackageGetRequest{ID: "habits"})
			if err != nil {
				t.Fatal(err)
			}
			return r.Package.State == sdk.PackageStateRunning
		})
	}

	dir := copyFixtureHome(t, false)
	copyOfficialPackage(t, dir, "habits")
	m, sock, _ := serveIsolation(t, dir)
	waitRunning(sock)
	instructions := func(agent string) string {
		got, err := m.agent.Instructions(context.Background(), agent, agent)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, agent := range []string{"code", "life"} {
		if got := instructions(agent); !strings.Contains(got, guide) || !strings.Contains(got, welcome) || !strings.Contains(got, "## 套件補充") {
			t.Fatalf("新家 %s 的指令要有習慣的兩段補充：\n%s", agent, got)
		}
	}
	// 錢包已經寫好（waiting），還沒有人介紹：程式沒有替新家記「不用介紹」。
	waitUntil(t, "錢包記起手禮還沒送", 20*time.Second, func() bool {
		code, out, _ := hg(t, sock, "", "data", "get", "habits.wallet", "main", "--json")
		return code == 0 && jsonLine(t, out)["fields"].(map[string]any)["starter"] == "waiting"
	})
	if code, _, _ := hg(t, sock, "", "data", "get", "habits.welcome", "main"); code == 0 {
		t.Fatal("新家不該先記「不用介紹」")
	}

	// 第一位秘書介紹了、記下來；第二位要記時被擋（照補充說的就什麼都不提）。
	t.Setenv("EVERGENIE_AGENT", "code")
	hgOK(t, sock, "data", "add", "habits.welcome", "--id", "main", "--json", `{"status":"done","agent":"code"}`)
	t.Setenv("EVERGENIE_AGENT", "life")
	if code, _, errOut := hg(t, sock, "", "data", "add", "habits.welcome", "--id", "main", "--json", `{"status":"done","agent":"life"}`); code == 0 || !strings.Contains(errOut, "已經有資料") {
		t.Fatalf("第二位秘書要被擋：%d %q", code, errOut)
	}
	if got := instructions("life"); !strings.Contains(got, guide) || strings.Contains(got, welcome) {
		t.Fatalf("介紹過之後只留習慣怎麼約：\n%s", got)
	}

	// 主人停用習慣：兩段都沒有。
	t.Setenv("EVERGENIE_AGENT", "")
	hgOK(t, sock, "packages", "disable", "habits")
	if got := instructions("code"); strings.Contains(got, guide) || strings.Contains(got, welcome) {
		t.Fatalf("停用之後不帶習慣的補充：\n%s", got)
	}

	// 升級前就在用的家。
	old := copyFixtureHome(t, false)
	copyOfficialPackage(t, old, "habits")
	writeTree(t, old, map[string]string{"vault/pkg/habits/wallet/main.md": "---\nschema: 1\nid: main\ncreated_at: 2026-10-26T12:00:00+08:00\nupdated_at: 2026-11-01T12:00:00+08:00\nby: habits\nearned: 0\nspent: 0\npoints: 0\nledger: []\n---\n"})
	m, sock, _ = serveIsolation(t, old)
	waitRunning(sock)
	waitUntil(t, "升級前的家記「不用介紹」", 20*time.Second, func() bool {
		code, out, _ := hg(t, sock, "", "data", "get", "habits.welcome", "main", "--json")
		return code == 0 && jsonLine(t, out)["fields"].(map[string]any)["status"] == "skipped"
	})
	if got := instructions("code"); !strings.Contains(got, guide) || strings.Contains(got, welcome) {
		t.Fatalf("升級前的家不帶第一次介紹：\n%s", got)
	}
}
