package session

import (
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 工作 session 認單號照家的 [ids] tasks 字首：預設 SEC-，別的字首不算單號；工坊（tasks = "DEV-"）的 DEV-<n> 是任務單，SEC- 不算單號。
func TestWorkTaskIDFollowsHomePrefix(t *testing.T) {
	daily := platform.IDPrefixes{}
	ws := platform.IDPrefixes{Tasks: "DEV-", Goals: "DG-"}
	cases := []struct {
		ids        platform.IDPrefixes
		topic, nm  string
		want       string
		wantTicket bool
	}{
		{daily, "SEC-5", "", "SEC-5", true},
		{daily, "", "code-sec-7", "SEC-7", true},
		{daily, "", "code-dev-3", "", false},
		{daily, "", "code-ws-3", "", false},
		{ws, "DEV-73", "", "DEV-73", true},
		{ws, "", "code-dev-73", "DEV-73", true},
		{ws, "SEC-1", "", "", false},
		{ws, "", "code-sec-1", "", false},
	}
	for _, c := range cases {
		got := workTaskID(c.ids, &sdk.SessionOpenRequest{Topic: c.topic, Name: c.nm}, "")
		if got != c.want {
			t.Fatalf("%+v：認出 %q，要 %q", c, got, c.want)
		}
		id := c.topic
		if id == "" {
			id = c.want
		}
		if id != "" && c.ids.IsTask(id) != c.wantTicket {
			t.Fatalf("%+v：IsTask=%v", c, c.ids.IsTask(id))
		}
	}
	if got := workRoom("code", "DEV-73"); got != "code-dev-73" {
		t.Fatalf("工作 session 名稱：%q", got)
	}
	m := &Module{config: &platform.Home{IDs: ws}}
	if m.ids().Task() != "DEV-" {
		t.Fatal("session 要用家的字首")
	}
	if (&Module{}).ids().Task() != "SEC-" {
		t.Fatal("沒有設定時用預設")
	}
}
