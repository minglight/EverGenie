package roster

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// notesStore 是記憶與日誌的假服務：vault.get 讀、agents.write_memory／append_journal 寫，跟名冊模組一樣只准本人寫。
type notesStore struct {
	mu   sync.Mutex
	rows map[string]string // "<kind>/<id>" → 內文
}

type notesVault struct {
	sdk.VaultAPI
	s *notesStore
}

func (v notesVault) Get(_ context.Context, r *sdk.VaultGetRequest) (*sdk.VaultGetResponse, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	body, ok := v.s.rows[r.Kind+"/"+r.ID]
	if !ok {
		return nil, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "沒有這一筆"}
	}
	return &sdk.VaultGetResponse{Record: &sdk.Record{Kind: r.Kind, ID: r.ID, Body: body, Revision: "rev-" + body, Fields: map[string]any{"by": "life", "updated_at": "2026-11-15T10:00:00+08:00"}}}, nil
}

func (v notesVault) List(_ context.Context, r *sdk.VaultListRequest) (*sdk.VaultListResponse, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	out := []sdk.Record{}
	for key, body := range v.s.rows {
		if id, ok := strings.CutPrefix(key, r.Kind+"/"); ok {
			out = append(out, sdk.Record{Kind: r.Kind, ID: id, Body: body, Revision: "rev-" + body, Fields: map[string]any{"by": "life", "updated_at": "2026-11-15T10:00:00+08:00"}})
		}
	}
	return &sdk.VaultListResponse{Records: out}, nil
}

type notesAgents struct {
	sdk.AgentsAPI
	s *notesStore
}

func (a notesAgents) WriteMemory(_ context.Context, r *sdk.AgentsWriteMemoryRequest) (*sdk.AgentsWriteMemoryResponse, error) {
	if r.By == nil || r.By.Name != r.ID {
		return nil, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "只能修改自己的記憶"}
	}
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	a.s.rows["memory/"+r.ID] = r.Body
	return &sdk.AgentsWriteMemoryResponse{}, nil
}

func (a notesAgents) WriteNote(_ context.Context, r *sdk.AgentsWriteNoteRequest) (*sdk.AgentsWriteNoteResponse, error) {
	if r.By == nil || r.By.Name != r.ID {
		return nil, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "只能寫自己的做事筆記"}
	}
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	key := "note/" + r.ID + "/" + r.Name
	old, exists := a.s.rows[key]
	if exists && r.Revision != "rev-"+old {
		return nil, &sdk.Error{Code: sdk.ErrorCodeConflict, Message: "已經有這份筆記，要帶版本"}
	}
	a.s.rows[key] = r.Body
	return &sdk.AgentsWriteNoteResponse{Name: r.Name, Created: !exists, Revision: "rev-" + r.Body}, nil
}

func (a notesAgents) AppendJournal(_ context.Context, r *sdk.AgentsAppendJournalRequest) (*sdk.AgentsAppendJournalResponse, error) {
	if r.By == nil || r.By.Name != r.ID {
		return nil, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "只能寫自己的日誌"}
	}
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	key := "journal/" + r.ID + "/" + time.Now().Format("2006-01-02")
	a.s.rows[key] += "- 09:00 " + r.Line + "\n"
	return &sdk.AgentsAppendJournalResponse{}, nil
}

func serveNotes(t *testing.T) (*sdk.Client, *notesStore) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	s := &notesStore{rows: map[string]string{"memory/life": "主人喜歡喝無糖綠茶\n"}}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sdk.Serve(ctx, sock, map[string]any{"vault": notesVault{s: s}, "agents": notesAgents{s: s}})
	}()
	t.Cleanup(func() { cancel(); <-done; os.RemoveAll(dir) })
	waitSocket(t, sock)
	return sdk.Dial(sock), s
}

// TestMemoryAndJournalCommands：秘書用 EverGenie 寫自己的記憶與日誌（DEV-1）；身分是 $EVERGENIE_AGENT，沒有就不寫。
// Guards: REQ-251
func TestMemoryAndJournalCommands(t *testing.T) {
	c, s := serveNotes(t)
	t.Setenv("EVERGENIE_AGENT", "code")

	// 還沒有記憶：show 白話說沒有；add 從空的開始。
	if out := mustRun(t, c, "memory", "show"); !strings.Contains(out, "code 還沒有記憶") {
		t.Fatalf("show 空的：%q", out)
	}
	mustRun(t, c, "memory", "add", "office", "是開發機")
	mustRun(t, c, "memory", "add", "發版先", "mini", "後", "office")
	if got := s.rows["memory/code"]; got != "office 是開發機\n發版先 mini 後 office\n" {
		t.Fatalf("add 接在尾端：%q", got)
	}
	if out := mustRun(t, c, "memory", "show"); out != "office 是開發機\n發版先 mini 後 office\n" {
		t.Fatalf("show：%q", out)
	}
	// 看別人的可以（讀不限本人），寫只寫自己的。
	if out := mustRun(t, c, "memory", "show", "life"); !strings.Contains(out, "無糖綠茶") {
		t.Fatalf("show life：%q", out)
	}

	// write：--file、--body、標準輸入，整份取代；兩個都給或都沒給是用法錯誤。
	file := filepath.Join(t.TempDir(), "mem.md")
	if err := os.WriteFile(file, []byte("# 記憶\n- 一\n- 二\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := mustRun(t, c, "memory", "write", "--file", file); !strings.Contains(out, "3 行") {
		t.Fatalf("write --file：%q", out)
	}
	if s.rows["memory/code"] != "# 記憶\n- 一\n- 二\n" {
		t.Fatalf("write 整份取代：%q", s.rows["memory/code"])
	}
	if code, _, _ := run(t, c, "從標準輸入\n", "memory", "write", "--file", "-"); code != 0 || s.rows["memory/code"] != "從標準輸入\n" {
		t.Fatalf("write --file -：%d %q", code, s.rows["memory/code"])
	}
	for _, args := range [][]string{{"memory", "write"}, {"memory", "write", "--file", file, "--body", "x"}, {"memory", "write", "--body", "  "}, {"memory", "add"}, {"journal", "add"}} {
		if code, _, _ := run(t, c, "", args...); code != 2 {
			t.Fatalf("%v 要是用法錯誤：%d", args, code)
		}
	}

	// journal：add 寫今天、show 讀今天或 --date 那天。
	mustRun(t, c, "journal", "add", "修好", "DEV-1")
	if out := mustRun(t, c, "journal", "show"); !strings.Contains(out, "修好 DEV-1") {
		t.Fatalf("journal show：%q", out)
	}
	if out := mustRun(t, c, "journal", "show", "code", "--date", "2020-01-01"); !strings.Contains(out, "2020-01-01 沒有日誌") {
		t.Fatalf("journal show --date：%q", out)
	}
	if code, _, _ := run(t, c, "", "journal", "show", "--date", "昨天"); code != 2 {
		t.Fatal("--date 格式不對要是用法錯誤")
	}

	// 沒有秘書身分（主人或一般 session）：寫入是用法錯誤，什麼都沒送；看要指定是誰。
	t.Setenv("EVERGENIE_AGENT", "")
	before := s.rows["memory/code"]
	for _, args := range [][]string{{"memory", "add", "x"}, {"memory", "write", "--body", "x"}, {"journal", "add", "x"}, {"memory", "show"}} {
		code, _, errOut := run(t, c, "", args...)
		if code != 2 || !strings.Contains(errOut, "EVERGENIE_AGENT") && !strings.Contains(errOut, "哪一位") {
			t.Fatalf("%v 沒有秘書身分：%d %q", args, code, errOut)
		}
	}
	if s.rows["memory/code"] != before {
		t.Fatal("沒有秘書身分不能改記憶")
	}
	if out := mustRun(t, c, "memory", "show", "code"); out != before {
		t.Fatalf("主人可以看：%q", out)
	}
}

// TestMemoryNoteCommands：秘書的做事筆記——list／show 讀、write 只寫自己的；新檔不用版本、改舊檔要版本；內文不要自帶檔頭。
func TestMemoryNoteCommands(t *testing.T) {
	c, s := serveNotes(t)
	t.Setenv("EVERGENIE_AGENT", "life")
	if out := mustRun(t, c, "memory", "note", "list"); !strings.Contains(out, "life 還沒有做事筆記") {
		t.Fatalf("list 空的：%q", out)
	}
	code, out, _ := run(t, c, "第一週\n", "memory", "note", "write", "訓練計畫", "--file", "-")
	if code != 0 || !strings.Contains(out, "已新增") || s.rows["note/life/訓練計畫"] != "第一週\n" {
		t.Fatalf("write 新檔（標準輸入）：%d %q %q", code, out, s.rows["note/life/訓練計畫"])
	}
	// 已經有：沒帶版本拒絕；帶 show 看到的版本可以。
	if code, _, errOut := run(t, c, "第二週\n", "memory", "note", "write", "訓練計畫", "--file", "-"); code == 0 || !strings.Contains(errOut, "版本") {
		t.Fatalf("沒帶版本要拒絕：%d %q", code, errOut)
	}
	show := mustRun(t, c, "memory", "note", "show", "訓練計畫")
	if !strings.Contains(show, "版本：rev-第一週") || !strings.Contains(show, "第一週") {
		t.Fatalf("show：%q", show)
	}
	if code, out, _ := run(t, c, "第二週\n", "memory", "note", "write", "訓練計畫", "--file", "-", "--revision", "rev-第一週\n"); code != 0 || !strings.Contains(out, "已更新") {
		t.Fatalf("帶版本改舊檔：%d %q", code, out)
	}
	if out := mustRun(t, c, "memory", "note", "list"); !strings.Contains(out, "- 訓練計畫 ·") {
		t.Fatalf("list：%q", out)
	}
	// 看別人的可以（--agent），寫別人的不行（沒有參數可以指定寫誰）；內文自帶檔頭、缺名字或缺 --file 是用法錯誤。
	s.rows["note/code/部署清單"] = "步驟\n"
	if out := mustRun(t, c, "memory", "note", "show", "部署清單", "--agent", "code"); !strings.Contains(out, "步驟") {
		t.Fatalf("show --agent：%q", out)
	}
	if code, _, errOut := run(t, c, "", "memory", "note", "show", "不存在"); code == 0 || !strings.Contains(errOut, "沒有做事筆記") {
		t.Fatalf("show 不存在：%d %q", code, errOut)
	}
	for _, args := range [][]string{{"memory", "note"}, {"memory", "note", "write"}, {"memory", "note", "write", "x"}, {"memory", "note", "write", "x", "--file", "-", "y"}, {"memory", "note", "foo"}} {
		if code, _, _ := run(t, c, "內文\n", args...); code != 2 {
			t.Fatalf("%v 要是用法錯誤：%d", args, code)
		}
	}
	if code, _, errOut := run(t, c, "---\nby: x\n---\n內文\n", "memory", "note", "write", "自帶檔頭", "--file", "-"); code != 2 || !strings.Contains(errOut, "檔頭") {
		t.Fatalf("內文自帶檔頭要拒絕：%d %q", code, errOut)
	}
	if _, ok := s.rows["note/life/自帶檔頭"]; ok {
		t.Fatal("被拒的不能寫進去")
	}
	// 沒有秘書身分：不寫、看要指定是誰。
	t.Setenv("EVERGENIE_AGENT", "")
	for _, args := range [][]string{{"memory", "note", "write", "x", "--file", "-"}, {"memory", "note", "list"}} {
		if code, _, errOut := run(t, c, "x\n", args...); code != 2 || !strings.Contains(errOut, "EVERGENIE_AGENT") && !strings.Contains(errOut, "哪一位") {
			t.Fatalf("%v 沒有秘書身分：%d %q", args, code, errOut)
		}
	}
	if out := mustRun(t, c, "memory", "note", "list", "--agent", "code"); !strings.Contains(out, "部署清單") {
		t.Fatalf("主人看別人的：%q", out)
	}
}
