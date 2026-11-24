package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 假的 EverGenie：`serve -home <家>` 把自己的參數與 EVERGENIE_* 環境印到標準輸出，然後等 SIGTERM；stubborn 時忽略 SIGTERM（要 SIGKILL 才停得掉）。
func fakeServeExe(t *testing.T, stubborn bool) string {
	t.Helper()
	trap := "trap 'exit 0' TERM"
	if stubborn {
		trap = "trap '' TERM"
	}
	script := "#!/bin/sh\necho \"args: $*\"\necho \"EVERGENIE_HOME=${EVERGENIE_HOME:-} EVERGENIE_ENGINE_BIN_DIR=${EVERGENIE_ENGINE_BIN_DIR:-}\"\n" + trap + "\nwhile :; do sleep 0.05; done\n"
	exe := filepath.Join(t.TempDir(), "evergenie")
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

// 背景行程模式（EVERGENIE_SERVICE_MODE=process）：不碰 launchd，serve 以背景行程跑，pid 寫在 runtime/serve.pid，日誌在 runtime/logs/serve.log；
// 安裝＝（重新）啟動、重開換新 pid、停止後 pid 檔消失；TERM 停不掉的用 KILL。
// Guards: REQ-280
func TestServiceProcessMode(t *testing.T) {
	t.Setenv(ServiceModeEnv, "process")
	t.Setenv("EVERGENIE_HOME", "/不該帶過去的家")
	t.Setenv("EVERGENIE_ENGINE_BIN_DIR", "")
	home := testHome(t)
	exe := fakeServeExe(t, false)
	svc := Service{Runner: &fakeRunner{}, Spec: ServiceSpec{Exe: exe, Home: home}, UID: os.Getuid()}
	ctx := context.Background()
	t.Cleanup(func() { _, _ = svc.Stop(ctx) })

	if st := svc.Status(ctx); st.Installed || st.Running {
		t.Fatalf("還沒啟動：%+v", st)
	}
	if lines, err := svc.Stop(ctx); err != nil || !strings.Contains(strings.Join(lines, ""), "本來就沒有在跑") {
		t.Fatalf("沒在跑時 stop：%v %v", lines, err)
	}
	if _, err := svc.Install(ctx); err != nil {
		t.Fatal(err)
	}
	st := svc.Status(ctx)
	if !st.Installed || !st.Loaded || !st.Running || st.PID <= 0 || !st.Process {
		t.Fatalf("安裝後：%+v", st)
	}
	pid1 := st.PID
	data, err := os.ReadFile(filepath.Join(home, "runtime", "serve.pid"))
	if err != nil || strings.TrimSpace(string(data)) == "" {
		t.Fatalf("pid 檔：%q %v", data, err)
	}
	if loaded, running, pid := svc.Loaded(ctx); !loaded || !running || pid != pid1 {
		t.Fatalf("Loaded：%v %v %d", loaded, running, pid)
	}
	var logText string
	for i := 0; i < 60; i++ {
		b, _ := os.ReadFile(filepath.Join(home, "runtime", "logs", "serve.log"))
		if logText = string(b); strings.Contains(logText, "EVERGENIE_HOME=") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(logText, "args: serve -home "+home) {
		t.Fatalf("serve.log 沒有啟動參數：%q", logText)
	}
	if strings.Contains(logText, "不該帶過去的家") || !strings.Contains(logText, "EVERGENIE_ENGINE_BIN_DIR="+filepath.Dir(exe)) {
		t.Fatalf("環境：別的家的 EVERGENIE_HOME 不能帶過去，引擎資料夾預設是執行檔旁邊：%q", logText)
	}
	if lines, err := svc.Start(ctx); err != nil || !strings.Contains(strings.Join(lines, ""), "已經在跑") || svc.Status(ctx).PID != pid1 {
		t.Fatalf("已經在跑時 start：%v %v", lines, err)
	}
	if _, err := svc.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	st = svc.Status(ctx)
	if !st.Running || st.PID == pid1 || pidAlive(pid1) {
		t.Fatalf("重開要換新的 pid、舊的要停掉：舊 %d、%+v", pid1, st)
	}
	pid2 := st.PID
	if _, err := svc.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if pidAlive(pid2) {
		t.Fatal("stop 之後行程還活著")
	}
	if _, err := os.Stat(filepath.Join(home, "runtime", "serve.pid")); err == nil {
		t.Fatal("stop 之後 pid 檔要消失")
	}
	if st := svc.Status(ctx); st.Running || st.Installed {
		t.Fatalf("停止後：%+v", st)
	}

	// 沒有 home.toml 或執行檔不存在：跟 launchd 模式一樣擋下來，不啟動。
	bad := Service{Runner: &fakeRunner{}, Spec: ServiceSpec{Exe: filepath.Join(t.TempDir(), "沒有這個檔"), Home: home}}
	if _, err := bad.Start(ctx); err == nil {
		t.Fatal("執行檔不存在該被擋")
	}
	if _, err := (Service{Runner: &fakeRunner{}, Spec: ServiceSpec{Exe: exe, Home: t.TempDir()}}).Start(ctx); err == nil {
		t.Fatal("家沒有 home.toml 該被擋")
	}
	if _, err := os.Stat(filepath.Join(home, "runtime", "serve.pid")); err == nil {
		t.Fatal("被擋的啟動不能留下 pid 檔")
	}
}

// Guards: REQ-280
func TestServiceProcessModeKillsStubborn(t *testing.T) {
	t.Setenv(ServiceModeEnv, "process")
	old := processStopWait
	processStopWait = 300 * time.Millisecond
	t.Cleanup(func() { processStopWait = old })
	home := testHome(t)
	svc := Service{Runner: &fakeRunner{}, Spec: ServiceSpec{Exe: fakeServeExe(t, true), Home: home}}
	ctx := context.Background()
	if _, err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	pid := svc.Status(ctx).PID
	time.Sleep(200 * time.Millisecond) // 等 sh 設好 trap
	t.Cleanup(func() { _, _ = svc.Stop(ctx) })
	if _, err := svc.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if pidAlive(pid) {
		t.Fatal("忽略 TERM 的行程要用 KILL 停掉")
	}
}

// 沒設 EVERGENIE_SERVICE_MODE 時完全不碰背景行程：launchd 模式的 plist 照舊寫、--root 不呼叫 launchctl。
// Guards: REQ-280
func TestServiceModeDefaultsToLaunchd(t *testing.T) {
	t.Setenv(ServiceModeEnv, "")
	if ProcessMode() {
		t.Fatal("沒設環境變數不該是背景行程模式")
	}
	home := testHome(t)
	exe := testExe(t)
	svc := Service{Runner: &fakeRunner{}, Root: t.TempDir(), Spec: ServiceSpec{Exe: exe, Home: home}}
	if _, err := svc.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(svc.PlistPath()); err != nil {
		t.Fatalf("launchd 模式要寫 plist：%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "runtime", "serve.pid")); err == nil {
		t.Fatal("launchd 模式不該有 pid 檔")
	}
}
