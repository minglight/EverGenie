package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/engine"
	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/internal/base/platform"
)

// `evergenie init`（P8 第二輪 S2、REQ-279）：在一個空的資料夾建出一個能開看板的新家。給一鍵安裝（scripts/install.sh）用，也能手動跑。

const initUsage = "evergenie init --home <資料夾> [--name <你的名字>] [--port <看板 port>] [--timezone <時區>] [--state <狀態資料夾>] [--from-backup <備份檔>]"

// defaultBoardPort 是看板預設的 port；被占用就往上找。
const defaultBoardPort = 8765

type initOptions struct {
	Home, Name, Timezone string
	// State 是狀態資料夾（暫存、密鑰、官方角色、別人的套件；家外面）。空的＝預設 ~/.evergenie/state/<家的資料夾名>，home.toml 不寫死。
	State string
	Port  int // 0＝預設 8765，被占用就往上找
}

func initCommand(ctx context.Context, r cmdline.HostRequest) int {
	vals, _, rest, ok := flags(r.Args, []string{"home", "name", "port", "timezone", "state", "from-backup"}, nil)
	if !ok || len(rest) > 0 {
		fmt.Fprintln(r.Err, "用法："+initUsage)
		return 2
	}
	home, err := resolveHome(vals, "") // init 不吃 $EVERGENIE_HOME：一定要明講要建在哪，才不會蓋到現有的家
	if err != nil {
		fmt.Fprintln(r.Err, "用法："+initUsage)
		return 2
	}
	opts := initOptions{Home: home, Name: vals["name"], Timezone: vals["timezone"], State: vals["state"]}
	if v := vals["port"]; v != "" {
		if opts.Port, err = strconv.Atoi(v); err != nil || opts.Port < 1 || opts.Port > 65535 {
			fmt.Fprintln(r.Err, "--port 要是 1–65535 的數字")
			return 2
		}
	}
	// 從備份建家（DEV-153）：先看備份檔讀不讀得懂，再建家、把備份放進去（homebackup.go）。
	backup := vals["from-backup"]
	if backup != "" {
		if backup, err = checkBackupFile(backup); err != nil {
			return fail(r.Err, err)
		}
	}
	// 建家之前本來就在的（空）資料夾：從備份建家失敗要收拾時只清裡面、不把資料夾本身刪掉（比照 createHome 只刪這次才建的）。
	homeWasThere, stateWasThere := isDir(home), isDir(plannedState(opts))
	port, note, err := createHome(opts)
	if err != nil {
		return fail(r.Err, err)
	}
	if backup != "" {
		return finishHomeFromBackup(r, home, backup, note, homeWasThere, stateWasThere)
	}
	fmt.Fprintf(r.Out, "家建好了：%s\n看板網址：http://127.0.0.1:%d/\n", home, port)
	if note != "" {
		fmt.Fprintln(r.Out, note)
	}
	fmt.Fprintf(r.Out, "下一步：evergenie service install --home %s（裝成服務），再打開看板網址完成設定。\n", home)
	return 0
}

// initHome 建出新家，回看板 port（不要提醒的呼叫端用；見 createHome）。
func initHome(o initOptions) (int, error) {
	port, _, err := createHome(o)
	return port, err
}

// createHome 建出新家，回看板 port 與一句要給人看的提醒（帳號設定沒能自動填上時；填上了是空字串）。
// 資料夾必須不存在或是空的，其他情況什麼都不動。
func createHome(o initOptions) (int, string, error) {
	if o.Home == "" || !filepath.IsAbs(o.Home) {
		return 0, "", platform.Invalid("home", "請給家的絕對路徑（--home）")
	}
	created := false
	switch entries, err := os.ReadDir(o.Home); {
	case err == nil && len(entries) > 0:
		return 0, "", platform.Conflict("這裡已經有東西了：" + o.Home + "（init 只在不存在或空的資料夾建新家，不會動現有的家）")
	case err != nil && !os.IsNotExist(err):
		return 0, "", platform.Unavailable("讀不了資料夾 " + o.Home)
	case err != nil:
		created = true
	}
	if o.Name = strings.TrimSpace(o.Name); o.Name == "" {
		o.Name = defaultUserName()
	}
	if o.Timezone == "" {
		o.Timezone = detectTimezone()
	}
	if _, err := time.LoadLocation(o.Timezone); err != nil {
		return 0, "", platform.Invalid("timezone", "時區「"+o.Timezone+"」不認得，請用 IANA 的名字（例如 Asia/Taipei）")
	}
	port := o.Port
	if port == 0 {
		port = freePort(defaultBoardPort)
	}
	// 新家一開始就是佈局第 2 版，而且官方角色放家外（DEV-154）：家裡只有主人的東西，升級與角色包同步不寫家裡的檔。
	// 資料夾一律照佈局算，不自己接 runtime/。
	cfg := platform.LayoutConfig{Version: 2}
	layoutText := "[layout]\nversion = 2\n"
	if o.State = strings.TrimSpace(o.State); o.State != "" {
		o.State = platform.ExpandHome(o.State)
		if !filepath.IsAbs(o.State) {
			return 0, "", platform.Invalid("state", "狀態資料夾請給絕對路徑（--state）")
		}
		cfg.State = o.State
		layoutText += "state = " + strconv.Quote(o.State) + "\n"
	}
	if o.State == "" && testing.Testing() {
		// 測試沒給狀態資料夾會寫進真的 ~/.evergenie/state（2026-11-13 踩過）：一律要明講。
		return 0, "", platform.Invalid("state", "測試裡的 init 要給 State（--state），不能用預設的 ~/."+platform.ProductName+"/state")
	}
	layout := platform.NewLayout(o.Home, cfg)
	if rel, err := filepath.Rel(o.Home, layout.State); err == nil && (rel == "." || !strings.HasPrefix(rel, "..")) {
		return 0, "", platform.Invalid("state", "狀態資料夾要在家的外面："+layout.State)
	}
	stateCreated := false
	switch entries, err := os.ReadDir(layout.State); {
	case err == nil && len(entries) > 0:
		// 預設的狀態資料夾用家的資料夾名：另一個同名的家已經在用，就不能共用（暫存與密鑰會混在一起）。
		return 0, "", platform.Conflict("狀態資料夾已經有東西了：" + layout.State + "（另一個同名的家在用？請換一個家的資料夾名，或加 --state <資料夾> 指定別的位置）")
	case err != nil && !os.IsNotExist(err):
		return 0, "", platform.Unavailable("讀不了狀態資料夾 " + layout.State)
	case err != nil:
		stateCreated = true
	}
	cleanup := func() {
		if created {
			_ = os.RemoveAll(o.Home)
		}
		if stateCreated {
			_ = os.RemoveAll(layout.State)
		}
	}
	if err := os.MkdirAll(o.Home, 0o755); err != nil {
		return 0, "", platform.Unavailable("建不了資料夾 " + o.Home)
	}
	for _, d := range []string{filepath.Join(o.Home, "settings"), filepath.Join(o.Home, "vault"), filepath.Join(o.Home, "personas", "mine"), layout.Runtime("logs"), layout.Runtime("sockets"), layout.OfficialPersonas()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			cleanup()
			return 0, "", platform.Unavailable("建不了 " + d)
		}
	}
	// 第一個帳號的帳號設定（設定資料夾等）問引擎：用這台電腦現在登入的那一個（ENG-19）。問不到就不寫，提醒主人到看板填。
	engineConfigText, note := "", ""
	if ec, why := engineAccountDefaults(registry.Default); why != "" {
		note = "注意：帳號的設定資料夾沒有自動填上（" + why + "），第一次開秘書前請到看板「設定 → 電腦」的「AI 帳號」填。"
	} else {
		engineConfigText = "\n[accounts.main.engine_config]\n" + ec
	}
	text := fmt.Sprintf(`schema = 2
mode = "simple"

[user]
name = %s
language = "zh-TW"
timezone = %s

[slots]
default = "life"

[slots.life]
label = "日常"
remind = "08:30"
check = "18:00"

[accounts]
default = "main"

[accounts.main]
engine = %s
%s
[web]
port = %d

[update]
channel = "release"

%s`, strconv.Quote(o.Name), strconv.Quote(o.Timezone), strconv.Quote(registry.Default), engineConfigText, port, layoutText)
	if err := platform.AtomicWrite(filepath.Join(o.Home, "home.toml"), []byte(text), 0o644); err != nil {
		cleanup()
		return 0, "", platform.Unavailable("寫不了 home.toml")
	}
	if _, err := platform.LoadHome(o.Home); err != nil {
		cleanup()
		return 0, "", err
	}
	return port, note, nil
}

// engineAccountDefaults 把引擎套件給的第一個帳號的 engine_config（engine.AccountDefaults，ENG-19）寫成 TOML 的幾行（鍵排好序）。
// 問不到回一句白話的原因（第二個回傳值），不寫帳號設定。
func engineAccountDefaults(engineID string) (string, string) {
	m, why := engine.AccountDefaults(context.Background(), engineID)
	if why != "" {
		return "", why
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		var line bytes.Buffer
		if err := toml.NewEncoder(&line).Encode(map[string]any{k: m[k]}); err != nil {
			return "", "問到的帳號設定看不懂"
		}
		b.Write(line.Bytes())
	}
	return b.String(), ""
}

func defaultUserName() string {
	if u, err := user.Current(); err == nil {
		if u.Name != "" {
			return u.Name
		}
		if u.Username != "" {
			return u.Username
		}
	}
	return "我"
}

// detectTimezone 找這台電腦的時區（IANA 名稱）：TZ 環境變數、/etc/localtime 指到的 zoneinfo；都找不到用 UTC。
func detectTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if _, after, ok := strings.Cut(link, "zoneinfo/"); ok {
			if _, err := time.LoadLocation(after); err == nil {
				return after
			}
		}
	}
	return "UTC"
}

// freePort 從 start 開始找第一個沒被占用的 port（最多找 200 個）。
func freePort(start int) int {
	for p := start; p < start+200 && p <= 65535; p++ {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err == nil {
			_ = ln.Close()
			return p
		}
	}
	return start
}
