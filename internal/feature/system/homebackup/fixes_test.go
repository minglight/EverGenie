package homebackup

import (
	"archive/tar"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// shortDisk 讓這個測試裡「磁碟還剩多少」固定回 free（測完還原）。
func shortDisk(t *testing.T, free int64) {
	t.Helper()
	old := freeBytes
	freeBytes = func(string) (int64, bool) { return free, true }
	t.Cleanup(func() { freeBytes = old })
}

// 空間不夠就不做備份（HBK-10）：白話說要多少、剩多少，不留半個檔；夠了照做。
func TestCreateRefusesWhenDiskIsShort(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	write(t, filepath.Join(home, "media", "share", "big.bin"), strings.Repeat("x", 1<<20))
	shortDisk(t, spaceMargin) // 餘裕剛好、放不下內容
	_, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err == nil || !strings.Contains(err.Error(), "空間不夠") || !strings.Contains(err.Error(), "只剩") {
		t.Fatalf("空間不夠要拒絕並說原因：%v", err)
	}
	dir := platform.HomeBackupDir(home, sdk.HomeBackupKindManual)
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("沒做成不能留檔：%v", left)
	}
	shortDisk(t, withMargin(estimateHome(home)))
	if _, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow}); err != nil {
		t.Fatalf("空間剛好夠要做得成：%v", err)
	}
}

// 準備還原前先看空間（HBK-10）：解開備份＋留一份現況放不下就不開始，家沒動、不留暫存、也不多留一份現況。
func TestStageRefusesWhenDiskIsShort(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	archive, _ := backupThenChange(t, home)
	before := snapshot(t, home)
	shortDisk(t, spaceMargin)
	_, err := Stage(StageOptions{Home: home, Archive: archive, Now: testNow.Add(1)})
	if err == nil || !strings.Contains(err.Error(), "空間不夠還原") || !strings.Contains(err.Error(), "你的家沒有動") {
		t.Fatalf("空間不夠要拒絕並說原因：%v", err)
	}
	sameSnapshot(t, "空間不夠沒還原", before, snapshot(t, home))
	if exists(restoreDir(home)) || len(listDir(platform.HomeBackupDir(home, sdk.HomeBackupKindBeforeRestore))) != 0 {
		t.Fatal("沒開始的還原不能留暫存或現況備份")
	}
}

// 還原會讓家裡原有的捷徑（符號連結）不見（HBK-11）：預覽就列出來，還原後的「還要你做的事」請主人自己重建；
// 家資料夾最上層的捷徑不在要換的範圍裡，不列。
func TestRestoreTellsWhichShortcutsWillBeLost(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	m, _, _, _ := newModule(t, home, testNow)
	s := &service{m: m}
	ctx := context.Background()
	made, err := s.Create(ctx, &sdk.BackupCreateRequest{By: ownerActor})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(home), "outside")
	write(t, filepath.Join(outside, "a.md"), "外面的檔\n")
	for _, link := range []string{filepath.Join(home, "vault", "notes", "共用筆記"), filepath.Join(home, "media", "相簿"), filepath.Join(home, "top-link")} {
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
	}
	want := "media/相簿、vault/notes/共用筆記"
	pre, err := s.Preview(ctx, &sdk.BackupPreviewRequest{Kind: made.File.Kind, Name: made.File.Name})
	if err != nil || strings.Join(pre.LostLinks, "、") != want {
		t.Fatalf("預覽要列出會不見的捷徑 %q：%+v %v", want, pre.LostLinks, err)
	}
	p, err := Stage(StageOptions{Home: home, Archive: filepath.Join(platform.HomeBackupDir(home, made.File.Kind), made.File.Name), Now: testNow.Add(time.Second)})
	if err != nil || strings.Join(p.LostLinks, "、") != want {
		t.Fatalf("準備還原時要記下來：%+v %v", p, err)
	}
	res, err := ApplyPending(ApplyOptions{Home: home, Now: testNow.Add(2 * time.Second)})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	if len(res.Todo) != 1 || !strings.Contains(res.Todo[0], "2 個捷徑") || !strings.Contains(res.Todo[0], want) || !strings.Contains(res.Todo[0], "自己重建") {
		t.Fatalf("同一台還原只該有「捷徑不見了」這一件事：%q", res.Todo)
	}
	if exists(filepath.Join(home, "media", "相簿")) || !exists(filepath.Join(home, "top-link")) {
		t.Fatal("換掉的那幾塊裡的捷徑不見、最上層的捷徑留著")
	}
}

// 還要你做的事（HBK-7）：同一台同一個家還原不列「別人分享的套件要重新安裝」；換了電腦才列。
// 手機連回家指到「設定 → 手機連線」、重新登入指到修復入口，不叫主人找電腦秘書、不叫他打指令。
func TestRestoreTodoDependsOnWhetherTheMachineChanged(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	c, err := Create(CreateOptions{Home: home, Kind: sdk.HomeBackupKindManual, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Manifest.Installed) == 0 {
		t.Fatal("測試前提：這個家有別人分享的套件")
	}
	if _, err := Stage(StageOptions{Home: home, Archive: c.Path, Now: testNow.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	res, err := ApplyPending(ApplyOptions{Home: home})
	if err != nil || !res.OK || len(res.Todo) != 0 {
		t.Fatalf("同一台還原不該有還要做的事：%+v %v", res, err)
	}
	todo := strings.Join(Todo([]string{"shared-pkg"}), "\n")
	for _, want := range []string{"設定 → 手機連線", platform.RescueName, "shared-pkg"} {
		if !strings.Contains(todo, want) {
			t.Errorf("換電腦後要做的事少了 %q：\n%s", want, todo)
		}
	}
	for _, bad := range []string{"電腦秘書", "evergenie tailscale"} {
		if strings.Contains(todo, bad) {
			t.Errorf("不該出現 %q：\n%s", bad, todo)
		}
	}
}

// 這個家有活著的服務時（服務跑著會一直拿著鎖檔）不換上備份（HBK-12）：第二個 serve 被開起來只會失敗退出，
// 家沒動、等著的還原也還在；服務停了（鎖放掉）才換。
func TestApplyRefusesWhileTheServiceIsAlive(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	archive, _ := backupThenChange(t, home)
	if _, err := Stage(StageOptions{Home: home, Archive: archive, Now: testNow.Add(1)}); err != nil {
		t.Fatal(err)
	}
	changed := snapshot(t, home)
	lock := platform.ServiceLockPath(home)
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	res, err := ApplyPending(ApplyOptions{Home: home})
	if err == nil || res != nil || !strings.Contains(err.Error(), "已經有一個服務在跑") {
		t.Fatalf("服務活著時要拒絕：%+v %v", res, err)
	}
	if !HasPending(home) {
		t.Fatal("等著的還原要留著")
	}
	sameSnapshot(t, "服務活著時沒換", changed, snapshot(t, home, filepath.Join("..", "state-h")))
	// 準備到一半留下的暫存也一樣：有活著的服務就不收（那個服務可能正在準備還原）。
	other := fakeHome(t, "o", false, 18002)
	half := filepath.Join(restoreDir(other), "tok", "new", "home", "vault", "big.bin")
	write(t, half, "正在解")
	of, err := os.OpenFile(platform.ServiceLockPath(other), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer of.Close()
	if err := syscall.Flock(int(of.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPending(ApplyOptions{Home: other}); err == nil || !exists(half) {
		t.Fatalf("服務活著時不能收它的暫存：%v", err)
	}
	// 服務停了：照常換上去。
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if res, err := ApplyPending(ApplyOptions{Home: home}); err != nil || res == nil || !res.OK {
		t.Fatalf("服務停了要換得上去：%+v %v", res, err)
	}
}

// 換完、驗過、結果也寫了，只差沒記「做完了」就被關掉：下次啟動當成做完了收尾，不能把成功的還原退回去又說沒做成。
func TestRestoreInterruptedAfterResultIsWrittenStaysRestored(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	archive, before := backupThenChange(t, home)
	p, err := Stage(StageOptions{Home: home, Archive: archive, Now: testNow.Add(1)})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(pendingPath(home))
	if err != nil {
		t.Fatal(err)
	}
	// 驗證那一步（換完之後才跑）把「正在換」的紀錄與暫存留一份：等一下放回去，就是行程死在寫完結果之後的樣子。
	keep := filepath.Join(t.TempDir(), "restore-copy")
	var swapping []byte
	verify := func(string) error {
		swapping, err = os.ReadFile(pendingPath(home))
		if err != nil {
			return err
		}
		return os.CopyFS(keep, os.DirFS(restoreDir(home)))
	}
	res, err := ApplyPending(ApplyOptions{Home: home, Now: testNow.Add(2), Verify: verify})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	if string(swapping) == string(saved) || !strings.Contains(string(swapping), phaseSwapping) {
		t.Fatalf("測試前提：驗證時紀錄停在「正在換」：%s", swapping)
	}
	if err := os.CopyFS(restoreDir(home), os.DirFS(keep)); err != nil {
		t.Fatal(err)
	}
	// 下次啟動。
	again, err := ApplyPending(ApplyOptions{Home: home, Now: testNow.Add(3)})
	if err != nil || again != nil {
		t.Fatalf("已經成功的還原不該再做一次或退回：%+v %v", again, err)
	}
	r := readResult(home)
	if r == nil || !r.OK || r.Safety != p.Safety || r.Token != p.Token {
		t.Fatalf("結果要維持「成功」：%+v", r)
	}
	if exists(restoreDir(home)) {
		t.Fatal("暫存要收掉")
	}
	got := snapshot(t, home, "home.toml")
	delete(before, "home.toml")
	sameSnapshot(t, "還原的內容要留著", before, got)
	// 對照：結果不是這一次的（上一次還原留下的成功結果），「正在換」就照舊全部換回去。
	home2 := fakeHome(t, "h2", true, 18003)
	archive2, _ := backupThenChange(t, home2)
	changed2 := snapshot(t, home2)
	markNotified(home2, &restoreRecord{HomeRestoreResult: sdk.HomeRestoreResult{OK: true}, Token: "上一次的"})
	if _, err := Stage(StageOptions{Home: home2, Archive: archive2, Now: testNow.Add(1)}); err != nil {
		t.Fatal(err)
	}
	p2, err := loadPending(home2)
	if err != nil {
		t.Fatal(err)
	}
	p2.Phase = phaseSwapping
	if err := savePending(home2, p2); err != nil {
		t.Fatal(err)
	}
	res2, err := ApplyPending(ApplyOptions{Home: home2})
	if err != nil || res2 == nil || res2.OK {
		t.Fatalf("別次的成功結果不能讓換到一半的被當成做完：%+v %v", res2, err)
	}
	sameSnapshot(t, "換回還原前", changed2, snapshot(t, home2))
}

type fakeSessions struct {
	calls int
	order *[]string
}

func (f *fakeSessions) CloseAll(context.Context) (int, error) {
	f.calls++
	*f.order = append(*f.order, "close")
	return 3, nil
}

// 還原準備好、服務重開之前，先把這個家開著的秘書對話全部收掉（HBK-12）；沒準備成就不收。
func TestRestoreClosesSessionsBeforeRestart(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	var order []string
	sessions := &fakeSessions{order: &order}
	m := New(Deps{Clock: platform.NewFakeClock(testNow), Sessions: sessions, Restart: func() { order = append(order, "restart") }})
	m.SetHome(home)
	s := &service{m: m}
	ctx := context.Background()
	made, err := s.Create(ctx, &sdk.BackupCreateRequest{By: ownerActor})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(ctx, &sdk.BackupRestoreRequest{Kind: "manual", Name: "不存在-20260101-000000.egbackup", By: ownerActor}); err == nil || sessions.calls != 0 {
		t.Fatalf("沒準備成的還原不能收對話：%v，收了 %d 次", err, sessions.calls)
	}
	if _, err := s.Restore(ctx, &sdk.BackupRestoreRequest{Kind: made.File.Kind, Name: made.File.Name, By: ownerActor}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "close,restart" {
		t.Fatalf("要先收對話再重開：%v", order)
	}
}

// 改名（DEV-158）前的舊版程式做的備份（HBK-1）：照樣列出來（可以下載），但這一版不還原——
// 預覽與還原都回一句白話、停在動到任何東西之前（家沒動、沒有暫存、沒有等著換的還原、沒關對話也沒重開）。
// 真的舊檔是「舊副檔名＋說明檔的產品名不是新名」；兩個記號各自也都要擋住。舊產品名不寫出來，用假的 oldname。
func TestLegacyBackupIsListedButNotRestorable(t *testing.T) {
	home := fakeHome(t, "h", true, 18001)
	var order []string
	m := New(Deps{Clock: platform.NewFakeClock(testNow), Sessions: &fakeSessions{order: &order}, Restart: func() { order = append(order, "restart") }})
	m.SetHome(home)
	s := &service{m: m}
	ctx := context.Background()
	made, err := s.Create(ctx, &sdk.BackupCreateRequest{By: ownerActor})
	if err != nil {
		t.Fatal(err)
	}
	dir := platform.HomeBackupDir(home, made.File.Kind)
	src := filepath.Join(dir, made.File.Name)
	stem := strings.TrimSuffix(made.File.Name, platform.HomeBackupExt)
	cases := map[string]string{
		"舊副檔名、產品名不是新名（真的舊檔）": withProduct(t, src, filepath.Join(dir, stem+"-a"+platform.LegacyHomeBackupExt), "oldname"),
		"新副檔名、產品名不是新名":       withProduct(t, src, filepath.Join(dir, stem+"-b"+platform.HomeBackupExt), "oldname"),
		"舊副檔名、產品名是新名":        withProduct(t, src, filepath.Join(dir, stem+"-c"+platform.LegacyHomeBackupExt), platform.ProductName),
	}
	before := snapshot(t, home)
	for what, path := range cases {
		name := filepath.Base(path)
		listed := false
		for _, f := range List(home) {
			listed = listed || (f.Name == name && f.Kind == made.File.Kind && f.Download == "/home-backup/"+made.File.Kind+"/"+name)
		}
		if !listed {
			t.Fatalf("%s：要列出來（可以下載）：%+v", what, List(home))
		}
		target := &sdk.BackupPreviewRequest{Kind: made.File.Kind, Name: name}
		if _, err := s.Preview(ctx, target); err == nil || !strings.Contains(err.Error(), "改名前的舊版備份") || !strings.Contains(err.Error(), "舊版的程式") {
			t.Fatalf("%s：預覽要白話說這一版不能還原：%v", what, err)
		}
		if _, err := s.Restore(ctx, &sdk.BackupRestoreRequest{Kind: made.File.Kind, Name: name, By: ownerActor}); err == nil || !strings.Contains(err.Error(), "改名前的舊版備份") {
			t.Fatalf("%s：還原要白話拒絕：%v", what, err)
		}
		if _, err := Stage(StageOptions{Home: home, Archive: path, Now: testNow.Add(time.Second)}); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "改名前的舊版備份") {
			t.Fatalf("%s：直接 Stage 也要拒絕：%v", what, err)
		}
	}
	sameSnapshot(t, "舊版備份沒還原", before, snapshot(t, home))
	if HasPending(home) || exists(restoreDir(home)) || len(order) != 0 || len(listDir(platform.HomeBackupDir(home, sdk.HomeBackupKindBeforeRestore))) != 0 {
		t.Fatalf("拒絕還原不能有副作用：pending=%v order=%v", HasPending(home), order)
	}
}

// withProduct 把 src 這份備份抄到 dst，只把說明檔寫的產品名換成 product（每個檔的檢查碼照舊對得上）；回 dst。
func withProduct(t *testing.T, src, dst, product string) string {
	t.Helper()
	got := entries(t, src)
	var man map[string]any
	if err := json.Unmarshal([]byte(got[manifestName]), &man); err != nil {
		t.Fatal(err)
	}
	man["product"] = product
	data, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	var hdrs []tar.Header
	var bodies []string
	for _, n := range names(got) {
		if n != manifestName {
			hdrs, bodies = append(hdrs, tar.Header{Name: n, Typeflag: tar.TypeReg}), append(bodies, got[n])
		}
	}
	hdrs, bodies = append(hdrs, tar.Header{Name: manifestName, Typeflag: tar.TypeReg}), append(bodies, string(data))
	if err := os.Rename(craft(t, hdrs, bodies), dst); err != nil {
		t.Fatal(err)
	}
	return dst
}
