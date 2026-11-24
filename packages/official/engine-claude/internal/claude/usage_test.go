package claude

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

func writeSnapshot(t *testing.T, dir, file, configDir string, five, week float64, fiveReset, weekReset time.Time) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{
		"updatedAt": "2026-10-25T07:00:00.000Z",
		"profile":   map[string]any{"configDir": configDir},
		"rateLimits": map[string]any{
			"fiveHour": map[string]any{"usedPercentage": five, "resetsAt": float64(fiveReset.Unix())},
			"sevenDay": map[string]any{"usedPercentage": week, "resetsAt": float64(weekReset.Unix())},
		},
	})
	if err := os.WriteFile(filepath.Join(dir, file), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestUsageFromSnapshot：用量來自帳號狀態列的快照；五小時過期當 0（要等下一則訊息才開新的），每週過期當 0 並把下一次重置往後推整數週；
// 快照用 profile.configDir 對到帳號，沒有快照不是錯誤。
// Guards: REQ-86
func TestUsageFromSnapshot(t *testing.T) {
	v := newEngEnv(t)
	now := v.clock.Now()
	snaps := t.TempDir()
	cfg := map[string]any{"config_dir": v.cfgDir, "monitor_dir": snaps}
	ctx := context.Background()

	r, err := v.e.Usage(ctx, &sdk.EngineUsageRequest{Account: "acct-a", EngineConfig: cfg})
	if err != nil || len(r.Windows) != 0 || !r.MeasuredAt.IsZero() {
		t.Fatalf("沒有快照：%+v %v", r, err)
	}

	// 快照檔名跟帳號名不同，靠 profile.configDir 對上。
	writeSnapshot(t, snaps, "something.json", v.cfgDir, 42, 10, now.Add(2*time.Hour), now.Add(3*24*time.Hour))
	if err := os.WriteFile(filepath.Join(snaps, "broken.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err = v.e.Usage(ctx, &sdk.EngineUsageRequest{Account: "acct-a", EngineConfig: cfg})
	if err != nil || len(r.Windows) != 2 || r.UsedRatio != 0.42 || r.MeasuredAt.IsZero() {
		t.Fatalf("有快照：%+v %v", r, err)
	}
	if w := r.Windows[0]; w.Kind != sdk.UsageWindowKindShort || w.Hours != 5 || w.Expired {
		t.Fatalf("五小時視窗：%+v", w)
	}

	// 五小時過期、每週過了 10 天：兩個都換算成 0。
	writeSnapshot(t, snaps, "something.json", v.cfgDir, 99, 80, now.Add(-time.Hour), now.Add(-10*24*time.Hour))
	r, err = v.e.Usage(ctx, &sdk.EngineUsageRequest{Account: "acct-a", EngineConfig: cfg})
	if err != nil || r.UsedRatio != 0 {
		t.Fatalf("過期後用量當 0：%+v %v", r, err)
	}
	short, weekly := r.Windows[0], r.Windows[1]
	if !short.Expired || short.UsedRatio != 0 {
		t.Fatalf("五小時過期：%+v", short)
	}
	wantNext := now.Add(-10 * 24 * time.Hour).Add(14 * 24 * time.Hour)
	if !weekly.Rolled || weekly.UsedRatio != 0 || !weekly.ResetsAt.Equal(wantNext.Truncate(time.Second)) {
		t.Fatalf("每週重置要往後推整數週：%+v 想要 %v", weekly, wantNext)
	}

	// 沒給 config_dir 的帳號：回清楚的錯誤。
	if _, err := v.e.Usage(ctx, &sdk.EngineUsageRequest{Account: "x", EngineConfig: map[string]any{}}); err == nil {
		t.Fatal("缺 config_dir 要回錯誤")
	}
}

// TestUsageWarmup：暖機用這個帳號送一則極小的請求（不留對話、不給工具）；失敗回清楚的錯誤。
// Guards: REQ-86
func TestUsageWarmup(t *testing.T) {
	v := newEngEnv(t)
	var gotArgs []string
	var gotEnv []string
	v.e.d.Run = func(_ context.Context, env []string, name string, args ...string) ([]byte, error) {
		gotArgs, gotEnv = append([]string{name}, args...), env
		return []byte("pong"), nil
	}
	cfg := map[string]any{"config_dir": v.cfgDir, "monitor_dir": t.TempDir()}
	r, err := v.e.Usage(context.Background(), &sdk.EngineUsageRequest{Account: "acct-a", EngineConfig: cfg, Warmup: true})
	if err != nil || !r.Warmed {
		t.Fatalf("暖機：%+v %v", r, err)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"claude -p ping", "--model haiku", "--no-session-persistence", "--disallowedTools *"} {
		if !strings.Contains(joined, want) {
			t.Errorf("暖機指令少了 %q：%s", want, joined)
		}
	}
	found := false
	for _, e := range gotEnv {
		found = found || e == "CLAUDE_CONFIG_DIR="+v.cfgDir
	}
	if !found {
		t.Errorf("暖機要用這個帳號的資料夾：%v", gotEnv)
	}
	v.e.d.Run = func(context.Context, []string, string, ...string) ([]byte, error) { return nil, errors.New("exit 1") }
	if _, err := v.e.Usage(context.Background(), &sdk.EngineUsageRequest{Account: "acct-a", EngineConfig: cfg, Warmup: true}); err == nil {
		t.Fatal("暖機失敗要回錯誤")
	}
}

// TestEnginePreflightChecks：只檢查不安裝；引擎旁邊與 PATH 都沒有 tmux 才算缺（ECL-63），修法分下載版（升級時補上）與原始碼版（brew install），有 Homebrew 仍給 brew 的安裝建議；
// 沒登入給白話修法。
// Guards: REQ-165
func TestEnginePreflightChecks(t *testing.T) {
	v := newEngEnv(t)
	v.e.d.LookPath = func(name string) (string, error) {
		if name == "tmux" {
			return "", errors.New("not found")
		}
		return "/usr/local/bin/" + name, nil
	}
	req := &sdk.EnginePreflightRequest{Account: "acct-a", EngineConfig: map[string]any{"config_dir": v.cfgDir}}
	r, err := v.e.Preflight(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]sdk.PreflightCheck{}
	for _, c := range r.Checks {
		by[c.ID] = c
	}
	if !by["claude"].Ok || by["tmux"].Ok || strings.Join(by["tmux"].Install, " ") != "brew install tmux" || by["login:acct-a"].Ok || by["login:acct-a"].Fix == "" {
		t.Fatalf("檢查結果：%+v", r.Checks)
	}
	if fix := by["tmux"].Fix; !strings.Contains(fix, "evergenie upgrade --apply") || !strings.Contains(fix, "brew install tmux") || strings.Contains(fix, "引擎") {
		t.Fatalf("缺 tmux 的修法要分兩種裝法說、不用「引擎」這種內部用語：%q", fix)
	}
	if err := os.WriteFile(filepath.Join(v.cfgDir, ".claude.json"), []byte(`{"oauthAccount":{"emailAddress":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, _ = v.e.Preflight(context.Background(), req)
	for _, c := range r.Checks {
		if c.ID == "login:acct-a" && !c.Ok {
			t.Fatalf("有登入資訊要通過：%+v", c)
		}
	}
	r, err = v.e.Preflight(context.Background(), &sdk.EnginePreflightRequest{Account: "bad"})
	if err != nil || len(r.Checks) < 3 {
		t.Fatalf("缺 config_dir 也要回報，不是整個失敗：%+v %v", r, err)
	}
}

// 快照檔名跟帳號名一樣、但寫的是別的帳號資料夾：是別的帳號（例如同一台別的家剛好也叫 personal），不拿來用（SEC-373）。
func TestUsageSnapshotNameMatchIgnoredWhenConfigDirDiffers(t *testing.T) {
	v := newEngEnv(t)
	now := v.clock.Now()
	snaps := t.TempDir()
	cfg := map[string]any{"config_dir": v.cfgDir, "monitor_dir": snaps}
	writeSnapshot(t, snaps, "acct-a.json", "/Users/someone/.claude-other", 77, 10, now.Add(2*time.Hour), now.Add(3*24*time.Hour))
	r, err := v.e.Usage(context.Background(), &sdk.EngineUsageRequest{Account: "acct-a", EngineConfig: cfg})
	if err != nil || len(r.Windows) != 0 {
		t.Fatalf("別的帳號資料夾的快照不能算這個帳號的：%+v %v", r, err)
	}
	// 沒寫 configDir 的舊快照照舊靠檔名對上。
	writeSnapshot(t, snaps, "acct-a.json", "", 33, 10, now.Add(2*time.Hour), now.Add(3*24*time.Hour))
	r, err = v.e.Usage(context.Background(), &sdk.EngineUsageRequest{Account: "acct-a", EngineConfig: cfg})
	if err != nil || r.UsedRatio != 0.33 {
		t.Fatalf("沒寫資料夾的快照靠檔名：%+v %v", r, err)
	}
}
