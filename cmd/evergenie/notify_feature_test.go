package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 從 registry 起暫存家與 fake 引擎，驗指令、help、首頁 API 的業務模組登記接線。
func TestNotifyFeatureOnFakeEngineHome(t *testing.T) {
	dir := doctorHome(t)
	_, sock := startDoctorAPI(t, dir)
	out := hgOK(t, sock, "notify", "--agent", "code", "--category", "approve", "--title", "通知搬移驗收", "--todo", "確認", "--options", "好|不要")
	var n sdk.Notification
	if err := json.Unmarshal([]byte(out), &n); err != nil || n.ID == "" {
		t.Fatalf("notify: %s %v", out, err)
	}
	if help := hgOK(t, sock, "help"); !strings.Contains(help, "notify --agent") || !strings.Contains(help, "push init") {
		t.Fatalf("help 缺指令: %s", help)
	}
	home, err := platform.LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := fmt.Sprintf("http://127.0.0.1:%d/api/web/get_overview", home.Web.Port)
	resp, err := http.Post(h, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var overview sdk.WebGetOverviewResponse
	if err := json.Unmarshal(raw, &overview); err != nil || resp.StatusCode != 200 {
		t.Fatalf("overview: %d %s %v", resp.StatusCode, raw, err)
	}
	found := false
	for _, a := range overview.Approvals {
		if a.ID == n.ID {
			found = true
		}
	}
	if !found || overview.Unread < 1 {
		t.Fatalf("通知未進首頁: %s", raw)
	}
}
