package notes

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/agent/personafixture"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// 測試自己隔離環境：把 EVERGENIE_* 清掉，個別測試要什麼自己 t.Setenv。
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if sdk.IsProductEnv(name) {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

// readVault 只開 vault 的讀（list）給指令用，接到真的資料庫。
type readVault struct {
	sdk.VaultAPI
	v *vault.Module
}

func (r readVault) List(ctx context.Context, req *sdk.VaultListRequest) (*sdk.VaultListResponse, error) {
	rows, err := r.v.List(ctx, req.Kind, vault.Query{})
	if err != nil {
		return nil, err
	}
	return &sdk.VaultListResponse{Records: rows}, nil
}

// serve 開一個真的家：底層 vault＋agent（主題筆記的規則都在那裡），notes 服務是這個模組的。
func serve(t *testing.T) (*sdk.Client, *homefixture.Home) {
	t.Helper()
	h := homefixture.NewShort(t)
	a := agent.New(agent.Deps{Vault: h.Vault, Clock: h.Clock, Bus: nopBus{}})
	a.SetHome(h.Dir)
	a.SetPersonaSource(personafixture.New(h.Dir))
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(context.Background()) })
	h.WithRecords(sdk.Record{Kind: "agent", ID: "life", Fields: map[string]any{"duty": "生活", "persona": "athena"}})
	sock := filepath.Join(h.Dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sdk.Serve(ctx, sock, map[string]any{"vault": readVault{v: h.Vault}, "notes": &service{m: New(a)}})
	}()
	t.Cleanup(func() { cancel(); <-done })
	for i := 0; i < 500; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			return sdk.Dial(sock), h
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket never accepted a connection")
	return nil, nil
}

type nopBus struct{}

func (nopBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return nil, nil
}
func (nopBus) Publish(context.Context, string, map[string]any) error { return nil }

func run(t *testing.T, c *sdk.Client, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut}, Commands(), "用法\n")
	return code, out.String(), errOut.String()
}

func mustRun(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errOut := run(t, c, "", args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errOut)
	}
	return out
}

// evergenie notes：秘書開主題、加一行（自動標名）、看、找、整份取代（沒標名拒絕）、分類；索引跟著重產。
func TestNotesCommands(t *testing.T) {
	c, h := serve(t)
	t.Setenv("EVERGENIE_AGENT", "life")
	if code, _, errOut := run(t, c, "", "notes", "add", "小美", "女兒"); code == 0 || !strings.Contains(errOut, "分類") {
		t.Fatalf("新主題沒給分類要拒絕：%d %s", code, errOut)
	}
	out := mustRun(t, c, "notes", "add", "小美", "女兒，8", "歲", "--category", "人", "--summary", "主人的女兒", "--aliases", "美美")
	if !strings.Contains(out, "已開新主題") || !strings.Contains(out, "女兒，8 歲（life 10-23）") {
		t.Fatalf("add：%q", out)
	}
	mustRun(t, c, "notes", "add", "美美", "對花生過敏")
	out = mustRun(t, c, "notes", "show", "小美")
	if !strings.Contains(out, "一句說明：主人的女兒") || !strings.Contains(out, "對花生過敏（life 10-23）") {
		t.Fatalf("show：%q", out)
	}
	if out := mustRun(t, c, "notes", "find", "花生"); !strings.Contains(out, "[[小美]]") || !strings.Contains(out, "    對花生過敏（life 10-23）") {
		t.Fatalf("find：%q", out)
	}
	if out := mustRun(t, c, "notes", "list", "--follow", "life"); !strings.Contains(out, "- [[小美]]（別名：美美） — 主人的女兒 · 跟進：life · 分類：人") {
		t.Fatalf("list：%q", out)
	}
	if out := mustRun(t, c, "notes", "list", "--category", "東西"); !strings.Contains(out, "沒有符合的主題") {
		t.Fatalf("list 篩分類：%q", out)
	}
	if !strings.Contains(out, "版本：") {
		t.Fatalf("show 要印版本：%q", out)
	}
	rev := out[strings.Index(out, "版本：")+len("版本："):]
	rev = rev[:strings.IndexAny(rev, "（\n")]
	if code, _, errOut := run(t, c, "女兒（life 10-23）\n", "notes", "set", "小美", "--file", "-"); code == 0 || !strings.Contains(errOut, "notes show") {
		t.Fatalf("set 沒帶版本要拒絕：%d %s", code, errOut)
	}
	code, _, errOut := run(t, c, "女兒（life 10-23）\n新的一行\n", "notes", "set", "小美", "--file", "-", "--revision", rev)
	if code == 0 || !strings.Contains(errOut, "第 2 行") {
		t.Fatalf("set 沒標名要拒絕並列行號：%d %s", code, errOut)
	}
	if code, _, errOut := run(t, c, "女兒（life 10-23）\n", "notes", "set", "小美", "--file", "-", "--revision", "舊的"); code == 0 || !strings.Contains(errOut, "別人改過") {
		t.Fatalf("set 版本不對要拒絕：%d %s", code, errOut)
	}
	if code, _, errOut := run(t, c, "女兒，8 歲，對花生過敏（life 10-23）\n", "notes", "set", "小美", "--file", "-", "--revision", rev); code != 0 {
		t.Fatalf("set：%s", errOut)
	}
	mustRun(t, c, "notes", "add", "小美2", "女兒，8 歲，對花生過敏", "--category", "人", "--summary", "重複的")
	mustRun(t, c, "notes", "add", "小美2", "愛畫畫")
	if code, _, errOut := run(t, c, "", "notes", "archive", "小美2", "--into", "小美"); code == 0 || !strings.Contains(errOut, "愛畫畫") {
		t.Fatalf("archive 有沒併的行要拒絕並列出：%d %s", code, errOut)
	}
	mustRun(t, c, "notes", "add", "小美", "愛畫畫")
	if out := mustRun(t, c, "notes", "archive", "小美2", "--into", "小美"); !strings.Contains(out, "已封存") || !strings.Contains(out, "archive/") {
		t.Fatalf("archive：%q", out)
	}
	if out := mustRun(t, c, "notes", "list"); strings.Contains(out, "小美2") {
		t.Fatalf("封存後 list 不該看到：%q", out)
	}
	if out := mustRun(t, c, "notes", "categories", "list"); !strings.Contains(out, "- 人：") || !strings.Contains(out, "- 喜好：") ||
		!strings.Contains(out, "\n  建議段落：`## 他是誰`、`## 喜好與禁忌`、`## 最近的事`。\n") {
		t.Fatalf("預設分類：%q", out)
	}
	mustRun(t, c, "notes", "categories", "add", "寵物", "--summary", "家裡的貓狗")
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/notes/寵物/寵物.md")); err != nil {
		t.Fatal("新分類要有說明檔：", err)
	}
	index, err := os.ReadFile(filepath.Join(h.Dir, "vault/agents/life/索引.md"))
	if err != nil || !strings.Contains(string(index), "[[小美]]") {
		t.Fatalf("職位索引要跟著重產：%v\n%s", err, index)
	}
	t.Setenv("EVERGENIE_AGENT", "")
	if out := mustRun(t, c, "notes", "add", "主人", "住台北"); !strings.Contains(out, "住台北（主人 10-23）") {
		t.Fatalf("主人也能寫，標「主人」：%q", out)
	}
}

// evergenie notes quips：記梗（沒出處拒絕、第一次自己建梗本）、看開場帶的那一節、記下提過哪一條（這週先不帶）。
func TestNotesQuipCommands(t *testing.T) {
	c, _ := serve(t)
	t.Setenv("EVERGENIE_AGENT", "life")
	if out := mustRun(t, c, "notes", "quips"); !strings.Contains(out, "梗本一共 0 條") || !strings.Contains(out, "梗本還沒有梗") {
		t.Fatalf("還沒有梗：%q", out)
	}
	if code, _, errOut := run(t, c, "", "notes", "add", "梗本", "「人形鬧鐘」主人五點半自己醒"); code == 0 || !strings.Contains(errOut, "出處") {
		t.Fatalf("沒寫出處要拒絕：%d %s", code, errOut)
	}
	if out := mustRun(t, c, "notes", "add", "梗本", "「人形鬧鐘」主人五點半自己醒——出處：聊天"); !strings.Contains(out, "已開新主題並寫進「梗本」") {
		t.Fatalf("第一次記就建梗本：%q", out)
	}
	mustRun(t, c, "notes", "add", "梗本", "「泡麵大師」——出處：晚餐")
	out := mustRun(t, c, "notes", "quips")
	if !strings.Contains(out, "梗本一共 2 條，帶了 2 條") || !strings.Contains(out, "- 「人形鬧鐘」主人五點半自己醒——出處：聊天（life 10-23）") {
		t.Fatalf("notes quips：%q", out)
	}
	if out := mustRun(t, c, "notes", "quips", "used", "人形鬧鐘"); !strings.Contains(out, "以前不會再帶進開場") {
		t.Fatalf("quips used：%q", out)
	}
	out = mustRun(t, c, "notes", "quips")
	if strings.Contains(out, "- 「人形鬧鐘」") || !strings.Contains(out, "這週提過、先不帶：「人形鬧鐘」主人五點半自己醒——出處：聊天") {
		t.Fatalf("提過的這週先不帶：%q", out)
	}
	if code, _, _ := run(t, c, "", "notes", "quips", "used"); code == 0 {
		t.Fatal("quips used 沒給字要回用法")
	}
}
