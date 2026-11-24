package pkg

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

type process struct {
	done <-chan error
	kill func()
}

// startProgram 啟動套件行程：不帶參數，所有資訊走環境變數；標準輸出與錯誤寫到 logPath。
// kill 先送 SIGTERM，3 秒後強制結束。
func startProgram(ctx context.Context, bin, dir string, env []string, logPath string) (*process, error) {
	pctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(pctx, bin)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 3 * time.Second
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err == nil {
		// 接在後面寫（DEV-8）：重開會清掉上一次的輸出，就看不到它為什麼掛。大小由 trimLog 控制。
		trimLog(logPath)
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			fmt.Fprintf(f, "---- %s 啟動 %s\n", time.Now().Format(time.RFC3339), filepath.Base(bin))
			cmd.Stdout, cmd.Stderr = f, f
			defer f.Close() // 子行程已繼承 fd
		}
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); cancel(); close(done) }()
	return &process{done: done, kill: cancel}, nil
}

// logKeep：out.log 超過 logMax 就只留最後 logKeep 位元組（每次啟動時檢查）。
const (
	logMax  = 1 << 20
	logKeep = 256 << 10
)

func trimLog(path string) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) <= logMax {
		return
	}
	_ = os.WriteFile(path, data[len(data)-logKeep:], 0o644)
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// fingerprint 是套件資料夾的內容指紋（路徑、大小、修改時間）；略過版本控制、相依與 fork 基準。
func fingerprint(dir string) string {
	h := sha1.New()
	var lines []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() && p != dir && (name == ".git" || name == "node_modules" || name == forkBaseDir) {
			return filepath.SkipDir
		}
		if d.IsDir() || name == ".DS_Store" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		lines = append(lines, fmt.Sprintf("%s|%d|%d", rel, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	sort.Strings(lines)
	h.Write([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h.Sum(nil))
}

// scanFingerprintsLocked：只看 mine 與 installed（官方的唯讀，跟著發版）。回傳 資料夾 → 指紋。
func (m *Module) scanFingerprintsLocked() map[string]string {
	out := map[string]string{}
	for _, tier := range []sdk.Tier{sdk.TierInstalled, sdk.TierMine} {
		root := m.tierDir(tier)
		items, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, it := range items {
			if it.IsDir() && !skipPackageDir(it.Name()) {
				dir := filepath.Join(root, it.Name())
				if notAPackage(dir, tier) {
					continue // 主人自己放的資料夾：不是套件，不用每拍走一遍
				}
				out[dir] = fingerprint(dir)
			}
		}
	}
	return out
}

// pollChangesLocked：回傳「已經穩定的改動」的資料夾。同一個指紋要連續兩輪都看到才算穩定（編輯中途不重編）。
func (m *Module) pollChangesLocked() map[string]bool {
	cur := m.scanFingerprintsLocked()
	changed := map[string]bool{}
	dirs := map[string]bool{}
	for d := range cur {
		dirs[d] = true
	}
	for d := range m.seen {
		dirs[d] = true
	}
	for d := range dirs {
		now, was := cur[d], m.seen[d]
		if now == was {
			delete(m.pend, d)
			continue
		}
		if m.pend[d] == now {
			changed[d] = true
			delete(m.pend, d)
			if now == "" {
				delete(m.seen, d)
			} else {
				m.seen[d] = now
			}
			continue
		}
		m.pend[d] = now
	}
	return changed
}

// Tick 是監督的一拍：偵測 mine／installed 的改動、處理行程結束、必要時編譯與啟動。測試直接用假時間呼叫。
func (m *Module) Tick(ctx context.Context, now time.Time) {
	m.mu.Lock()
	if m.run == nil || m.run.Err() != nil || ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	if changed := m.pollChangesLocked(); len(changed) > 0 {
		m.rescanLocked(now)
		for _, e := range m.entries {
			if changed[e.dir] {
				e.rebuild, e.gaveUp, e.wantRun, e.restarts = true, false, true, 0
			}
		}
	}
	for _, id := range m.order {
		e := m.entries[id]
		switch {
		case e == nil:
		case supervisable(e):
			m.superviseLocked(e, now)
		case e.proc != nil && (e.err != nil || e.man == nil):
			// 跑著的套件後來被拒（manifest 壞了、相依的套件停用了、資料轉換沒過）：行程收掉，不留一個沒人管的在跑
			m.stopProcLocked(e)
		}
	}
	m.mu.Unlock()
	m.flushAlerts()
}

// supervisable：這個套件歸監督管（有效的 manifest、附程式、沒被拒、不是外部提供的）。
func supervisable(e *entry) bool {
	return e.err == nil && e.man != nil && e.man.Kind == string(sdk.PackageKindProgram) && !e.external
}

func (m *Module) superviseLocked(e *entry, now time.Time) {
	if !e.enabled {
		m.stopProcLocked(e)
		e.state, e.errText = sdk.PackageStateStopped, ""
		return
	}
	if e.building {
		return
	}
	if e.proc != nil {
		select {
		case err := <-e.proc.done:
			m.onExitLocked(e, err, now)
		default:
			if !e.hello && now.Sub(e.launched) > m.HelloTimeout {
				e.errText = "行程啟動後沒有在時限內向核心報到"
				e.proc.kill()
			}
			if e.rebuild {
				m.startBuildLocked(e) // 舊的繼續跑，新版編好才換
			}
			return
		}
	}
	if e.proc == nil && e.wantRun && (e.rebuild || (!e.gaveUp && !now.Before(e.next))) {
		m.startBuildLocked(e)
	}
}

// onExitLocked：行程結束。只影響這個套件（REQ-96）：標 failed、發 system.alert、依 program.restart 與退避重啟。
func (m *Module) onExitLocked(e *entry, err error, now time.Time) {
	healthy := e.hello && now.Sub(e.helloAt) >= healthyAfter
	e.proc, e.hello = nil, false
	if healthy {
		e.restarts = 0
	}
	if be := e.buildErr; be != nil {
		// 新版編不過、舊版也結束了：不會自動重啟（編不過要等改檔），錯誤換成編譯的那個；通知只放白話（不夾編譯工具的原文）
		e.buildErr = nil
		e.state, e.errText = sdk.PackageStateFailed, "編譯失敗："+errMessage(be)
		if err != nil {
			if last, ok := m.crashAlert[e.id]; !ok || now.Sub(last) >= crashAlertEvery {
				m.crashAlert[e.id] = now
				m.alertLocked(fmt.Sprintf("套件「%s」掛了", e.man.Name), "新版編譯失敗："+buildNotice(be)+"；舊版也停了，不會自動重啟；修好之後存檔或重新載入就會再試。")
			}
		}
		return
	}
	policy := e.man.restart()
	clean := err == nil
	if !clean {
		e.state = sdk.PackageStateFailed
		if e.errText == "" {
			e.errText = "行程異常結束"
		}
		if last, ok := m.crashAlert[e.id]; !ok || now.Sub(last) >= crashAlertEvery {
			m.crashAlert[e.id] = now // 同一個套件 10 分鐘內只通知一次（LOG-6）
			m.alertLocked(fmt.Sprintf("套件「%s」掛了", e.man.Name), e.errText+"；核心會依設定重啟它，其他套件不受影響。")
		}
		if m.log != nil {
			m.log.Warn("套件行程異常結束", "id", e.id)
		}
	} else {
		e.state, e.errText = sdk.PackageStateStopped, ""
	}
	if policy == "never" || (policy == "on-failure" && clean) {
		e.wantRun = false
		return
	}
	e.restarts++
	if e.restarts > maxRestarts {
		e.gaveUp, e.state = true, sdk.PackageStateFailed
		e.errText = "連續失敗太多次，已停止自動重啟；修好之後存檔或手動重新載入就會再試"
		m.alertLocked(fmt.Sprintf("套件「%s」連續失敗，已停止重啟", e.man.Name), e.errText)
		return
	}
	delay := m.RestartDelay << (e.restarts - 1)
	if delay > 5*time.Minute || delay <= 0 {
		delay = 5 * time.Minute
	}
	e.next = now.Add(delay)
}

// startBuildLocked 在背景編譯（必要時）並啟動；完成後回到鎖裡更新狀態。
func (m *Module) startBuildLocked(e *entry) {
	m.buildSeq++
	e.building, e.rebuild, e.buildID = true, false, m.buildSeq
	if e.proc == nil {
		e.state, e.errText = sdk.PackageStateBuilding, ""
	}
	id, tier, dir, man, run, bid := e.id, e.tier, e.dir, e.man, m.run, e.buildID
	m.async(func() {
		bin, err := m.ensureBinary(run, id, tier, dir, man)
		var fp string
		if err == nil && tier != sdk.TierOfficial {
			fp = fingerprint(dir) // 編譯可能動到 go.sum 之類，更新已套用的指紋，避免自己觸發自己
		}
		m.mu.Lock()
		now := m.d.Clock.Now()
		cur := m.entries[id]
		if cur != nil && cur != e && cur.building && cur.buildID == bid {
			// 編譯中重掃過（偵測到存檔、Reload）：entry 換了一份、狀態照抄（含這次編譯），結果算在現在這份上；
			// 不然那份會一直停在「編譯中」，之後再也不編、不啟動。
			e = cur
		}
		if cur != e || run.Err() != nil {
			e.building = false
			m.mu.Unlock()
			return
		}
		e.building = false // rebuild 不清：編譯中又要求重編的（Reload、存檔），下一拍再編一次
		if fp != "" {
			m.seen[dir] = fp
			delete(m.pend, dir)
		}
		if !supervisable(e) || !e.enabled {
			// 編譯中停用、被拒、manifest 改壞了：不啟動、不通知（重新啟用或修好時照常再編；編好的不用重編）
			if e.proc == nil && e.state == sdk.PackageStateBuilding {
				e.state, e.errText = sdk.PackageStateStopped, ""
			}
			m.mu.Unlock()
			return
		}
		if miss := (*goMissing)(nil); errors.As(err, &miss) {
			// 沒有能用的 Go：不自動裝，標 failed＋白話原因；能幫忙下載時才帶補救代號，主人同意後由 InstallGo 補裝（PKG-65、PKG-N5）。
			// 不能幫忙下載（原始碼版、這台晶片沒有包）就不帶補救代號：按了也裝不了，白話裡寫了要怎麼做。
			text := m.tools.missingText(miss.reason)
			body := text + "（裝好 Go 之後重新載入套件就會再編一次）"
			e.gaveUp, e.remedy = true, ""
			if _, _, why := m.tools.offer(); why == "" {
				e.remedy = sdk.PackageRemedyInstallGo
				body = text + "（跟秘書說一聲，或到設定的套件頁按「安裝 Go」；你同意之前不會下載）"
			}
			e.state, e.errText = sdk.PackageStateFailed, text
			m.alertLocked(fmt.Sprintf("套件「%s」要先裝編譯工具（Go）才能編譯", man.Name), body)
			m.mu.Unlock()
			m.flushAlerts()
			return
		}
		e.remedy, e.buildErr = "", nil
		if err != nil {
			e.gaveUp = true // 編不過就不用一直重試：等檔案改動
			msg := "編譯失敗：" + errMessage(err)
			if e.proc != nil {
				e.errText, e.buildErr = "新版"+msg+"；仍在跑舊版", err
			} else {
				e.state, e.errText = sdk.PackageStateFailed, msg
			}
			m.alertLocked(fmt.Sprintf("套件「%s」編譯失敗", man.Name), "編譯失敗："+buildNotice(err)) // 通知只放白話；原文在套件的錯誤裡給秘書看
			m.mu.Unlock()
			m.flushAlerts()
			return
		}
		m.stopProcLocked(e) // 新版編好了才換掉舊的
		e.gaveUp = false
		m.launchLocked(e, bin, now)
		m.mu.Unlock()
		m.flushAlerts()
	})
}

func (m *Module) launchLocked(e *entry, bin string, now time.Time) {
	e.token = randomToken()
	layout := platform.LayoutFor(m.home)
	env := append(filterEnv(os.Environ(), sdk.EnvPrefixes("EVERGENIE_HOME=", "EVERGENIE_API_SOCKET=", "EVERGENIE_PACKAGE_", "EVERGENIE_STATE=", "EVERGENIE_MEDIA=", "EVERGENIE_OFFICIAL_PERSONAS=", sdk.EnvBundledPersonas+"=")...),
		"EVERGENIE_HOME="+m.home,
		sdk.EnvBundledPersonas+"="+bundledCardsIn(m.officialDir()), // 跟著程式的公開卡（PRS-25）：套件用 sdk.OfficialCardPath 找官方卡
		"EVERGENIE_API_SOCKET="+layout.Runtime("sockets", "api.sock"),
		"EVERGENIE_PACKAGE_ID="+e.id,
		"EVERGENIE_PACKAGE_API="+e.man.API,
		"EVERGENIE_PACKAGE_VERSION="+e.man.Version,
		"EVERGENIE_PACKAGE_DIR="+e.dir)
	env = append(env, layout.Env()...) // 佈局（DEV-145）：套件用 sdk.RuntimeDir 等拿路徑
	p, err := m.launch(m.run, bin, e.dir, env, filepath.Join(m.runtimeDir(e.id), "out.log"))
	if err != nil {
		e.state, e.errText = sdk.PackageStateFailed, "無法啟動行程"
		e.restarts++
		e.next = now.Add(m.RestartDelay)
		m.alertLocked(fmt.Sprintf("套件「%s」無法啟動", e.man.Name), e.errText)
		return
	}
	e.proc, e.launched = p, now
	e.state, e.errText = sdk.PackageStateBuilding, "" // 已啟動，等它向核心報到（Hello）
}

// stopProcLocked 收掉這個套件現在的行程（停用、移除、換位置時）：不算失敗、不通知。
func (m *Module) stopProcLocked(e *entry) {
	if e.proc == nil {
		return
	}
	old := e.proc
	old.kill()
	e.proc, e.hello = nil, false
	go func() { <-old.done }()
}

func filterEnv(env []string, prefixes ...string) []string {
	out := make([]string, 0, len(env))
outer:
	for _, v := range env {
		for _, p := range prefixes {
			if strings.HasPrefix(v, p) {
				continue outer
			}
		}
		out = append(out, v)
	}
	return out
}

func (m *Module) alertLocked(title, context string) {
	m.alerts = append(m.alerts, alert{title, context})
}

// flushAlerts 在鎖外發 system.alert（notify 會把它變成通知）；發不出去只寫 log。
func (m *Module) flushAlerts() {
	m.mu.Lock()
	pending := m.alerts
	m.alerts = nil
	log := m.log
	m.mu.Unlock()
	for _, a := range pending {
		if m.d.Bus == nil {
			continue
		}
		if err := m.d.Bus.Publish(context.Background(), "system.alert", map[string]any{"title": a.title, "context": a.context, "agent": ""}); err != nil && log != nil {
			log.Warn("system.alert 發不出去")
		}
	}
}
