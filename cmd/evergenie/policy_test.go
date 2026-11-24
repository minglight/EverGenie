package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// TestPolicyCommandsOnFixtureHome：真的 serve＋fixture 家，秘書送申請、不能審；主人看過指紋才核准，撤銷回到原狀。
// Guards: REQ-167, REQ-168
func TestPolicyCommandsOnFixtureHome(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)
	before := hgOK(t, sock, "trust", "show", "--json")
	trustBefore, _ := os.ReadFile(filepath.Join(dir, "settings", "trust.toml"))

	help := hgOK(t, sock, "policy", "--help")
	for _, sub := range []string{"request", "list", "show", "decide", "revoke", "--why", "--fingerprint"} {
		if !strings.Contains(help, sub) {
			t.Errorf("policy --help 少了 %s", sub)
		}
	}

	t.Setenv("EVERGENIE_AGENT", "code")
	out := hgOK(t, sock, "policy", "request", "--kind", "rule", "--action", "write", "--scope", "~/Workspace/amy/evergenie", "--why", "改 EverGenie 的檔案被擋了兩次", "--risk", "只限這個 repo")
	var q sdk.PolicyRequest
	if err := json.Unmarshal([]byte(out), &q); err != nil || q.ID != "P-1" || q.Status != sdk.PolicyStatusPending || q.Broad {
		t.Fatalf("request：%s %v", out, err)
	}
	if code, _, errOut := hg(t, sock, "", "policy", "request", "--kind", "rule", "--action", "write", "--scope", "~/x/y/z"); code == 0 || !strings.Contains(errOut, "被擋住的情境") {
		t.Fatalf("空 why 要失敗：%d %q", code, errOut)
	}
	out = hgOK(t, sock, "policy", "request", "--kind", "rule", "--action", "read", "--why", "要讀別的地方")
	if !strings.Contains(out, `"broad":true`) {
		t.Fatalf("沒範圍要標 broad：%s", out)
	}
	if after := hgOK(t, sock, "trust", "show", "--json"); after != before {
		t.Fatalf("送申請不能改任何權限：\n%s\n%s", before, after)
	}
	if list := hgOK(t, sock, "policy", "list", "--status", "pending"); strings.Count(list, "\n") != 2 {
		t.Fatalf("policy list：\n%s", list)
	}

	// 秘書不能審、不能撤銷（API 回 403）。
	if code, _, errOut := hg(t, sock, "", "policy", "decide", "P-1", "--approve", "--fingerprint", q.Fingerprint); code != 1 || !strings.Contains(errOut, "只有主人") {
		t.Fatalf("秘書審申請：%d %q", code, errOut)
	}
	status := rawPost(t, sock, "/api/policy/decide", `{"id":"P-1","approve":true,"fingerprint":"`+q.Fingerprint+`","by":{"name":"code"}}`)
	if status != http.StatusForbidden {
		t.Fatalf("秘書回合呼叫 decide 要 403，實際 %d", status)
	}
	if status := rawPost(t, sock, "/api/policy/revoke", `{"id":"P-1","by":{"name":"code"}}`); status != http.StatusForbidden {
		t.Fatalf("秘書回合呼叫 revoke 要 403，實際 %d", status)
	}

	// 主人：沒帶指紋是用法錯誤；帶舊的指紋要重看；看過再核准。
	t.Setenv("EVERGENIE_AGENT", "")
	if code, _, _ := hg(t, sock, "", "policy", "decide", "P-1"); code != 2 {
		t.Fatalf("沒說核准或拒絕：%d", code)
	}
	if code, _, _ := hg(t, sock, "", "policy", "decide", "P-1", "--approve"); code != 2 {
		t.Fatalf("核准沒帶指紋：%d", code)
	}
	if code, _, errOut := hg(t, sock, "", "policy", "decide", "P-1", "--approve", "--fingerprint", "stale"); code != 1 || !strings.Contains(errOut, "重新看") {
		t.Fatalf("舊指紋：%d %q", code, errOut)
	}
	show := hgOK(t, sock, "policy", "show", "P-1")
	if !strings.Contains(show, "指紋："+q.Fingerprint) || !strings.Contains(show, "影響：") {
		t.Fatalf("policy show：\n%s", show)
	}
	out = hgOK(t, sock, "policy", "decide", "P-1", "--approve", "--fingerprint", q.Fingerprint, "--note", "測試用，等一下撤")
	if !strings.Contains(out, `"status":"applied"`) || !strings.Contains(out, `"expires_at"`) {
		t.Fatalf("核准：%s", out)
	}
	hgOK(t, sock, "policy", "decide", "P-2", "--reject", "--note", "太寬了")
	out = hgOK(t, sock, "policy", "revoke", "P-1", "--note", "測完撤掉")
	if !strings.Contains(out, `"status":"revoked"`) {
		t.Fatalf("撤銷：%s", out)
	}
	if after := hgOK(t, sock, "trust", "show", "--json"); after != before {
		t.Fatal("撤銷後要回到申請前")
	}
	if trustAfter, _ := os.ReadFile(filepath.Join(dir, "settings", "trust.toml")); string(trustAfter) != string(trustBefore) {
		t.Fatal("申請流程不能改 trust.toml")
	}
	if _, err := os.Stat(filepath.Join(dir, "vault", "policy", "P-1.md")); err != nil {
		t.Fatalf("申請單要存在 vault/policy：%v", err)
	}
}

// rawPost 直接打 API socket，回 HTTP 狀態碼。
func rawPost(t *testing.T, sock, path, body string) int {
	t.Helper()
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	resp, err := c.Post("http://hg"+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestRequestPermissionSkillMatchesCLI：技能裡寫到的 EverGenie 指令與旗標都要真的存在（計畫 S4 的驗收）。
// Guards: REQ-167
func TestRequestPermissionSkillMatchesCLI(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packages", "official", "engine-claude", "skills", "request-permission", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(data)
	code, help, _ := hg(t, "/nonexistent.sock", "", "policy", "--help")
	if code != 0 {
		t.Fatalf("policy --help → %d", code)
	}
	_, top, _ := hg(t, "/nonexistent.sock", "", "help")
	cmd := regexp.MustCompile("evergenie ((?:policy|trust) [a-z]+)([^`\\n]*)")
	matches := cmd.FindAllStringSubmatch(skill, -1)
	if len(matches) < 6 {
		t.Fatalf("技能裡的指令太少：%d", len(matches))
	}
	for _, m := range matches {
		parts := strings.Fields(m[1])
		switch parts[0] {
		case "policy":
			if !strings.Contains(help, "  "+parts[1]) {
				t.Errorf("policy --help 沒有 %s", parts[1])
			}
			for _, flag := range regexp.MustCompile(`--[a-z_]+`).FindAllString(m[2], -1) {
				if !strings.Contains(help, flag) {
					t.Errorf("%s 用到的 %s 不在 policy --help", m[1], flag)
				}
			}
		case "trust":
			if !strings.Contains(top, "trust show|scan|diff|denied") || !strings.Contains("show scan diff denied", parts[1]) {
				t.Errorf("evergenie %s 不存在", m[1])
			}
		}
	}
	for _, must := range []string{"不自己改", "終端機", "--why"} {
		if !strings.Contains(skill, must) {
			t.Errorf("技能要寫明 %q", must)
		}
	}
}
