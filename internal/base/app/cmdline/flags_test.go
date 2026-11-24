package cmdline

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 旗標名就是請求的 JSON 欄位名；測試用自己的請求型別，不綁任何業務模組。
type testReq struct {
	ID       string           `json:"id"`
	Content  string           `json:"content"`
	Count    int              `json:"count"`
	Flag     bool             `json:"flag"`
	At       time.Time        `json:"at"`
	Opt      *string          `json:"opt"`
	Tags     []string         `json:"tags"`
	Choices  []string         `json:"choices"`
	Statuses []string         `json:"statuses"`
	Page     *sdk.PageRequest `json:"page"`
	By       *sdk.Actor       `json:"by"`
	Nested   struct{}         `json:"nested"`
}

func newEnv() *Env { return &Env{By: "tester", Out: &bytes.Buffer{}, Err: &bytes.Buffer{}} }

func TestPrepareFillsFlagsAndActor(t *testing.T) {
	p, err := Prepare[testReq](newEnv(), []string{"--content", "繳費", "--count=3", "--flag", "--opt", "x", "--tags", "a", "--tags=b", "--choices", "好|不要"},
		Spec{Split: map[string]string{"choices": "|"}})
	if err != nil {
		t.Fatal(err)
	}
	r := p.Req
	if r.Content != "繳費" || r.Count != 3 || !r.Flag || r.Opt == nil || *r.Opt != "x" || !reflect.DeepEqual(r.Tags, []string{"a", "b"}) || !reflect.DeepEqual(r.Choices, []string{"好", "不要"}) {
		t.Fatalf("%+v", r)
	}
	if r.By == nil || r.By.Name != "tester" {
		t.Fatalf("by 要填執行環境的操作者：%+v", r.By)
	}
	if !p.Set["content"] || p.Set["id"] {
		t.Fatalf("Set 只記命令列上有給的旗標：%v", p.Set)
	}
}

func TestPrepareTimeFlag(t *testing.T) {
	p, err := Prepare[testReq](newEnv(), []string{"--at", "2026-10-25T09:30"}, Spec{})
	if err != nil || !p.Req.At.Equal(time.Date(2026, 10, 25, 9, 30, 0, 0, time.Local)) {
		t.Fatalf("沒有時區的時間照本機時間：%v %v", p.Req.At, err)
	}
	p, err = Prepare[testReq](newEnv(), []string{"--at", "2026-10-25T09:30:00+08:00"}, Spec{})
	if err != nil || !p.Req.At.Equal(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("RFC3339：%v %v", p.Req.At, err)
	}
}

func TestPrepareUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"壞時間":       {"--at", "明天"},
		"布林亂填":      {"--flag=maybe"},
		"整數亂填":      {"--count", "x"},
		"不認得的旗標":    {"--nope", "x"},
		"旗標少了值":     {"--content"},
		"多餘的位置參數":   {"stray"},
		"by 不能由旗標給": {"--by", "x"},
	}
	for name, args := range cases {
		_, err := Prepare[testReq](newEnv(), args, Spec{})
		var ue *UsageError
		if !toUsageError(err, &ue) {
			t.Errorf("%s：%v 應該是用法錯誤", name, err)
		}
	}
}

func toUsageError(err error, target **UsageError) bool {
	ue, ok := err.(*UsageError)
	*target = ue
	return ok
}

func TestPrepareIDAndRenameAndSkip(t *testing.T) {
	p, err := Prepare[testReq](newEnv(), []string{"R-1", "--status", "todo"}, Spec{ID: true, Rename: map[string]string{"status": "statuses"}})
	if err != nil || p.Req.ID != "R-1" || !reflect.DeepEqual(p.Req.Statuses, []string{"todo"}) || !p.Set["statuses"] {
		t.Fatalf("%+v %v", p.Req, err)
	}
	for _, args := range [][]string{nil, {"R-1", "R-2"}, {"R-1", "--id", "R-2"}} {
		if _, err := Prepare[testReq](newEnv(), args, Spec{ID: true}); err == nil {
			t.Errorf("%v 應該是用法錯誤", args)
		}
	}
	p, err = Prepare[testReq](newEnv(), []string{"a", "b"}, Spec{Free: true})
	if err != nil || !reflect.DeepEqual(p.Pos, []string{"a", "b"}) {
		t.Fatalf("Free 把位置參數留給指令：%v %v", p.Pos, err)
	}
	if _, err := Prepare[testReq](newEnv(), []string{"--content", "x"}, Spec{Skip: []string{"content"}}); err == nil {
		t.Fatal("Skip 的欄位不開旗標")
	}
}

type testResp struct{ Items []string }

func TestOnePrintsOneLineUnescaped(t *testing.T) {
	e := newEnv()
	call := func(_ context.Context, r *testReq) (*testResp, error) {
		return &testResp{Items: []string{r.Content}}, nil
	}
	if err := One(e, []string{"--content", "<報表> & 備份"}, Spec{}, call, func(r *testResp) any { return r.Items }); err != nil {
		t.Fatal(err)
	}
	if got := e.Out.(*bytes.Buffer).String(); got != "[\"<報表> & 備份\"]\n" {
		t.Fatalf("%q", got)
	}
}

func TestEachFollowsCursorOnceAndPrintsLines(t *testing.T) {
	e := newEnv()
	var cursors []string
	call := func(_ context.Context, r *testReq) (*testResp, error) {
		if r.Page == nil {
			return &testResp{Items: []string{"a", "b"}}, nil
		}
		cursors = append(cursors, r.Page.Cursor)
		return &testResp{Items: []string{"c"}}, nil
	}
	pages := 0
	items := func(r *testResp) ([]string, string) {
		pages++
		if pages == 1 {
			return r.Items, "p2"
		}
		return r.Items, "" // 沒有下一頁
	}
	err := Each(e, nil, Spec{}, call, func(r *testReq, c string) { r.Page = &sdk.PageRequest{Cursor: c} }, items)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Out.(*bytes.Buffer).String(); got != "\"a\"\n\"b\"\n\"c\"\n" || !reflect.DeepEqual(cursors, []string{"p2"}) {
		t.Fatalf("%q %v", got, cursors)
	}
}

func TestNextCursor(t *testing.T) {
	if NextCursor(nil) != "" || NextCursor(&sdk.PageResponse{NextCursor: "n"}) != "n" {
		t.Fatal("NextCursor")
	}
}

// 用法錯誤經 Run 回結束碼 2，且 stderr 有一行白話。
func TestRunMapsPrepareErrorToExitTwo(t *testing.T) {
	cmds := map[string]Command{"x": {Run: func(e *Env, a []string) error {
		_, err := Prepare[testReq](e, a, Spec{})
		return err
	}}}
	code, out, errb := run(t, cmds, "x", "--at", "明天")
	if code != 2 || out != "" || !strings.Contains(errb, "--at") {
		t.Fatalf("%d %q %q", code, out, errb)
	}
}

// 子指令的 --help 照請求型別列旗標：名稱、型別、列舉值；by、巢狀結構不列；位置參數是編號的要說（DEV-89）。
type helpReq struct {
	ID      string     `json:"id"`
	Content string     `json:"content"`
	Follow  sdk.Follow `json:"follow"`
	Count   int        `json:"count"`
	Flag    bool       `json:"flag"`
	At      time.Time  `json:"at"`
	Tags    []string   `json:"tags"`
	Page    *sdk.PageRequest
	By      *sdk.Actor `json:"by"`
}

func TestPrepareHelpListsFlags(t *testing.T) {
	cmds := map[string]Command{"x y": {Run: func(e *Env, a []string) error {
		_, err := Prepare[helpReq](e, a, Spec{ID: true, Skip: []string{"count"}})
		return err
	}}}
	for _, flag := range []string{"--help", "-h"} {
		code, out, errb := run(t, cmds, "x", "y", "SEC-1", flag)
		if code != 0 || errb != "" {
			t.Fatalf("%s：%d %q %q", flag, code, out, errb)
		}
		for _, want := range []string{"evergenie x y <編號>", "--content", "--follow", "none|remind|confirm|check|discuss", "--flag", "--at", "時間", "--tags", "可重複"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s 的說明少了 %q：\n%s", flag, want, out)
			}
		}
		for _, no := range []string{"--by", "--page", "--count", "--id"} {
			if strings.Contains(out, no) {
				t.Fatalf("%s 的說明不該有 %q：\n%s", flag, no, out)
			}
		}
	}
}

// 不認得的旗標：列出這個指令能用的旗標，不用再去翻 ai_docs（DEV-89）。
func TestPrepareUnknownFlagListsValid(t *testing.T) {
	_, err := Prepare[helpReq](newEnv(), []string{"--title", "x"}, Spec{})
	var ue *UsageError
	if !toUsageError(err, &ue) || !strings.Contains(ue.Msg, "--title") || !strings.Contains(ue.Msg, "--content") || !strings.Contains(ue.Msg, "--follow") || strings.Contains(ue.Msg, "--by") {
		t.Fatal(err)
	}
}

// API 說缺欄位時，訊息帶上是哪個欄位（DEV-89）。
func TestMessageNamesField(t *testing.T) {
	e := newEnv()
	e.Ctx = context.Background()
	if got := e.Message(&sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "請填寫必要欄位", Field: "content"}); got != "請填寫必要欄位（欄位：content，命令列是 --content）" {
		t.Fatal(got)
	}
	if got := e.Message(&sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "content 太長", Field: "content"}); got != "content 太長" {
		t.Fatal(got)
	}
	// home.toml 的設定名（帶點）沒有對應的旗標，不能叫人打 --web.owner_login。
	if got := e.Message(&sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "主人的登入要像 email", Field: "web.owner_login"}); got != "主人的登入要像 email" {
		t.Fatal(got)
	}
}

// 手寫旗標的指令：--help 印自己的用法、結束 0（DEV-89）。
func TestParseOptsHelp(t *testing.T) {
	cmds := map[string]Command{"z": {Run: func(e *Env, a []string) error {
		_, err := ParseOpts(a, []string{"out"}, nil, "z <名字> [--out <資料夾>]")
		return err
	}}}
	code, out, _ := run(t, cmds, "z", "--help")
	if code != 0 || out != "用法：z <名字> [--out <資料夾>]\n" {
		t.Fatalf("%d %q", code, out)
	}
}
