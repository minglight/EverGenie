package roster

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// fakeDirectory 是看板名冊的唯讀查詢：全部秘書與現任人設的人名（沒有的回錯，退回 display_name）。
type fakeDirectory struct {
	list  []*sdk.Record
	names map[string]string
}

func (d fakeDirectory) List(context.Context) ([]*sdk.Record, error) { return d.list, nil }
func (d fakeDirectory) PersonaName(_ context.Context, id string) (string, error) {
	if n, ok := d.names[id]; ok {
		return n, nil
	}
	return "", errors.New("沒有人設")
}

// fakeAvatars：只有 builder 這個人設有動態頭像與頭像圖。
type fakeAvatars struct{}

func (fakeAvatars) HasImage(persona string) bool { return persona == "builder" }

func (fakeAvatars) VideoURL(agentID, persona string) string {
	if persona == "builder" {
		return "/avatars/" + agentID + ".mp4"
	}
	return ""
}

func (fakeAvatars) VideoURLs(agentID, persona string) []string {
	if persona == "builder" {
		return []string{"/avatars/" + agentID + ".mp4", "/avatars/" + agentID + "+2.mp4"}
	}
	return nil
}

func rec(id string, f map[string]any) *sdk.Record {
	return &sdk.Record{Kind: "agent", ID: id, Fields: f}
}

const accountsTOML = `[user]
name="測試使用者"
timezone="Asia/Taipei"
[slots]
default="life"
[slots.life]
label="日常"
remind="08:30"
check="18:00"
[accounts]
default="main"
[accounts.main]
engine="test-engine"
[accounts.work]
engine="test-engine"
[web]
port=18080
`

func boardFixture(t *testing.T, dir fakeDirectory) *Module {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(accountsTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Vault: fakeRecords{}, Cards: &fakeCards{}, Directory: dir, Avatars: fakeAvatars{}})
	m.SetHome(home)
	return m
}

func decodeInto[T any](t *testing.T, code int, raw []byte) T {
	t.Helper()
	var out T
	if code != 200 || json.Unmarshal(raw, &out) != nil {
		t.Fatalf("%d %s", code, raw)
	}
	return out
}

// 看板名冊（/api/web/get_roster，由 agents 登記）：只有啟用的，依 id 排；人名讀現任人設、頭像網址是 /avatars/<id>.png——
// 這位的人設沒有頭像圖就不給（DEV-157，看板直接畫名字、不抓 404）；動態頭像網址照 Avatars 給的（沒有就連欄位都沒有）。
// Guards: REQ-111, REQ-112
func TestRosterOnlyEnabledWithPersonaNames(t *testing.T) {
	m := boardFixture(t, fakeDirectory{
		list: []*sdk.Record{
			rec("life", map[string]any{"duty": "生活", "persona": "helper", "enabled": true}),
			rec("fitness", map[string]any{"duty": "健身", "persona": "coach", "enabled": false}),
			rec("code", map[string]any{"duty": "測試開發", "persona": "builder", "enabled": true, "color": "#336699", "color_dark": "#99ccff"}),
		},
		names: map[string]string{"code": "Builder", "life": "Helper", "fitness": "Coach"},
	})
	code, raw := post(t, serve(t, m), "/api/web/get_roster", struct{}{})
	got := decodeInto[sdk.WebGetRosterResponse](t, code, raw)
	if len(got.Agents) != 2 {
		t.Fatalf("roster = %+v", got)
	}
	c := got.Agents[0]
	if c.ID != "code" || c.Name != "Builder" || c.Duty != "測試開發" || c.Color != "#336699" || c.ColorDark != "#99ccff" ||
		c.AvatarURL != "/avatars/code.png" || c.VideoURL != "/avatars/code.mp4" || len(c.VideoURLs) != 2 || c.VideoURLs[1] != "/avatars/code+2.mp4" || c.Persona != "builder" || !c.Enabled {
		t.Fatalf("code = %+v", c)
	}
	if l := got.Agents[1]; l.ID != "life" || l.Name != "Helper" || l.Color != "" || l.AvatarURL != "" || l.VideoURL != "" || len(l.VideoURLs) != 0 {
		t.Fatalf("life = %+v", l)
	}
	if strings.Count(string(raw), `"video_url"`) != 1 || strings.Count(string(raw), `"video_urls"`) != 1 {
		t.Fatalf("只有 code 有動態頭像：%s", raw)
	}
}

// 設定頁「職位與秘書」（/api/web/get_agents）：含停用的、依 id 排；沒有人設名退回 display_name；缺 enabled 算停用；
// 帳號名稱來自 home.toml、不含 default。
func TestGetAgentsIncludesDisabled(t *testing.T) {
	m := boardFixture(t, fakeDirectory{
		list: []*sdk.Record{
			rec("life", map[string]any{"duty": "生活", "persona": "helper", "enabled": true, "account": "main", "model": "sonnet", "effort": "high"}),
			rec("fitness", map[string]any{"duty": "健身", "persona": "coach", "enabled": false}),
			rec("code", map[string]any{"duty": "開發", "persona": "builder", "display_name": "Dev"}),
		},
		names: map[string]string{"life": "Helper", "fitness": "Coach"},
	})
	code, raw := post(t, serve(t, m), "/api/web/get_agents", struct{}{})
	got := decodeInto[sdk.WebGetAgentsResponse](t, code, raw)
	if len(got.Agents) != 3 || got.Agents[0].ID != "code" || got.Agents[1].ID != "fitness" || got.Agents[2].ID != "life" {
		t.Fatalf("要含停用的、依 id 排：%+v", got.Agents)
	}
	if c := got.Agents[0]; c.Name != "Dev" || c.Enabled || c.Persona != "builder" {
		t.Fatalf("code：%+v", c)
	}
	if f := got.Agents[1]; f.Enabled || f.Name != "Coach" || f.Duty != "健身" {
		t.Fatalf("fitness：%+v", f)
	}
	if l := got.Agents[2]; !l.Enabled || l.Name != "Helper" || l.Account != "main" || l.Model != "sonnet" || l.Effort != "high" {
		t.Fatalf("life：%+v", l)
	}
	if strings.Join(got.Accounts, ",") != "main,work" {
		t.Fatalf("accounts = %v", got.Accounts)
	}
}

// 沒接名冊：名冊與設定頁都是空的，不是錯誤。
func TestRosterWithoutDirectoryIsEmpty(t *testing.T) {
	m := New(Deps{Vault: fakeRecords{}, Cards: &fakeCards{}})
	h := serve(t, m)
	code, raw := post(t, h, "/api/web/get_roster", struct{}{})
	if got := decodeInto[sdk.WebGetRosterResponse](t, code, raw); len(got.Agents) != 0 {
		t.Fatalf("roster = %+v", got)
	}
	code, raw = post(t, h, "/api/web/get_agents", struct{}{})
	if got := decodeInto[sdk.WebGetAgentsResponse](t, code, raw); len(got.Agents) != 0 {
		t.Fatalf("agents = %+v", got)
	}
}
