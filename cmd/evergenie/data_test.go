package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

const noteSchemaFile = `schema = 1
title = "筆記"
date_field = "date"
[[field]]
name = "date"
type = "date"
description = "哪一天"
[[field]]
name = "text"
type = "string"
required = true
description = "內容"
[[field]]
name = "score"
type = "number"
description = "分數"
[[field]]
name = "mood"
type = "enum"
enum = ["good", "bad"]
`

const daySchemaFile = `schema = 1
title = "一天"
[[field]]
name = "weight"
type = "number"
required = true
`

// writePackage 在家的 packages/mine/ 放一個套件（測試用）。
func writePackage(t *testing.T, home, id, manifest string, files map[string]string) {
	t.Helper()
	all := map[string]string{"package.toml": manifest}
	for k, v := range files {
		all[k] = v
	}
	for rel, body := range all {
		p := filepath.Join(home, "packages", "mine", id, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func demoManifest(extra string) string {
	return `schema = 1
id = "demo"
name = "示範"
version = "1.0.0"
description = "資料指令測試用"
kind = "data"
api = "evergenie.v1"
[[contributes.data]]
kind = "note"
schema = "schemas/note.toml"
ids = "seq"
prefix = "N-"
[[contributes.data]]
kind = "day"
schema = "schemas/day.toml"
ids = "given"
` + extra
}

// TestDataCommandsOnFixtureHome：真的 serve＋fixture 家＋一個宣告了資料的套件，跑 data kinds／add／list／get／set／rm；
// 欄位不符被擋並說明，given 種類不給編號被擋，核心種類是用法錯誤，套件停用資料就看不到、啟用又回來。
// Guards: REQ-173
func TestDataCommandsOnFixtureHome(t *testing.T) {
	_, sock := startFixtureAPIWith(t, func(dir string) {
		writePackage(t, dir, "demo", demoManifest(""), map[string]string{"schemas/note.toml": noteSchemaFile, "schemas/day.toml": daySchemaFile})
	})
	t.Setenv("EVERGENIE_AGENT", "fitness")

	help := hgOK(t, sock, "data", "--help")
	for _, sub := range []string{"kinds", "list", "get", "add", "set", "rm"} {
		if !strings.Contains(help, sub) {
			t.Errorf("data --help 少了 %s", sub)
		}
	}
	kinds := hgOK(t, sock, "data", "kinds")
	for _, want := range []string{"demo.note", "筆記", "N-<數字>", "demo.day", "編號要自己給", "text", "必填", "enum(good|bad)", "哪一天"} {
		if !strings.Contains(kinds, want) {
			t.Errorf("data kinds 少了「%s」：\n%s", want, kinds)
		}
	}
	var seen []string
	for _, line := range strings.Split(strings.TrimSpace(hgOK(t, sock, "data", "kinds", "--json")), "\n") {
		var k struct {
			Package, Kind, IDs, Prefix string
			Fields                     []sdk.DataFieldDecl
		}
		if err := json.Unmarshal([]byte(line), &k); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		seen = append(seen, k.Kind)
		if k.Package != "demo" {
			t.Errorf("package：%+v", k)
		}
	}
	if strings.Join(seen, ",") != "demo.day,demo.note" {
		t.Fatalf("kinds：%v", seen)
	}

	// 新增：seq 種類由核心發編號。
	if out := strings.TrimSpace(hgOK(t, sock, "data", "add", "demo.note", "--json", `{"date":"2026-10-20","text":"舊的","score":3}`)); out != "N-1" {
		t.Fatalf("第一筆：%q", out)
	}
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("內文第一行\n第二行\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := strings.TrimSpace(hgOK(t, sock, "data", "add", "demo.note", "--json", `{"date":"2026-10-25","text":"新的","score":4.5,"mood":"good"}`, "--body-file", bodyFile)); out != "N-2" {
		t.Fatalf("第二筆：%q", out)
	}
	code, out, errOut := hg(t, sock, `{"text":"從標準輸入","date":"2026-10-22"}`, "data", "add", "demo.note", "--json", "-")
	if code != 0 || strings.TrimSpace(out) != "N-3" {
		t.Fatalf("stdin 新增：%d %q %q", code, out, errOut)
	}
	// seq 種類不能給 --id；given 種類一定要給；格式不對的 id 被核心擋。
	if code, _, errOut := hg(t, sock, "", "data", "add", "demo.note", "--id", "N-9", "--json", `{"text":"x"}`); code != 2 || !strings.Contains(errOut, "不能給 --id") {
		t.Fatalf("seq 給 id：%d %q", code, errOut)
	}
	if code, _, errOut := hg(t, sock, "", "data", "add", "demo.day", "--json", `{"weight":70}`); code != 2 || !strings.Contains(errOut, "編號要自己給") {
		t.Fatalf("given 不給 id 要被擋：%d %q", code, errOut)
	}
	if code, _, errOut := hg(t, sock, "", "data", "add", "demo.day", "--id", "Bad_Id", "--json", `{"weight":70}`); code != 1 || !strings.Contains(errOut, "編號") {
		t.Fatalf("given 編號格式：%d %q", code, errOut)
	}
	if out := strings.TrimSpace(hgOK(t, sock, "data", "add", "demo.day", "--id", "2026-10-25", "--json", `{"weight":70.5}`)); out != "2026-10-25" {
		t.Fatalf("given 新增：%q", out)
	}
	// 欄位不符：缺必填、型別錯、enum 外、不在規格裡的欄位；都被擋並說明（沒有存進去）。
	for _, c := range []struct{ json, want string }{
		{`{"score":1}`, "text"},
		{`{"text":"x","score":"高"}`, "score"},
		{`{"text":"x","mood":"meh"}`, "mood"},
		{`{"text":"x","kcla":1}`, "kcla"},
		{`{"text":"x","date":"明天"}`, "date"},
		{`[1]`, "JSON 物件"},
	} {
		code, _, errOut := hg(t, sock, "", "data", "add", "demo.note", "--json", c.json)
		if code == 0 || !strings.Contains(errOut, c.want) {
			t.Errorf("add %s 要被擋並提到 %s：%d %q", c.json, c.want, code, errOut)
		}
	}

	// list：新到舊（先看日期，沒有日期用建立日）；--since／--until／--limit。
	list := strings.Split(strings.TrimSpace(hgOK(t, sock, "data", "list", "demo.note")), "\n")
	if len(list) != 3 || !strings.HasPrefix(list[0], "N-2  2026-10-25") || !strings.HasPrefix(list[1], "N-3  2026-10-22") || !strings.HasPrefix(list[2], "N-1  2026-10-20") {
		t.Fatalf("list：%q", list)
	}
	if !strings.Contains(list[0], "text=新的") || !strings.Contains(list[0], "score=4.5") {
		t.Fatalf("list 摘要：%q", list[0])
	}
	since := strings.TrimSpace(hgOK(t, sock, "data", "list", "demo.note", "--since", "2026-10-21", "--until", "2026-10-25"))
	if lines := strings.Split(since, "\n"); len(lines) != 2 || !strings.HasPrefix(lines[0], "N-2") || !strings.HasPrefix(lines[1], "N-3") {
		t.Fatalf("--since／--until：%q", since)
	}
	if one := strings.TrimSpace(hgOK(t, sock, "data", "list", "demo.note", "--limit", "1")); strings.Count(one, "\n") != 0 || !strings.HasPrefix(one, "N-2") {
		t.Fatalf("--limit：%q", one)
	}
	// 這一筆沒填日期欄位：用建立日（今天），所以 --since 一個很早的日期會列到它。
	undated := strings.TrimSpace(hgOK(t, sock, "data", "add", "demo.note", "--json", `{"text":"沒填日期"}`))
	if !strings.HasPrefix(undated, "N-") {
		t.Fatalf("沒填日期的那筆：%q", undated)
	}
	if early := hgOK(t, sock, "data", "list", "demo.note", "--since", "2000-01-01"); !strings.Contains(early, undated) {
		t.Fatalf("沒填日期的用建立日：%q", early)
	}
	hgOK(t, sock, "data", "rm", "demo.note", undated)
	jl := strings.Split(strings.TrimSpace(hgOK(t, sock, "data", "list", "demo.note", "--json")), "\n")
	if len(jl) != 3 {
		t.Fatalf("list --json：%q", jl)
	}

	// get：文字與 --json。
	get := hgOK(t, sock, "data", "get", "demo.note", "N-2")
	for _, want := range []string{"N-2（筆記）", "text: 新的", "mood: good", "內文第一行"} {
		if !strings.Contains(get, want) {
			t.Errorf("get 少了「%s」：\n%s", want, get)
		}
	}
	rec := jsonLine(t, hgOK(t, sock, "data", "get", "demo.note", "N-2", "--json"))
	if rec["id"] != "N-2" || rec["kind"] != "demo.note" || !strings.Contains(rec["body"].(string), "第二行") {
		t.Fatalf("get --json：%v", rec)
	}
	if code, _, _ := hg(t, sock, "", "data", "get", "demo.note", "N-99"); code != 1 {
		t.Fatalf("不存在要失敗：%d", code)
	}

	// set：合併頂層欄位，null 拿掉，內文不動；欄位不符不改。
	hgOK(t, sock, "data", "set", "demo.note", "N-2", "--json", `{"text":"改過","score":null}`)
	after := jsonLine(t, hgOK(t, sock, "data", "get", "demo.note", "N-2", "--json"))
	fields := after["fields"].(map[string]any)
	if fields["text"] != "改過" || fields["mood"] != "good" || fields["by"] != "fitness" || !strings.Contains(after["body"].(string), "內文第一行") {
		t.Fatalf("set 之後：%v", after)
	}
	if _, has := fields["score"]; has {
		t.Fatalf("null 的欄位要拿掉：%v", fields)
	}
	if code, _, errOut := hg(t, sock, "", "data", "set", "demo.note", "N-2", "--json", `{"mood":"meh"}`); code == 0 || !strings.Contains(errOut, "mood") {
		t.Fatalf("set 欄位不符：%d %q", code, errOut)
	}
	if code, _, errOut := hg(t, sock, "", "data", "set", "demo.note", "N-2", "--json", `{"text":null}`); code == 0 || !strings.Contains(errOut, "text") {
		t.Fatalf("不能拿掉必填欄位：%d %q", code, errOut)
	}

	// rm：封存不刪檔；列表看不到，get 還查得到。
	if out := hgOK(t, sock, "data", "rm", "demo.note", "N-1"); !strings.Contains(out, "沒有刪檔") {
		t.Fatalf("rm：%q", out)
	}
	if left := strings.TrimSpace(hgOK(t, sock, "data", "list", "demo.note")); strings.Contains(left, "N-1") {
		t.Fatalf("封存後列表：%q", left)
	}
	if got := jsonLine(t, hgOK(t, sock, "data", "get", "demo.note", "N-1", "--json")); got["archived"] != true {
		t.Fatalf("封存的還查得到：%v", got)
	}

	// 自己給編號的種類封存後，同編號新增成功並列得出來；修改不碰封存那筆；錯誤不說「資料已更新」（DEV-151）。
	if code, _, errOut := hg(t, sock, "", "data", "add", "demo.day", "--id", "2026-10-25", "--json", `{"weight":71}`); code != 1 || !strings.Contains(errOut, "已經有資料") || strings.Contains(errOut, "資料已更新") {
		t.Fatalf("正本還在時重複新增：%d %q", code, errOut)
	}
	hgOK(t, sock, "data", "rm", "demo.day", "2026-10-25")
	if code, _, errOut := hg(t, sock, "", "data", "set", "demo.day", "2026-10-25", "--json", `{"weight":99}`); code != 1 || !strings.Contains(errOut, "已封存") || strings.Contains(errOut, "資料已更新") {
		t.Fatalf("修改封存的要回找不到：%d %q", code, errOut)
	}
	if got := jsonLine(t, hgOK(t, sock, "data", "get", "demo.day", "2026-10-25", "--json")); got["fields"].(map[string]any)["weight"] != 70.5 {
		t.Fatalf("封存那筆不該被改：%v", got)
	}
	if out := strings.TrimSpace(hgOK(t, sock, "data", "add", "demo.day", "--id", "2026-10-25", "--json", `{"weight":72}`)); out != "2026-10-25" {
		t.Fatalf("封存後同編號新增：%q", out)
	}
	if days := hgOK(t, sock, "data", "list", "demo.day"); !strings.Contains(days, "2026-10-25") || !strings.Contains(days, "weight=72") {
		t.Fatalf("新的一筆要出現在列表：%q", days)
	}
	hgOK(t, sock, "data", "set", "demo.day", "2026-10-25", "--json", `{"weight":73}`)
	if days := hgOK(t, sock, "data", "list", "demo.day"); !strings.Contains(days, "weight=73") {
		t.Fatalf("修改改到新的那筆：%q", days)
	}

	// 核心種類不歸這裡管：用法錯誤（結束碼 2）。
	for _, args := range [][]string{{"data", "list", "task"}, {"data", "add", "reminder", "--json", "{}"}, {"data", "get", "agent", "code"}} {
		if code, _, errOut := hg(t, sock, "", args...); code != 2 || !strings.Contains(errOut, "data 只管套件的資料；任務、待辦請用各自的指令") {
			t.Fatalf("%v：%d %q", args, code, errOut)
		}
	}
	if code, _, errOut := hg(t, sock, "", "data", "list", "nobody.thing"); code != 1 || !strings.Contains(errOut, "沒有「nobody.thing」這種資料") {
		t.Fatalf("不認得的種類：%d %q", code, errOut)
	}

	// 套件停用：資料看不到、不能寫；啟用就回來（檔案一直在）。
	pk := sdk.NewPackageClient(sdk.Dial(sock))
	by := &sdk.Actor{Name: "owner(cli)"}
	if _, err := pk.SetEnabled(context.Background(), &sdk.PackageSetEnabledRequest{ID: "demo", Enabled: false, By: by}); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := hg(t, sock, "", "data", "list", "demo.note"); code != 1 || !strings.Contains(errOut, "沒有「demo.note」這種資料") {
		t.Fatalf("停用後：%d %q", code, errOut)
	}
	if strings.Contains(hgOK(t, sock, "data", "kinds"), "demo.note") {
		t.Fatal("停用後 kinds 不該列出")
	}
	if _, err := pk.SetEnabled(context.Background(), &sdk.PackageSetEnabledRequest{ID: "demo", Enabled: true, By: by}); err != nil {
		t.Fatal(err)
	}
	if back := hgOK(t, sock, "data", "list", "demo.note"); !strings.Contains(back, "N-3") && !strings.Contains(back, "N-2") {
		t.Fatalf("啟用後資料回來：%q", back)
	}
}

const trainerTemplate = `---
duty: 管健身計畫
color: "#2f8f6f"
mode: ondemand
---
你是健身教練。
每週一提醒量體重。
`

const weighTemplate = `---
name: 提醒量體重
cron: "0 8 * * 1"
catchup: skip
---
提醒主人早上量體重。
`

// TestPackagesSyncOnFixtureHome：真的 serve＋fixture 家：預演不改任何東西；秘書不能套用；沒有預設人設時說清楚；
// 主人套用之後秘書與排程真的出現（排程的來源是套件 id），再同步一次什麼都不動。
// Guards: REQ-174
func TestPackagesSyncOnFixtureHome(t *testing.T) {
	dir, sock := startFixtureAPIWith(t, func(dir string) {
		writePackage(t, dir, "fit", `schema = 1
id = "fit"
name = "健身"
version = "1.0.0"
description = "同步測試用"
kind = "data"
api = "evergenie.v1"
[[contributes.agent]]
id = "trainer"
template = "agents/trainer/agent.md"
[[contributes.job]]
agent = "trainer"
id = "weigh"
path = "jobs/weigh.md"
`, map[string]string{"agents/trainer/agent.md": trainerTemplate, "jobs/weigh.md": weighTemplate})
	})

	pre := hgOK(t, sock, "packages", "sync", "fit")
	for _, want := range []string{"預演", "trainer", "新增", "trainer/weigh"} {
		if !strings.Contains(pre, want) {
			t.Errorf("預演少了「%s」：\n%s", want, pre)
		}
	}
	if strings.Contains(hgOK(t, sock, "agents", "list"), "trainer") {
		t.Fatal("預演不能新增秘書")
	}

	// 秘書自己不能套用。
	t.Setenv("EVERGENIE_AGENT", "code")
	if code, _, errOut := hg(t, sock, "", "packages", "sync", "fit", "--apply"); code != 1 || !strings.Contains(errOut, "只有主人能套用套件的職責與排程") {
		t.Fatalf("秘書套用：%d %q", code, errOut)
	}
	t.Setenv("EVERGENIE_AGENT", "")
	// 主人套用，但還沒有預設人設。
	if code, _, errOut := hg(t, sock, "", "packages", "sync", "fit", "--apply"); code != 1 || !strings.Contains(errOut, "預設人設") {
		t.Fatalf("沒有預設人設：%d %q", code, errOut)
	}
	if strings.Contains(hgOK(t, sock, "agents", "list"), "trainer") {
		t.Fatal("失敗不能留下半套")
	}
	hgOK(t, sock, "config", "set", "defaults.persona", "builder")

	out := hgOK(t, sock, "packages", "sync", "fit", "--apply")
	if !strings.Contains(out, "已套用") {
		t.Fatalf("套用輸出：%s", out)
	}
	agents := hgOK(t, sock, "agents", "list")
	if !strings.Contains(agents, "trainer") || !strings.Contains(agents, "builder") {
		t.Fatalf("秘書要出現：%s", agents)
	}
	r, err := sdk.NewVaultClient(sdk.Dial(sock)).Get(context.Background(), &sdk.VaultGetRequest{Kind: "agent", ID: "trainer"})
	if err != nil {
		t.Fatal(err)
	}
	f := r.Record.Fields
	if f["duty"] != "管健身計畫" || f["color"] != "#2f8f6f" || f["mode"] != "ondemand" || f["persona"] != "builder" || f["enabled"] != false || !strings.Contains(r.Record.Body, "每週一提醒量體重") {
		t.Fatalf("新秘書：%v body=%q", f, r.Record.Body)
	}
	job := jsonLine(t, hgOK(t, sock, "jobs", "list", "--agent", "trainer", "--json"))
	if job["id"] != "trainer/weigh" || job["source"] != "fit" || job["enabled"] != true || !strings.Contains(job["body"].(string), "量體重") {
		t.Fatalf("新排程：%v", job)
	}
	// 再同步一次：全部相同，什麼都不動（排程不會被重新建立或改掉）。
	again := hgOK(t, sock, "packages", "sync", "fit", "--apply")
	if !strings.Contains(again, "職責相同，不動") || !strings.Contains(again, "已存在，不改") || strings.Contains(again, "新增") {
		t.Fatalf("第二次：%s", again)
	}
	// 主人在附加規則加的字，官方更新（update）不會蓋掉；職責被改過就會被列為更新。
	hgOK(t, sock, "customize", "add", "trainer", "我的規則：先講結論")
	if err := os.WriteFile(filepath.Join(dir, "packages", "mine", "fit", "agents", "trainer", "agent.md"), []byte(strings.Replace(trainerTemplate, "每週一提醒量體重。", "每週一提醒量體重。\n新增的一條規則。", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if upd := hgOK(t, sock, "packages", "sync", "fit"); !strings.Contains(upd, "更新職責（新增 1 行、移除 0 行") {
		t.Fatalf("職責變了：%s", upd)
	}
	hgOK(t, sock, "packages", "sync", "fit", "--apply")
	show := hgOK(t, sock, "customize", "show", "trainer")
	if !strings.Contains(show, "新增的一條規則") || !strings.Contains(show, "我的規則：先講結論") {
		t.Fatalf("職責更新、附加規則不動：\n%s", show)
	}
}
