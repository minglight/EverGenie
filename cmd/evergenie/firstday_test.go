package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// firstDayGreetings 回佇列裡給 to 的第一位秘書招呼提示（核心排的 routine、key first-day:<to>）。
func firstDayGreetings(t *testing.T, sock, to string) []sdk.Envelope {
	t.Helper()
	r, err := sdk.NewBusClient(sdk.Dial(sock)).List(context.Background(), &sdk.BusListRequest{To: to})
	if err != nil {
		t.Fatal(err)
	}
	var out []sdk.Envelope
	for _, e := range r.Envelopes {
		if e.IdempotencyKey == "first-day:"+to {
			out = append(out, e)
		}
	}
	return out
}

// TestFirstSecretaryGreetsOnNewHome（DEV-153，agents AGS-11；真 serve＋fixture 家、假引擎，不碰正式家）：
// 名冊清空的新家請第一位秘書（建好再設成啟用；看板歡迎頁的 package.sync＋setField 最後也是同一組 Create、SetField）→ 她的佇列多一則核心的招呼提示；
// 再請第二位、重開服務、停用再啟用都不再多一則。升級上來的舊家（fixture 原本的名冊）請新秘書不送。
func TestFirstSecretaryGreetsOnNewHome(t *testing.T) {
	dir := copyFixtureHome(t, false)
	if err := os.RemoveAll(filepath.Join(dir, "vault", "agents")); err != nil {
		t.Fatal(err)
	}
	_, sock, stop := serveIsolation(t, dir)
	t.Setenv("EVERGENIE_HOME", dir)
	hgOK(t, sock, "agents", "new", "life", "--persona", "helper", "--duty", "生活")
	if got := firstDayGreetings(t, sock, "life"); len(got) != 0 {
		t.Fatalf("建好還沒啟用不送：%d", len(got))
	}
	hgOK(t, sock, "agents", "set", "life", "enabled", "true")
	got := firstDayGreetings(t, sock, "life")
	if len(got) != 1 {
		t.Fatalf("第一位秘書啟用之後要有一則招呼提示：%d", len(got))
	}
	e := got[0]
	if e.Kind != sdk.EnvelopeKindRoutine || e.From != "system" || e.Route != sdk.EnvelopeRouteCore || !strings.HasPrefix(e.Body, "［核心］") {
		t.Fatalf("招呼提示是核心的系統提示，不是主人的話：%+v", e)
	}
	if f, _ := agentFields(t, sock, "life"); f["first_day"] != nil {
		t.Fatalf("排好之後拿掉 first_day：%v", f)
	}

	hgOK(t, sock, "agents", "new", "work", "--persona", "builder", "--duty", "工作")
	hgOK(t, sock, "agents", "set", "work", "enabled", "true")
	hgOK(t, sock, "agents", "set", "life", "enabled", "false")
	hgOK(t, sock, "agents", "set", "life", "enabled", "true")
	if n, w := len(firstDayGreetings(t, sock, "life")), len(firstDayGreetings(t, sock, "work")); n != 1 || w != 0 {
		t.Fatalf("第二位、停用再啟用都不送：life %d、work %d", n, w)
	}

	stop()
	_, sock, _ = serveIsolation(t, dir)
	if n, w := len(firstDayGreetings(t, sock, "life")), len(firstDayGreetings(t, sock, "work")); n != 1 || w != 0 {
		t.Fatalf("重開服務不再送：life %d、work %d", n, w)
	}
}

// 升級上來的舊家：名冊本來就有秘書，請新秘書不記 first_day、不送。
func TestNoFirstDayGreetingOnUpgradedHome(t *testing.T) {
	dir := copyFixtureHome(t, false)
	_, sock, _ := serveIsolation(t, dir)
	t.Setenv("EVERGENIE_HOME", dir)
	hgOK(t, sock, "agents", "new", "scout", "--persona", "helper", "--duty", "查資料")
	hgOK(t, sock, "agents", "set", "scout", "enabled", "true")
	if got := firstDayGreetings(t, sock, "scout"); len(got) != 0 {
		t.Fatalf("舊家請新秘書不送：%d", len(got))
	}
	if f, _ := agentFields(t, sock, "scout"); f["first_day"] != nil {
		t.Fatalf("舊家請的不記 first_day：%v", f)
	}
}
