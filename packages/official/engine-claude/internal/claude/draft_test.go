package claude

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 真實畫面（2026-11-10 在空環境錄的，v2.1.322）：先回一句、工具還在跑。
const draftScreenRunning = ` ▐▛███▛█   Claude Code v2.1.322
 ▝▝   ▝▝   /tmp/wfmt

❯ 先用兩三句中文說你要去做什麼（句子長一點，要超過一行），然後用 Bash 執行 echo 加上一段 600
  字的中文段落

⏺ 我要先簡單說明接下來的動作：我會用 Bash 執行一個 echo
  指令，把一段大約六百字的中文段落直接印出來，讓你確認輸出內容是否完整、換行與排版是否正常。這段文字
  只是純粹的示範內容，不會建立任何檔案，也不會修改你電腦上的任何設定或資料。

  Running 1 shell command…

✽ Tinkering… (5s · ↓ 20 tokens)
  tmux focus-events off · add 'set -g focus-events on' to ~/.tmux.conf and reattach for focus tra…

────────────────────────────────────────────────────────────────────────────────────────────────────
❯
────────────────────────────────────────────────────────────────────────────────────────────────────
  [Sonnet 5.5] | ctx 5%
  ⏸ manual mode on
`

// 做完了：最後的總結有分段與清單。
const draftScreenDone = `❯ 先用兩句中文說你要做什麼，然後用 Bash 執行 sleep 6
  字中文，最後用三四句中文總結

⏺ 我先說明一下：接下來會先執行 sleep 6 等待六秒，再用 echo 輸出一段約 300
  字的中文。最後我會整理成簡短的總結給你。

  Ran 2 shell commands

⏺ 兩個指令都執行完了，sleep 6 等了六秒，echo 也輸出了那段中文。

  - 第一步： 等待六秒，沒有輸出。
  - 第二步： 印出一段約 300 字、描寫清晨城市生活的短文。

  整體來說，兩個步驟都成功完成，沒有出現錯誤。

✻ Crunched for 17s   done 12:33 PM

────────────────────────────────────────────────────────────────────────────────────────────────────
❯
────────────────────────────────────────────────────────────────────────────────────────────────────
`

func TestDraftFromScreen(t *testing.T) {
	cases := []struct{ name, screen, want string }{
		{"先回一句、工具還在跑", draftScreenRunning, "我要先簡單說明接下來的動作：我會用 Bash 執行一個 echo 指令，把一段大約六百字的中文段落直接印出來，讓你確認輸出內容是否完整、換行與排版是否正常。這段文字只是純粹的示範內容，不會建立任何檔案，也不會修改你電腦上的任何設定或資料。"},
		{"做完了（沒有轉圈）不送", draftScreenDone, ""},
		{"還在打最後一段（分段、清單各自一行）", strings.Replace(draftScreenDone, "✻ Crunched for 17s   done 12:33 PM", "✻ Crunching… (17s · ↓ 90 tokens)", 1), "兩個指令都執行完了，sleep 6 等了六秒，echo 也輸出了那段中文。\n\n- 第一步： 等待六秒，沒有輸出。\n- 第二步： 印出一段約 300 字、描寫清晨城市生活的短文。\n\n整體來說，兩個步驟都成功完成，沒有出現錯誤。"},
		{"主人剛送出、秘書還沒開口", "⏺ 上一輪的話\n\n❯ 新的一句\n\n✽ Thinking…\n\n────\n❯ \n────\n", ""},
		{"舊版畫面的工具呼叫不算話", "❯ 問\n\n⏺ Bash(echo hi)\n  ⎿  hi\n\n✽ Running…\n────\n❯ \n────\n", ""},
		{"工具結果後面接著的話", "❯ 問\n\n⏺ 好，我查一下。\n\n⏺ Bash(ls)\n  ⎿  a.txt\n\n⏺ 查好了，有 a.txt。\n✽ Thinking…\n────\n❯ \n────\n", "查好了，有 a.txt。"},
		{"英文折行補空白", "❯ q\n\n⏺ I will run the\n  command now.\n✽ Thinking…\n────\n❯ \n────\n", "I will run the command now."},
		{"新版畫面：工具狀態跟工具標題都不算話", "❯ 問\n\n⏺ 好，我來寫進日誌。\n⏺ Running 1 shell command…\n✽ Ideating…\n────\n❯ \n────\n", "好，我來寫進日誌。"},
		{"新版畫面：工具標題下面接結果", "❯ 問\n\n⏺ 好，我來寫進日誌。\n⏺ Adding a plan entry to the journal\n  ⎿  $ evergenie journal add x\n✽ Ideating…\n────\n❯ \n────\n", "好，我來寫進日誌。"},
		{"新版畫面：工具標題折兩行再接結果", "❯ 問\n\n⏺ 好，我來寫進日誌。\n⏺ Adding a very long plan entry to the journal so that it\n  wraps\n  ⎿  $ evergenie journal add x\n✽ Ideating…\n────\n❯ \n────\n", "好，我來寫進日誌。"},
		{"英文的話用 Read 開頭還是話", "❯ q\n\n⏺ Read this first: I will check.\n  Ran 1 shell command\n✽ Ideating…\n────\n❯ \n────\n", "Read this first: I will check."},
		{"轉圈的圓點閃掉只剩縮排也算在忙", "❯ 問\n\n⏺ 好。\n  Running 1 shell command…\n  Ideating… (3s)\n────\n❯ \n────\n", "好。"},
		{"被中斷（沒有轉圈）不送", "❯ 問\n\n⏺ 好，我來\n  ⎿  Interrupted · What should Claude do instead?\n────\n❯ \n────\n", ""},
		{"話後面的 summary 標記與靠右的狀態字不算（WEB-171）", "❯ 問\n\n⏺ 我理解新流程。接下來我先查 ctl。 · summary\n\n  Ran 1 shell command\n\n⏺ ctl 的 SSH 目前是直接用 root 連線，改動會集中在\n  internal/sshx。範圍需要先跟你確認。 · summary                                 No changes this session\n                                                                                         +50 files edited before this session (show)\n✽ Ideating…\n────\n❯ \n────\n", "ctl 的 SSH 目前是直接用 root 連線，改動會集中在 internal/sshx。範圍需要先跟你確認。"},
		{"縮排的清單續行不被當成靠右狀態字", "❯ 問\n\n⏺ 兩步：\n  1. 先登入\n     再下指令\n✽ Ideating…\n────\n❯ \n────\n", "兩步：\n1. 先登入再下指令"},
		{"中文字後面直接接靠右狀態字", "❯ 問\n\n⏺ 範圍需要先跟你確認。                                 No changes this session\n✽ Ideating…\n────\n❯ \n────\n", "範圍需要先跟你確認。"},
		{"表格與對齊的程式碼不切", "❯ 問\n\n⏺ 表格：\n  │ 名稱   │ 說明     │\n  x := 1   // 註解\n✽ Ideating…\n────\n❯ \n────\n", "表格：│ 名稱   │ 說明     │ x := 1   // 註解"},
		{"輸入框裡打到一半的字不算", "❯ 問\n\n⏺ 好。\n✽ Thinking…\n────\n❯ 主人打到一半\n────\n", "好。"},
	}
	for _, c := range cases {
		if got := draftFromScreen(c.screen); got != c.want {
			t.Errorf("%s：\n要 %q\n得 %q", c.name, c.want, got)
		}
	}
}

func TestDraftCovered(t *testing.T) {
	said := []string{draftKey("我要先**簡單**說明接下來的動作：我會用 `Bash` 執行")}
	if !draftCovered("我要先簡單說明接下來的動作：我會用 Bash 執行", said) {
		t.Fatal("畫面拿掉 markdown 符號、重新折行，還是同一段")
	}
	if draftCovered("另一段話", said) {
		t.Fatal("記錄裡沒有的話要送")
	}
	// WEB-171：畫面上那句帶了 summary 標記和靠右狀態字，濾掉後要對得上記錄。
	screen := "❯ 問\n\n⏺ 範圍需要先跟你確認。 · summary                    No changes this session\n✽ Ideating…\n────\n❯ \n────\n"
	if d := draftFromScreen(screen); !draftCovered(d, []string{draftKey("範圍需要先跟你**確認**。")}) {
		t.Fatalf("濾掉雜訊後要對得上記錄：%q", d)
	}
	if !draftCovered("  ", nil) {
		t.Fatal("空的不送")
	}
}

// 跟隨時：畫面上有、記錄還沒有的那段先用 draft 送；記錄寫進來之後不再送、也不重複送同一段。
func TestTranscriptFollowSendsScreenDraft(t *testing.T) {
	v := newEngEnv(t)
	v.start("code", nil)
	id := "00000000-0000-4000-8000-000000000001"
	dir := filepath.Join(v.cfgDir, "projects", projectDirName("/tmp/hg-work"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	user := `{"type":"user","uuid":"u1","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"早餐吃了豆漿"}}` + "\n"
	reply := `{"type":"assistant","uuid":"a1","timestamp":"2026-10-24T10:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"收到，我先記下來。"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	screen := "❯ 早餐吃了豆漿\n\n⏺ 收到，我先記\n✽ Thinking…\n────\n❯ \n────\n"
	setScreen := func(s string) {
		v.tmux.mu.Lock()
		v.tmux.screens["code"] = s
		v.tmux.mu.Unlock()
	}
	setScreen(screen)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	step := 0
	v.e.d.Sleeper = func(time.Duration) {
		step++
		switch step {
		case 1:
			setScreen("❯ 早餐吃了豆漿\n\n⏺ 收到，我先記下來。\n\n  Running 1 shell command…\n✽ Thinking…\n────\n❯ \n────\n")
		case 2: // 同一段，不重送
		case 3:
			f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
			f.WriteString(reply)
			f.Close()
		case 4: // 記錄有了，畫面還是那段：不再送
		default:
			cancel()
		}
	}
	var got []string
	err := v.e.Transcript(ctx, &sdk.EngineTranscriptRequest{Session: "code", Follow: true}, func(r *sdk.EngineTranscriptResponse) error {
		m := r.Message
		if m.Draft {
			got = append(got, "draft:"+m.Text)
		} else {
			got = append(got, m.Uuid+":"+m.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"u1:早餐吃了豆漿", "draft:收到，我先記", "draft:收到，我先記下來。", "a1:收到，我先記下來。"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("要 %q\n得 %q", want, got)
	}
	// 不跟隨（一次讀完、未讀計算）不看畫面。
	got = nil
	if err := v.e.Transcript(context.Background(), &sdk.EngineTranscriptRequest{Session: "code"}, func(r *sdk.EngineTranscriptResponse) error {
		if r.Message.Draft {
			got = append(got, r.Message.Text)
		}
		return nil
	}); err != nil || len(got) != 0 {
		t.Fatalf("不跟隨不送 draft：%v %v", got, err)
	}
}

// 重連（帶 SinceUuid）時游標之前、同一輪已經寫進記錄的話不算還在打；秘書做完後淡色那段留一下再收掉。
func TestTranscriptFollowReconnectAndIdleClear(t *testing.T) {
	v := newEngEnv(t)
	v.start("code", nil)
	id := "00000000-0000-4000-8000-000000000001"
	dir := filepath.Join(v.cfgDir, "projects", projectDirName("/tmp/hg-work"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	lines := `{"type":"user","uuid":"u1","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"早餐吃了豆漿"}}` + "\n" +
		`{"type":"assistant","uuid":"a1","timestamp":"2026-10-24T10:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"收到，我先記下來。"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	setScreen := func(s string) {
		v.tmux.mu.Lock()
		v.tmux.screens["code"] = s
		v.tmux.mu.Unlock()
	}
	// 一開始畫面上最後一段就是記錄裡游標那則（還在忙）：不送。
	setScreen("❯ 早餐吃了豆漿\n\n⏺ 收到，我先記下來。\n✽ Ideating…\n────\n❯ \n────\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	step := 0
	v.e.d.Sleeper = func(d time.Duration) {
		v.clock.Sleep(d)
		step++
		switch step {
		case 1:
			setScreen("❯ 早餐吃了豆漿\n\n⏺ 收到，我先記下來。\n\n⏺ 另一段還在打\n✽ Ideating…\n────\n❯ \n────\n")
		case 2: // 做完：轉圈沒了，正式那則還沒進記錄
			setScreen("❯ 早餐吃了豆漿\n\n⏺ 收到，我先記下來。\n\n⏺ 另一段還在打\n✻ Worked for 5s\n────\n❯ \n────\n")
		case 20:
			cancel()
		}
	}
	var got []string
	err := v.e.Transcript(ctx, &sdk.EngineTranscriptRequest{Session: "code", Follow: true, SinceUuid: "a1"}, func(r *sdk.EngineTranscriptResponse) error {
		if r.Message.Draft {
			got = append(got, "draft:"+r.Message.Text)
		} else {
			got = append(got, r.Message.Uuid)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"draft:另一段還在打", "draft:"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("要 %q\n得 %q", want, got)
	}
}
