package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// TestClaimFlowOnFixtureHome（SEC-302 S9 的流程驗收，真 serve＋fixture 家、假引擎，不碰正式家）：
// packages new garden → 角色放進套件並上線 → 秘書發認領通知 → 秘書自己回答不算、秘書 apply 被擋 →
// 主人按「好」→ garden 秘書存在、人設 garden.fairy、已啟用；另一個套件按「之後再說」後不再發第二則。
// Guards: REQ-341, REQ-343, REQ-344, REQ-345
func TestClaimFlowOnFixtureHome(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)
	hgOK(t, sock, "packages", "new", "garden", "--name", "花園")
	hgOK(t, sock, "personas", "new", "--to-package", "garden", "fairy", "--name", "花仙子")
	hgOK(t, sock, "personas", "live", "garden.fairy")

	t.Setenv("EVERGENIE_AGENT", "code")
	var r sdk.PackageClaimNotifyResponse
	if err := json.Unmarshal([]byte(hgOK(t, sock, "packages", "claim-notify", "garden", "--json")), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Sent || r.Agent != "garden" || r.Persona != "garden.fairy" || r.Notice == "" {
		t.Fatalf("認領通知：%+v", r)
	}
	n, err := sdk.NewNotifyClient(sdk.Dial(sock)).Get(context.Background(), &sdk.NotifyGetRequest{ID: r.Notice})
	if err != nil {
		t.Fatal(err)
	}
	if n.Notification.Category != sdk.CategoryApprove || n.Notification.Claim != "garden" || n.Notification.Source != sdk.NotifySourcePackage ||
		strings.Join(n.Notification.Options, "|") != "好，用 花仙子|換一個|之後再說" {
		t.Fatalf("通知內容：%+v", n.Notification)
	}
	// 秘書不能自己套用，也不能替主人按「好」。
	if code, _, errOut := hg(t, sock, "", "packages", "sync", "garden", "--persona", "garden.fairy", "--apply"); code != 1 || !strings.Contains(errOut, "只有主人") {
		t.Fatalf("秘書 apply：%d %q", code, errOut)
	}
	if status := rawPost(t, sock, "/api/notify/answer", `{"id":"`+r.Notice+`","text":"好，用 花仙子","via":"button","by":{"name":"code"}}`); status != http.StatusOK {
		t.Fatalf("秘書回答（通知照收，但核心不動作）：%d", status)
	}
	time.Sleep(300 * time.Millisecond)
	if _, ok := agentFields(t, sock, "garden"); ok {
		t.Fatal("秘書回答不能建出 garden")
	}

	// garden 那則已經被秘書答掉了（核心沒動作，也不會再問）；用另一個套件 park 測主人按「好」。
	t.Setenv("EVERGENIE_AGENT", "")
	hgOK(t, sock, "packages", "new", "park", "--name", "公園")
	hgOK(t, sock, "personas", "new", "--to-package", "park", "elf", "--name", "小精靈")
	hgOK(t, sock, "personas", "live", "park.elf")
	hgOK(t, sock, "packages", "new", "lawn")
	t.Setenv("EVERGENIE_AGENT", "code")
	var p sdk.PackageClaimNotifyResponse
	if err := json.Unmarshal([]byte(hgOK(t, sock, "packages", "claim-notify", "park", "--json")), &p); err != nil || !p.Sent || p.Persona != "park.elf" {
		t.Fatalf("park 認領通知：%+v %v", p, err)
	}
	// 主人在看板按「好」（看板代理把 by 換成 owner(web)；這裡直接打 API 帶主人身分）。
	if status := rawPost(t, sock, "/api/notify/answer", `{"id":"`+p.Notice+`","text":"好，用 小精靈","via":"button","by":{"name":"owner(web)"}}`); status != http.StatusOK {
		t.Fatalf("主人回答：%d", status)
	}
	var f map[string]any
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if got, ok := agentFields(t, sock, "park"); ok && got["enabled"] == true {
			f = got
			break
		}
	}
	if f == nil || f["persona"] != "park.elf" {
		t.Fatalf("主人按好之後 park 秘書要存在、人設 park.elf、已啟用：%v", f)
	}

	// 「之後再說」：garden 那則被秘書答掉了，用 garden 以外的新套件 lawn 測；之後同一個套件不再發第二則。
	var l sdk.PackageClaimNotifyResponse
	if err := json.Unmarshal([]byte(hgOK(t, sock, "packages", "claim-notify", "lawn", "--persona", "garden.fairy", "--json")), &l); err != nil || !l.Sent {
		t.Fatalf("lawn 認領通知：%+v %v", l, err)
	}
	if status := rawPost(t, sock, "/api/notify/answer", `{"id":"`+l.Notice+`","text":"之後再說","via":"button","by":{"name":"owner(web)"}}`); status != http.StatusOK {
		t.Fatalf("之後再說：%d", status)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		data, _ := os.ReadFile(filepath.Join(dir, "settings", "packages.toml"))
		if strings.Contains(string(data), `state = "later"`) {
			break
		}
	}
	before := countNotices(t, sock)
	out := hgOK(t, sock, "packages", "claim-notify", "lawn")
	if !strings.Contains(out, "沒有發新的通知") || !strings.Contains(out, "之後再說") {
		t.Fatalf("之後再說之後再請：%s", out)
	}
	if after := countNotices(t, sock); after != before {
		t.Fatalf("之後再說之後不能再發：%d → %d", before, after)
	}
	if _, ok := agentFields(t, sock, "lawn"); ok {
		t.Fatal("之後再說不建秘書")
	}
}

func agentFields(t *testing.T, sock, id string) (map[string]any, bool) {
	t.Helper()
	r, err := sdk.NewVaultClient(sdk.Dial(sock)).Get(context.Background(), &sdk.VaultGetRequest{Kind: "agent", ID: id})
	if err != nil || r.Record == nil {
		return nil, false
	}
	return r.Record.Fields, true
}

// countNotices 只算認領通知（Claim 不是空的）：fixture 家有一張過期的每週待辦 R-2，serve 起來後第一次 tick（約 1 秒）
// 才會補發它的到期通知；全部都算的話，測試跨過那一刻就會多一則（3→4），跟認領無關。
func countNotices(t *testing.T, sock string) int {
	t.Helper()
	r, err := sdk.NewNotifyClient(sdk.Dial(sock)).List(context.Background(), &sdk.NotifyListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range r.Notifications {
		if x.Claim != "" {
			n++
		}
	}
	return n
}
