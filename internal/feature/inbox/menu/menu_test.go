package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/board"
	"github.com/minglight/evergenie/internal/feature/inbox/overview"
	"github.com/minglight/evergenie/sdk"
)

// ---- 測試用的假來源：總覽（徽章的數字）與名冊（show_when）----

// fakeAPI 是各業務模組登記的首頁資料段（notify 的 approvals、unread，tasks 的 waiting_tasks，chat 的 waiting_threads），
// 餵給真的 overview（總覽怎麼算在 internal/feature/inbox/overview 測）。err 不空時資料段讀取失敗。
type fakeAPI struct {
	waitingUser int32
	notices     []sdk.Notification
	waiting     []sdk.WaitingTask
	err         error
}

// overview 回一個收了 f 的資料段的首頁彙整（跟 app 組裝時一樣經 ConsumeBoard）。
func (f *fakeAPI) overview(t *testing.T) *overview.Module {
	t.Helper()
	o := overview.New(overview.Deps{})
	parts := []app.BoardPart{
		{Key: "approvals", Load: func(context.Context) (any, error) {
			if f.err != nil {
				return nil, f.err
			}
			var out []sdk.Notification
			for _, n := range f.notices {
				if n.Category == sdk.CategoryApprove && !n.Inline && (n.State == sdk.NotificationStateUnread || n.State == sdk.NotificationStateRead) {
					out = append(out, n)
				}
			}
			return out, nil
		}},
		{Key: "unread", Load: func(context.Context) (any, error) {
			var n int32
			for _, x := range f.notices {
				if x.State == sdk.NotificationStateUnread {
					n++
				}
			}
			return n, nil
		}},
		{Key: "waiting_tasks", Load: func(context.Context) (any, error) { return f.waiting, nil }},
		{Key: "waiting_threads", Load: func(context.Context) (any, error) {
			var out []sdk.ThreadSummary
			for i := int32(0); i < f.waitingUser; i++ {
				out = append(out, sdk.ThreadSummary{Name: fmt.Sprintf("t%d", i), Status: sdk.ThreadStatusWaitingUser})
			}
			return out, nil
		}},
	}
	if err := o.ConsumeBoard(parts); err != nil {
		t.Fatal(err)
	}
	return o
}

type fakeAgents struct {
	list  []*sdk.Record
	names map[string]string
}

func (f fakeAgents) List(ctx context.Context) ([]*sdk.Record, error) { return f.list, nil }

func agentRecord(id string, fields map[string]any) *sdk.Record {
	return &sdk.Record{Kind: "agent", ID: id, Fields: fields}
}

const homeTOML = `[user]
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
[web]
port=%d
`

// freePort 找一個現在空著的 127.0.0.1 連接埠。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

type env struct {
	m      *Module
	h      http.Handler
	api    *fakeAPI
	home   string
	cat    *fakeCatalog // 套件目錄（可換內容）
	as     *board.Assets
	agents *fakeAgents
	recs   *fakeRecords // show_when 的 cap: 讀的紀錄（MNU-11）
}

// newEnv 起暫存的家、一個選單 Module（套件目錄一開始是空的），掛成跟 app 一樣的路由。
// h 模擬看板外殼：/api/ 的請求先把 by 換成主人（owner(web)）再交給選單，/pkg-bundles/ 由套件前端資源送檔。
func newEnv(t *testing.T) *env {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "hgm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	write(t, filepath.Join(home, "home.toml"), fmt.Sprintf(homeTOML, freePort(t)))

	api := &fakeAPI{}
	agents := &fakeAgents{
		list: []*sdk.Record{
			agentRecord("code", map[string]any{"duty": "測試開發", "persona": "builder", "enabled": true, "mode": "resident"}),
			agentRecord("life", map[string]any{"duty": "生活", "persona": "helper", "enabled": true, "mode": "resident"}),
			agentRecord("fitness", map[string]any{"duty": "健身", "persona": "coach", "enabled": false, "mode": "resident"}),
		},
	}
	cat := &fakeCatalog{dirs: map[string]string{}}
	as := board.NewAssets(cat)
	as.SetHome(home)
	recs := &fakeRecords{recs: map[string]sdk.Record{}}
	m := New(Deps{Frontend: as, Roster: agents, Records: recs, Overview: api.overview(t)})
	m.SetHome(home)
	apiMux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	h := http.NewServeMux()
	h.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var obj map[string]json.RawMessage
		if json.Unmarshal(body, &obj) == nil && obj != nil {
			obj["by"] = json.RawMessage(`{"name":"owner(web)"}`)
			body, _ = json.Marshal(obj)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		apiMux.ServeHTTP(w, r)
	})
	h.HandleFunc("/pkg-bundles/", as.ServeBundle)
	return &env{m: m, h: h, api: api, home: home, cat: cat, as: as, agents: agents, recs: recs}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func post(h http.Handler, path, body string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func get(h http.Handler, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	return v
}

func decodeErr(t *testing.T, w *httptest.ResponseRecorder) sdk.Error {
	t.Helper()
	var e sdk.Error
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	return e
}

func menu(t *testing.T, e *env) *sdk.Menu {
	t.Helper()
	return decode[sdk.WebGetMenuResponse](t, post(e.h, "/api/web/get_menu", "{}")).Menu
}

func ids(entries []sdk.MenuEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

func approve(id string, state sdk.NotificationState) sdk.Notification {
	return sdk.Notification{ID: id, Category: sdk.CategoryApprove, State: state, Title: "要不要？"}
}

// ---- 選單 ----

// ---- 選單 ----
// Guards: REQ-105
func TestMenuComputedOnceComplete(t *testing.T) {
	e := newEnv(t)
	e.api.waitingUser = 2
	e.api.notices = []sdk.Notification{approve("M-1", sdk.NotificationStateUnread), approve("M-2", sdk.NotificationStateRead), {ID: "M-3", Category: sdk.CategoryDone, State: sdk.NotificationStateUnread}}
	e.api.waiting = []sdk.WaitingTask{{Task: &sdk.Task{ID: "SEC-1"}}}
	got := menu(t, e)
	if got.Renderer != "" || got.RendererBundleURL != "" {
		t.Fatalf("P4 的 renderer 一律空：%+v", got)
	}
	var poolIDs []string
	badge := map[string]int32{}
	for _, it := range got.Pool {
		poolIDs = append(poolIDs, it.ID)
		badge[it.ID] = it.Badge
	}
	want := []string{"core.overview", "core.chat", "core.messages", "core.tasks", "core.reminders", "core.policy", "core.personas", "core.settings"}
	if strings.Join(poolIDs, ",") != strings.Join(want, ",") {
		t.Fatalf("pool = %v", poolIDs)
	}
	if badge["core.chat"] != 2 || badge["core.messages"] != 2 || badge["core.tasks"] != 1 || badge["core.overview"] != 3 || badge["core.reminders"] != 0 || badge["core.settings"] != 0 {
		t.Fatalf("徽章 = %v", badge)
	}
	// 一次就是完整的樹：預設樹 daily、settings（權限申請預設收到設定，MNU-10，工作群組空了不畫）。
	if strings.Join(ids(got.Tree), ",") != "daily,core.settings" {
		t.Fatalf("tree = %v", ids(got.Tree))
	}
	placed := 0
	for _, g := range got.Tree {
		if g.Route != "" && len(g.Children) == 0 {
			placed++
		}
		placed += len(g.Children)
	}
	if placed != 7 || len(got.Tucked) != 1 {
		t.Fatalf("樹裡放了 %d 個項目，要 7 個（另 1 個收到設定）：%+v", placed, got.Tree)
	}
	if got.Tree[0].Title != "日常" || got.Tree[0].Badge != 2+2+3+1 {
		t.Fatalf("群組：%+v", got.Tree)
	}
	// 預設樹的順序與名稱（S3、F6）：日常＝今天、聊天、待辦、訊息、任務單、介紹（DEV-117）；權限申請預設收到設定（MNU-10）。
	if strings.Join(ids(got.Tree[0].Children), ",") != "core.overview,core.chat,core.reminders,core.messages,core.tasks,core.personas" {
		t.Fatalf("預設樹的項目順序：%+v", got.Tree)
	}
	titles := map[string]string{}
	for _, it := range got.Pool {
		titles[it.ID] = it.Title
	}
	if titles["core.overview"] != "今天" || titles["core.reminders"] != "待辦" {
		t.Fatalf("核心項目名稱：%v", titles)
	}
	for _, it := range got.Pool {
		if it.Route == "" || it.Icon == "" || it.Title == "" {
			t.Fatalf("項目欄位不齊：%+v", it)
		}
	}
}

// Guards: REQ-106
func TestMenuTreeTwoLevelsOnly(t *testing.T) {
	e := newEnv(t)
	save := func(p *sdk.MenuPrefs) *httptest.ResponseRecorder {
		b, _ := json.Marshal(sdk.WebSetMenuPrefsRequest{Prefs: p})
		return post(e.h, "/api/web/set_menu_prefs", string(b))
	}
	// 第三層：群組的 children 裡放了群組。
	bad := save(&sdk.MenuPrefs{Tree: []sdk.MenuPrefEntry{
		{Group: "a", Title: "甲", Children: []string{"core.chat", "b"}},
		{Group: "b", Title: "乙", Children: []string{"core.tasks"}},
	}})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", bad.Code, bad.Body.String())
	}
	if er := decodeErr(t, bad); er.Code != sdk.ErrorCodeInvalid || er.Field != "entry.children" {
		t.Fatalf("error = %+v", er)
	}
	if _, err := os.Stat(filepath.Join(e.home, "settings", "menu.toml")); !os.IsNotExist(err) {
		t.Fatalf("被拒絕的選單不能寫檔：%v", err)
	}
	// 項目重複、群組沒名稱。
	if er := decodeErr(t, save(&sdk.MenuPrefs{Tree: []sdk.MenuPrefEntry{{Item: "core.chat"}, {Group: "a", Title: "甲", Children: []string{"core.chat"}}}})); er.Code != sdk.ErrorCodeInvalid {
		t.Fatalf("重複項目：%+v", er)
	}
	if er := decodeErr(t, save(&sdk.MenuPrefs{Tree: []sdk.MenuPrefEntry{{Group: "a", Title: "  ", Children: []string{"core.chat"}}}})); er.Code != sdk.ErrorCodeInvalid || er.Field != "entry.title" {
		t.Fatalf("沒有名稱：%+v", er)
	}
	// 兩層是可以的，存完立刻生效。
	ok := save(&sdk.MenuPrefs{Tree: []sdk.MenuPrefEntry{
		{Item: "core.overview"},
		{Group: "mine", Title: "我的", Icon: "star", Children: []string{"core.tasks", "core.chat"}},
		{Item: "core.settings"},
	}})
	if ok.Code != http.StatusOK {
		t.Fatalf("status %d: %s", ok.Code, ok.Body.String())
	}
	got := menu(t, e)
	if strings.Join(ids(got.Tree), ",") != "core.overview,mine,core.settings,unplaced" {
		t.Fatalf("tree = %v", ids(got.Tree))
	}
	if strings.Join(ids(got.Tree[1].Children), ",") != "core.tasks,core.chat" {
		t.Fatalf("children = %v", ids(got.Tree[1].Children))
	}
	data, err := os.ReadFile(filepath.Join(e.home, "settings", "menu.toml"))
	if err != nil || !strings.Contains(string(data), "schema = 1") || !strings.Contains(string(data), `group = "mine"`) {
		t.Fatalf("menu.toml: %s %v", data, err)
	}
	back := decode[sdk.WebGetMenuPrefsResponse](t, post(e.h, "/api/web/get_menu_prefs", "{}"))
	if len(back.Prefs.Tree) != 3 || back.Prefs.Tree[1].Icon != "star" {
		t.Fatalf("讀回來：%+v", back.Prefs)
	}
}

// Guards: REQ-106
func TestMenuUnplacedGroup(t *testing.T) {
	e := newEnv(t)
	// 預設樹把 7 個都放好了：沒有「未分類」。
	if got := menu(t, e); strings.Contains(strings.Join(ids(got.Tree), ","), "unplaced") {
		t.Fatalf("全部都放好時不該有未分類：%v", ids(got.Tree))
	}
	write(t, filepath.Join(e.home, "settings", "menu.toml"), `schema = 1
[[entry]]
group = "work"
title = "工作"
children = ["core.tasks"]
`)
	got := menu(t, e)
	if strings.Join(ids(got.Tree), ",") != "work,unplaced,core.settings" {
		t.Fatalf("tree = %v", ids(got.Tree))
	}
	un := got.Tree[1]
	if un.Title != "未分類" || strings.Join(ids(un.Children), ",") != "core.overview,core.chat,core.messages,core.reminders,core.personas" {
		t.Fatalf("未分類 = %+v", un)
	}
}

// Guards: REQ-106
func TestSettingsAlwaysReachable(t *testing.T) {
	e := newEnv(t)
	write(t, filepath.Join(e.home, "settings", "menu.toml"), `schema = 1
[[entry]]
item = "core.overview"
`)
	got := menu(t, e)
	if last := got.Tree[len(got.Tree)-1]; last.ID != "core.settings" || last.Route != "/settings" {
		t.Fatalf("設定要在最後：%v", ids(got.Tree))
	}
	// 就算整個偏好檔壞掉，也退回預設樹，設定還在。
	write(t, filepath.Join(e.home, "settings", "menu.toml"), "這不是 toml = = =")
	got = menu(t, e)
	if strings.Join(ids(got.Tree), ",") != "daily,core.settings" {
		t.Fatalf("壞檔案要退回預設樹：%v", ids(got.Tree))
	}
	// 使用者把設定放進群組也可以（只要可達）。
	write(t, filepath.Join(e.home, "settings", "menu.toml"), `schema = 1
[[entry]]
group = "more"
title = "更多"
children = ["core.settings"]
`)
	got = menu(t, e)
	if got.Tree[0].ID != "more" || got.Tree[0].Children[0].ID != "core.settings" {
		t.Fatalf("tree = %+v", got.Tree)
	}
}

// Guards: REQ-106
func TestStaleItemsIgnored(t *testing.T) {
	e := newEnv(t)
	body := `schema = 1
[[entry]]
item = "acme.report"
[[entry]]
group = "gone"
title = "消失的群組"
children = ["acme.a", "acme.b"]
[[entry]]
group = "work"
title = "工作"
children = ["acme.c", "core.tasks"]
`
	path := filepath.Join(e.home, "settings", "menu.toml")
	write(t, path, body)
	got := menu(t, e)
	if strings.Join(ids(got.Tree), ",") != "work,unplaced,core.settings" {
		t.Fatalf("tree = %v", ids(got.Tree))
	}
	if strings.Join(ids(got.Tree[0].Children), ",") != "core.tasks" {
		t.Fatalf("失效的項目要略過：%+v", got.Tree[0])
	}
	// 偏好檔保留失效的項目（套件重裝時選擇還在）。
	if data, _ := os.ReadFile(path); string(data) != body {
		t.Fatalf("算選單不能改偏好檔：%s", data)
	}
}

// 徽章的數字全部從首頁彙整（overview）來：總覽是 notify 的要你同意 ＋ 等你處理的任務單，聊天是等你回覆的討論串，訊息是未讀數。
// 來源失敗：sdk 錯誤原樣轉出，其他錯誤一律 unavailable（白話）；沒接上來源也是 unavailable；不需要總覽的方法（頁面、選單偏好）照常。
// Guards: REQ-109
func TestMenuBadgesComeFromOverview(t *testing.T) {
	e := newEnv(t)
	e.api.err = &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "x"}
	if w := post(e.h, "/api/web/get_menu", "{}"); w.Code != http.StatusNotFound {
		t.Fatalf("sdk 錯誤要原樣轉出：%d %s", w.Code, w.Body.String())
	}
	e.api.err = errors.New("boom")
	if w := post(e.h, "/api/web/get_menu", "{}"); w.Code != http.StatusServiceUnavailable || decodeErr(t, w).Code != sdk.ErrorCodeUnavailable {
		t.Fatalf("其他錯誤：%d %s", w.Code, w.Body.String())
	}
	// 不需要總覽的方法照常。
	if pages := decode[sdk.WebGetPagesResponse](t, post(e.h, "/api/web/get_pages", "{}")); len(pages.Pages) != 8 {
		t.Fatalf("pages = %+v", pages)
	}
	if prefs := decode[sdk.WebGetMenuPrefsResponse](t, post(e.h, "/api/web/get_menu_prefs", "{}")); len(prefs.Prefs.Tree) == 0 {
		t.Fatalf("prefs = %+v", prefs)
	}
	// 沒接上總覽。
	m := New(Deps{})
	m.SetHome(e.home)
	if _, err := m.Compute(t.Context()); err == nil {
		t.Fatal("沒接上總覽應該回 unavailable")
	} else if se, ok := err.(*sdk.Error); !ok || se.Code != sdk.ErrorCodeUnavailable {
		t.Fatalf("err = %v", err)
	}
}

// 登記：sdk 的 web 服務只有選單與頁面的四個方法由這裡掛（Mount.Methods），其餘方法由別的業務模組掛。
func TestRoutesOnlyMenuMethods(t *testing.T) {
	mounts := New(Deps{}).Routes()
	if len(mounts) != 1 || mounts[0].Service != "web" || strings.Join(mounts[0].Methods, ",") != "get_menu,get_pages,get_menu_prefs,set_menu_prefs" {
		t.Fatalf("Routes = %+v", mounts)
	}
}

// 只有主人（by 是 owner(…)）可以改選單。
// Guards: REQ-106
func TestSetMenuPrefsOwnerOnly(t *testing.T) {
	e := newEnv(t)
	mux, err := app.New(e.m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(sdk.WebSetMenuPrefsRequest{Prefs: &sdk.MenuPrefs{Tree: []sdk.MenuPrefEntry{{Item: "core.chat"}}}, By: &sdk.Actor{Name: "code"}})
	w := post(mux, "/api/web/set_menu_prefs", string(b))
	if w.Code != http.StatusForbidden || decodeErr(t, w).Field != "by" {
		t.Fatalf("秘書不能改選單：%d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(e.home, "settings", "menu.toml")); !os.IsNotExist(err) {
		t.Fatalf("被拒絕的選單不能寫檔：%v", err)
	}
}

// pages 是 web.get_pages 的結果（依頁面 id）；套件頁面的編譯在這裡發生。
func pages(t *testing.T, e *env) map[string]sdk.PageInfo {
	t.Helper()
	out := map[string]sdk.PageInfo{}
	for _, p := range decode[sdk.WebGetPagesResponse](t, post(e.h, "/api/web/get_pages", "{}")).Pages {
		out[p.ID] = p
	}
	return out
}
