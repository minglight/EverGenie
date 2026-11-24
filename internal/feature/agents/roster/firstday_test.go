package roster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/agent/personafixture"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// fakeMail 記下每一則排進佇列的封包；fail 不是 nil 時每次都回這個錯（佇列寫不進去）。
type fakeMail struct {
	mu   sync.Mutex
	sent []*sdk.Envelope
	fail error
}

func (f *fakeMail) Send(_ context.Context, e *sdk.Envelope) (*sdk.Envelope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, e)
	if f.fail != nil {
		return nil, f.fail
	}
	return e, nil
}

func (f *fakeMail) all() []*sdk.Envelope {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*sdk.Envelope(nil), f.sent...)
}

// firstDaySetup 起名冊管理：接真的底層 agent、真的 vault（測試家）、假的佇列；existing 是開始前名冊上就有的紀錄。
func firstDaySetup(t *testing.T, existing ...vault.Record) (*Module, *homefixture.Home, *fakeMail) {
	t.Helper()
	h := homefixture.New(t)
	ro := agent.New(agent.Deps{Vault: h.Vault, Clock: h.Clock, Bus: &quietBus{}})
	ro.SetHome(h.Dir)
	ro.SetPersonaSource(personafixture.New(h.Dir))
	must(t, ro.Start(context.Background()))
	t.Cleanup(func() { _ = ro.Stop(context.Background()) })
	must(t, os.MkdirAll(filepath.Join(h.Dir, "personas"), 0o700))
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas", "athena.md"), []byte("---\nname: Athena\n---\n人設\n"), 0o600))
	h.WithRecords(existing...)
	mail := &fakeMail{}
	m := New(Deps{Roster: ro, Vault: h.Vault, Mail: mail, Clock: h.Clock})
	m.SetHome(h.Dir)
	must(t, m.Start(context.Background()))
	return m, h, mail
}

// hire 照看板請秘書的順序：建（停用的）再設成啟用（board 的 hireSecretary、套件同步的認領都是這兩步）。
func hire(t *testing.T, m *Module, id string) {
	t.Helper()
	if _, err := m.Create(context.Background(), id, "athena", "", "", nil, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetField(context.Background(), id, "enabled", "true", "owner(web)"); err != nil {
		t.Fatal(err)
	}
}

// 新家（名冊一位都沒有）請到的第一位秘書：建的時候記 first_day、不送（停用的收不到）；設成啟用時排一則核心給她的
// 系統提示——routine、from system、走核心派送、固定的 idempotency key；開頭「［核心］」讓看板放在秘書那邊；
// 不寫平台名稱（TestNoVendorTermsInSession 掃原始碼擋）——排好就拿掉 first_day。之後再設一次啟用、再請第二位都不送。
func TestFirstSecretaryOfNewHomeGetsGreetingPrompt(t *testing.T) {
	m, h, mail := firstDaySetup(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, "life", "athena", "", "", nil, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if got := record(t, h, "agent", "life").Fields[FirstDayField]; got != true {
		t.Fatalf("第一位秘書建好要記 first_day：%v", got)
	}
	if got := len(mail.all()); got != 0 {
		t.Fatalf("還沒啟用不送（停用的秘書收不到話）：%d 則", got)
	}
	r, err := m.SetField(ctx, "life", "enabled", "true", "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	sent := mail.all()
	if len(sent) != 1 {
		t.Fatalf("設成啟用時要排一則招呼提示：%d 則", len(sent))
	}
	e := sent[0]
	if e.Kind != sdk.EnvelopeKindRoutine || e.Route != sdk.EnvelopeRouteCore || e.From != "system" || e.To != "life" || e.IdempotencyKey != "first-day:life" {
		t.Fatalf("招呼提示的封包：%+v", e)
	}
	if !strings.HasPrefix(e.Body, "［核心］") || !strings.Contains(e.Body, "自我介紹") || !strings.Contains(e.Body, "第一天") || !strings.Contains(e.Body, "不是主人說的話") {
		t.Fatalf("招呼提示的內容：%q", e.Body)
	}
	// 不寫平台名稱：提示是這個套件的常數，session 的 TestNoVendorTermsInSession 掃核心原始碼時一起擋。
	if _, still := record(t, h, "agent", "life").Fields[FirstDayField]; still || r.Fields[FirstDayField] != nil {
		t.Fatalf("排好之後拿掉 first_day：%v", record(t, h, "agent", "life").Fields)
	}

	// 停用再啟用、名冊有人之後再請（設定頁、CLI 都一樣走 Create＋SetField）：都不送。
	if _, err := m.SetField(ctx, "life", "enabled", "false", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetField(ctx, "life", "enabled", "true", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	hire(t, m, "work")
	if _, has := record(t, h, "agent", "work").Fields[FirstDayField]; has {
		t.Fatal("第二位秘書不記 first_day")
	}
	if got := len(mail.all()); got != 1 {
		t.Fatalf("只送第一位那一次：%d 則", got)
	}
}

// 升級上來的舊家（名冊本來就有人，含停用的）與只剩封存秘書的家，請新秘書都不送：不是「這個家第一次有秘書」。
// 舊家原本那位重新啟用也不送（她的紀錄沒有 first_day）。
func TestNoGreetingWhenHomeAlreadyHadSecretaries(t *testing.T) {
	disabled := vault.Record{Kind: "agent", ID: "code", Fields: map[string]any{"duty": "寫程式", "persona": "athena", "enabled": false}}
	m, _, mail := firstDaySetup(t, disabled)
	hire(t, m, "life")
	if _, err := m.SetField(context.Background(), "code", "enabled", "true", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if got := len(mail.all()); got != 0 {
		t.Fatalf("名冊本來就有秘書（停用的也算）不送：%d 則", got)
	}

	m, h, mail := firstDaySetup(t, disabled)
	must(t, h.Vault.ArchiveNow(context.Background(), "agent", "code", "owner(cli)"))
	hire(t, m, "life")
	if got := len(mail.all()); got != 0 {
		t.Fatalf("只剩封存的秘書也不是第一次有秘書，不送：%d 則", got)
	}
}

// 排不進佇列（佇列寫不進去）：設成啟用照樣成功、不卡住看板；first_day 那一次就拿掉，之後再啟用不補送（不送過期的招呼）。
// 建失敗（秘書不能建秘書、人設不存在）不記。mode 是 none（沒有對話）不排、欄位也拿掉，之後改 mode 再停用啟用也不送。
func TestGreetingFailureDoesNotBlockHiring(t *testing.T) {
	m, h, mail := firstDaySetup(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, "life", "nobody", "", "", nil, "owner(web)"); err == nil {
		t.Fatal("人設不存在要拒絕")
	}
	if _, err := m.Create(ctx, "life", "athena", "", "", nil, "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能建秘書：%v", err)
	}
	mail.fail = errors.New("佇列寫不進去")
	if _, err := m.Create(ctx, "life", "athena", "", "", nil, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if r, err := m.SetField(ctx, "life", "enabled", "true", "owner(web)"); err != nil || r.Fields["enabled"] != true {
		t.Fatalf("招呼排不進去，設成啟用照樣成功：%v %v", r, err)
	}
	life := record(t, h, "agent", "life")
	if _, still := life.Fields[FirstDayField]; still || len(mail.all()) != 1 || life.Fields["by"] != "system" {
		t.Fatalf("排不進去：只試一次、first_day 由核心拿掉：%v %d %q", life.Fields, len(mail.all()), life.Fields["by"])
	}
	mail.fail = nil
	if _, err := m.SetField(ctx, "life", "enabled", "false", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetField(ctx, "life", "enabled", "true", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if got := len(mail.all()); got != 1 {
		t.Fatalf("之後再啟用不補送：%d", got)
	}

	// mode 是 none：她沒有對話可以說話，不排；之後改成 resident、停用再啟用也不送。
	none, noneHome, noneMail := firstDaySetup(t)
	if _, err := none.Create(ctx, "life", "athena", "", "", nil, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	for _, step := range [][2]string{{"mode", "none"}, {"enabled", "true"}, {"mode", "resident"}, {"enabled", "false"}, {"enabled", "true"}} {
		if _, err := none.SetField(ctx, "life", step[0], step[1], "owner(web)"); err != nil {
			t.Fatal(err)
		}
	}
	if _, still := record(t, noneHome, "agent", "life").Fields[FirstDayField]; still || len(noneMail.all()) != 0 {
		t.Fatalf("mode none 不排、欄位拿掉、之後也不送：%v %d", still, len(noneMail.all()))
	}

	// 沒接佇列：照樣建、照樣啟用、不出錯。
	other, emptyHome, _ := firstDaySetup(t)
	noMail := New(Deps{Roster: other.d.Roster, Vault: emptyHome.Vault})
	noMail.SetHome(emptyHome.Dir)
	must(t, noMail.Start(ctx))
	hire(t, noMail, "life")
	if got := record(t, emptyHome, "agent", "life").Fields["enabled"]; got != true {
		t.Fatalf("沒接佇列照樣啟用：%v", got)
	}
}
