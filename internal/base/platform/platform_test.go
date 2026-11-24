package platform

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
	"github.com/oklog/ulid/v2"
)

const homeConfig = `[user]
name = "測試使用者"
timezone = "Asia/Taipei"
[slots]
default = "life"
[slots.life]
label = "日常"
remind = "08:30"
check = "18:00"
[accounts]
default = "main"
[accounts.main]
engine = "test-engine"
[web]
port = 8850
`

func configDir(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "home.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func fixedTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Guards: REQ-119
func TestLoadHomeDefaults(t *testing.T) {
	h, err := LoadHome(configDir(t, homeConfig))
	if err != nil {
		t.Fatal(err)
	}
	if h.Schema != 1 || h.Mode != "simple" || h.User.Language != "zh-TW" || h.User.QuietHours != "22:00-07:01" || h.User.QuietDeferTo != "08:30" || h.Sense.IntervalMin != 30 || h.Sense.MaxLines != 40 || h.Upgrade.Channel != "stable" || h.Location().String() != "Asia/Taipei" {
		t.Fatalf("wrong defaults: mode %s", h.Mode)
	}
	account := h.Accounts["main"].(map[string]any)
	if account["remote_access"] != false || account["usage_warmup"] != false || account["engine_config"] == nil {
		t.Fatal("account defaults missing")
	}
}

// Guards: REQ-119
func TestLoadHomeRejectsBadField(t *testing.T) {
	for _, tc := range []struct{ config, field string }{
		{"mode='bad'\n" + homeConfig, "mode"},
		{strings.Replace(homeConfig, "Asia/Taipei", "invalid-zone", 1), "user.timezone"},
		{strings.Replace(homeConfig, "08:30", "28:00", 1), "slots.life.remind"},
		{strings.Replace(homeConfig, "8850", "70000", 1), "web.port"},
		{strings.Replace(homeConfig, "name = \"測試使用者\"", "name = \"\"", 1), "user.name"},
	} {
		_, err := LoadHome(configDir(t, tc.config))
		var e *Err
		if !errors.As(err, &e) || e.Code != sdk.ErrorCodeInvalid || e.Field != tc.field {
			t.Fatalf("want invalid %s, got %v", tc.field, err)
		}
	}
	_, err := LoadHome(t.TempDir())
	if CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
}

// Guards: REQ-119
func TestTrustConfiguration(t *testing.T) {
	dir := configDir(t, homeConfig)
	if err := os.Mkdir(filepath.Join(dir, "settings"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings", "trust.toml")
	good := "schema=1\n[[repo]]\npath='/fixture/repo'\npush=false\nwriters=['code']\n[approve]\nactions=['read']\n"
	if err := os.WriteFile(path, []byte(good), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Trust.Repos) != 1 || *h.Trust.Repos[0].Push || h.Trust.Approve == nil {
		t.Fatal("trust not retained")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(good, "push=false\n", "", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadHome(dir)
	var e *Err
	if !errors.As(err, &e) || e.Field != "repo.push" {
		t.Fatal("missing push accepted")
	}
}

// Guards: REQ-3
func TestAtomicWriteCrash(t *testing.T) {
	if sdk.Getenv("EVERGENIE_ATOMIC_CHILD") == "1" {
		err := atomicWrite(sdk.Getenv("EVERGENIE_ATOMIC_PATH"), []byte("new complete"), 0600, func() {
			if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
				os.Exit(2)
			}
			var b [1]byte
			if _, err := os.Stdin.Read(b[:]); err != nil {
				os.Exit(3)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "record.md")
	if err := AtomicWrite(path, []byte("old complete"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAtomicWriteCrash$")
	cmd.Env = append(os.Environ(), "EVERGENIE_ATOMIC_CHILD=1", "EVERGENIE_ATOMIC_PATH="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	}()
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatal("child did not reach pre-rename point")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("child unexpectedly succeeded")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old complete" {
		t.Fatalf("partial write: %q", got)
	}
	if err := AtomicWrite(path, []byte("new complete"), 0640); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "new complete" {
		t.Fatalf("replacement failed: %v", err)
	}
}

// Guards: REQ-32, REQ-62
func TestQuietCrossMidnight(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	q := Quiet{Start: "22:00", End: "07:01", DeferTo: "08:30", Loc: loc}
	for _, tc := range []struct {
		s    string
		want bool
	}{{"2026-10-23T21:59:59+08:00", false}, {"2026-10-23T22:00:00+08:00", true}, {"2026-10-24T07:00:59+08:00", true}, {"2026-10-24T07:01:00+08:00", false}} {
		if got := q.Contains(fixedTime(t, tc.s)); got != tc.want {
			t.Errorf("%s: got %v", tc.s, got)
		}
	}
}

// Guards: REQ-32, REQ-62
func TestQuietDeferDefaultSlots(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	q := Quiet{Start: "22:00", End: "07:01", DeferTo: "08:30", Loc: loc}
	for _, tc := range []struct{ in, want string }{{"2026-10-23T23:00:00+08:00", "2026-10-24T08:30:00+08:00"}, {"2026-10-24T06:00:00+08:00", "2026-10-24T08:30:00+08:00"}, {"2026-10-24T09:00:00+08:00", "2026-10-24T09:00:00+08:00"}} {
		if got := q.Defer(fixedTime(t, tc.in)); !got.Equal(fixedTime(t, tc.want)) {
			t.Errorf("%s -> %s", tc.in, got)
		}
	}
}

// Guards: REQ-119
func TestFakeClockAdvance(t *testing.T) {
	initial := fixedTime(t, "2026-10-23T08:00:00+08:00")
	clock := NewFakeClock(initial)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); clock.Advance(time.Minute); clock.Now() }()
	}
	wg.Wait()
	if !clock.Now().Equal(initial.Add(20 * time.Minute)) {
		t.Fatal("concurrent increments lost")
	}
	clock.Set(initial)
	if !clock.Now().Equal(initial) {
		t.Fatal("Set failed")
	}
}

// Guards: REQ-120
func TestSecretsNeverLogged(t *testing.T) {
	sentinel := "synthetic-private-test-value"
	t.Setenv("EVERGENIE_TEST_PRIVATE", sentinel)
	dir := configDir(t, homeConfig+"\n[secrets]\nfile='private-data'\n")
	if err := os.WriteFile(filepath.Join(dir, "private-data"), []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h.SecretsPath() != filepath.Join(dir, "private-data") {
		t.Fatal("wrong path")
	}
	h.Clock = NewFakeClock(fixedTime(t, "2026-10-23T23:59:00+08:00"))
	log := NewLogger(h, "platform")
	log.Info("loaded", "home", h)
	h.Clock.(*FakeClock).Advance(2 * time.Minute)
	log.Info("next day")
	for _, day := range []string{"2026-10-23", "2026-10-24"} {
		data, err := os.ReadFile(filepath.Join(dir, "runtime", "logs", day+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(sentinel)) || bytes.Contains(data, []byte("private-data")) || !bytes.Contains(data, []byte(`"module":"platform"`)) {
			t.Fatal("unsafe or incomplete log")
		}
	}
}

// Guards: REQ-119
func TestErrorCodesAndIDs(t *testing.T) {
	for _, err := range []error{Invalid("field", "不合格"), NotFound("找不到"), Conflict("衝突"), Forbidden("不允許"), Unavailable("暫時無法使用"), Internal("發生錯誤")} {
		if CodeOf(fmt.Errorf("wrapped: %w", err)) != CodeOf(err) {
			t.Fatal("wrapped code lost")
		}
	}
	if CodeOf(nil) != "" || CodeOf(errors.New("unknown")) != sdk.ErrorCodeInternal {
		t.Fatal("wrong fallback")
	}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID()
		if _, err := ulid.ParseStrict(id); err != nil || seen[id] {
			t.Fatal("invalid or duplicate id")
		}
		seen[id] = true
	}
}
