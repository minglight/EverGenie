package main

import (
	"os"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/board"
	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/engine"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/session"
	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/feature/agents"
	"github.com/minglight/evergenie/internal/feature/agents/persona"
	"github.com/minglight/evergenie/internal/feature/chat"
	"github.com/minglight/evergenie/internal/feature/inbox"
	"github.com/minglight/evergenie/internal/feature/packages"
	"github.com/minglight/evergenie/internal/feature/packages/pkg"
	"github.com/minglight/evergenie/internal/feature/system"
	"github.com/minglight/evergenie/internal/feature/system/update"
	"github.com/minglight/evergenie/internal/feature/tasks"
	"github.com/minglight/evergenie/internal/feature/tasks/reminder"
	"github.com/minglight/evergenie/internal/feature/tasks/task"
	"github.com/minglight/evergenie/internal/feature/trust"
	"github.com/minglight/evergenie/internal/feature/trust/rules"
)

// modules 是組裝好的 app 與測試要直接拿來用的幾個模組。
type modules struct {
	vault     *vault.Module
	agent     *agent.Module
	tasks     *task.Module
	reminders *reminder.Module
	api       *app.HTTPServer
	engines   *engine.Module
	board     *board.Module
	app       *app.App
}

// Build validates the home and assembles all modules into one app: Start in the order given to app.New, Stop in reverse.
func Build(home string) (*app.App, error) {
	m, err := buildModules(home)
	if err != nil {
		return nil, err
	}
	return m.app, nil
}

// buildModules 建底層與 7 個業務塊（建構順序跟啟動順序不同：塊要的別塊先建，互相需要的用 setter 接），再交給 app 組裝。
func buildModules(home string) (*modules, error) {
	if _, err := platform.LoadHome(home); err != nil {
		return nil, err
	}
	clock := platform.SystemClock{}
	store := vault.New(vault.Deps{Clock: clock})
	messages := bus.New(bus.Deps{Vault: store, Clock: clock})
	timing := timer.NewScheduler(clock) // 共用的計時迴圈：任務單的每日跟催、待辦的到期、使用者排程
	roster := agent.New(agent.Deps{Bus: messages, Vault: store, Clock: clock})
	roster.SetCoreFinder(func() string { // 開發家的共用規則要告訴秘書 repo 在哪
		exe, _ := os.Executable()
		h, _ := platform.LoadHome(home)
		return update.MainRepo(update.FindCore(h, exe)) // 開發家從專屬 worktree 跑：給秘書的是主 repo，不是跑著的那份
	})
	engines := engine.New(engine.Deps{Bus: messages, Clock: clock})

	// 業務塊。inbox 的選單要看板的前端資源，前端資源要套件，所以 packages 最先建（menu 沒有 setter，也不能先傳有型別的 nil）。
	pkgs := packages.New(packages.Deps{Agents: roster, Bus: messages, Vault: store, Clock: clock})
	roster.SetPromptAppends(func(agentID string) []agent.PromptAppend { // 套件 prompt_append 擴充點的值接進秘書指令（DEV-154）
		var out []agent.PromptAppend
		for _, p := range pkgs.Store().PromptAppends(agentID) {
			out = append(out, agent.PromptAppend{Source: p.Package + "." + p.Extension, Title: p.Description, Text: p.Text})
		}
		return out
	})
	roster.SetOfficialDuties(pkgs.Store().OfficialDuties) // 官方套件帶來的職位：職責讀樣板，家裡不存一份（DEV-154）
	assets := board.NewAssets(pkgs.Store())               // 看板外殼的套件前端資源：選單、名冊管理、看板用同一份
	box := inbox.New(inbox.Deps{Bus: messages, Vault: store, Clock: clock, Frontend: assets, Roster: roster})
	notices := box.Notices()
	work := tasks.New(tasks.Deps{Vault: store, Bus: messages, Timer: timing, Agents: roster, Notify: notices, Clock: clock})
	box.SetTasks(work.TaskStore()) // 「還在等你」記進任務單的 nudges（P7-6），也讀單的狀態收掉已結束任務的訊息
	permits := trust.New(trust.Deps{Vault: store, Notify: notices, Bus: messages, Engine: engines, Clock: clock, Names: roster})
	// 權限：引擎讀政策；核准的 guidance 寫進產生的設定，規則在開 session 時交給引擎（Q7）。
	engines.SetTrustSource(rules.Policy)
	engines.SetGuidanceSource(permits.Requests().ActiveGuidance)
	engines.SetRuleSource(permits.Requests().ActiveRules)
	people := agents.New(agents.Deps{Roster: roster, Jobs: work.JobStore(), Clock: clock, Vault: store, Mail: messages, Cards: engines, Directory: roster, Avatars: assets, Health: roster, Templates: pkgs.Store(), Notes: roster})
	library := people.Library()
	roster.SetPersonaSource(library) // 名冊讀角色卡與說話層經角色庫；角色庫要名冊的職位清單，所以這邊用 setter
	people.SetPackPointer(pkg.PersonasPackPointer)
	people.SetBundledCards(pkg.PersonasBundledCards)                        // 跟著程式的公開卡（PRS-25）：角色庫不 import packages
	people.SetPackagePersonas(pkgs.Store())                                 // 套件角色（SEC-302 S2）：角色庫不 import packages
	library.SetNotifier(personaNotices{notices})                            // 新角色來打招呼（DEV-115）：角色庫不 import notify
	people.SetRecordStore(personaRecords{pkgs: pkgs.Store(), vault: store}) // 角色包同步寫跟著角色走的紀錄（PKG-59）：角色庫不 import packages

	sessions := session.New(session.Deps{Goals: work.GoalStore(), Engine: sessionEngines{engines}, Agents: sessionAgents{roster, library}, Notify: notices, Bus: messages, Vault: store, Tasks: sessionTasks{work.TaskStore()}, Nudge: notices, Clock: clock})
	sessions.SetTrust(permits.Levels(), permits.Levels())
	sessions.SetScheduledToday(work.JobStore().ScheduledOn)
	work.SetSessions(sessions) // 單改成進行中開工作 session、待辦的查證對話、夜間整理後重開對話
	people.SetTester(sessions) // personas.test 轉給 session 的測口吻
	sys := system.New(system.Deps{Engine: engines, Vault: store, Notify: notices, Bus: messages, Sessions: sessions, Closer: sessions, Roster: roster, Clock: clock, Push: system.PushKeys{Ready: inbox.PushKeysReady, Ensure: inbox.EnsurePushKeys}, Events: messages})
	talk := chat.New(chat.Deps{Sessions: sessions, Bus: messages, Tasks: work.TaskStore(), Roster: roster, Clock: clock})

	// 套件（P6-1）：引擎套件由 engine 監督，packages 只列出狀態；沒有 Go 時主人同意才由 pkg 自己下載到程式的工具資料夾（packages PKG-65）。
	pkgs.SetMigrationLog(migrationLog{}) // 主人自己的、分享來的套件載入時做資料轉換，紀錄跟升級的同一份（DEV-154）
	pkgs.SetExternal(engineClaims{engines})
	pkgs.SetEngines(engines)
	pkgs.SetSyncHost(packageSync{home: home, roster: roster, admin: people.Admin(), store: store, timing: work.JobStore()})
	pkgs.SetClaimHost(packageClaim{roster: people.Admin(), personas: library, notices: notices})

	gateway := app.NewHTTPServer(app.HTTPDeps{Clock: clock}) // 唯一的 HTTP 入口（unix socket）：掛各模組登記的路由
	shell := board.New(board.Deps{Assets: assets, Agents: roster, API: gateway, Clock: clock})
	runner := timer.NewJobRunner() // 各模組 Jobs() 登記的定時工作

	// 啟停順序就是這份清單的順序（反序停），一塊一格：底層在前；trust 在 engine 前（引擎握手要讀到已核准的 guidance）；
	// tasks 在 session 前（session 的 Start 讀今天的排程）；packages 在 engine、session 後（它的 Start 會叫起套件行程）；
	// gateway 在所有登記路由的之後；看板在 gateway 後（它的資料經 gateway）；定時工作的執行器最後（模組都起來才跑、關機最先停）。
	all := app.New(platform.New(platform.Deps{}), store, messages, timing, roster,
		box, people, work, permits, sys,
		engines, sessions,
		talk, pkgs,
		gateway, shell, runner)
	for _, m := range all.Modules() {
		if h, ok := m.(interface{ SetHome(string) }); ok {
			h.SetHome(home) // 塊的 SetHome 交給它的子模組；看板的交給前端資源
		}
	}
	// 組裝：定時工作交給 runner、健檢交給 doctor、通知來源交給 notify、首頁資料段交給 overview（各自的 Consume…）；
	// 重名、缺欄位、名稱或鍵不合格在啟動前擋下。
	if err := all.Assemble(); err != nil {
		return nil, err
	}
	mounts, err := all.Mounts()
	if err != nil {
		return nil, err
	}
	gateway.SetMounts(mounts)
	return &modules{vault: store, agent: roster, tasks: work.TaskStore(), reminders: work.ReminderStore(), api: gateway, engines: engines, board: shell, app: all}, nil
}

// featureCommands 合併所有指令：7 個業務塊的 Commands（套件層級，指令行程只是 client，不建 Module）、
// 要動這台電腦的本機指令（hostCommands）。名稱重複會回錯。
func featureCommands() (map[string]app.Command, error) {
	sets := []map[string]app.Command{chat.Commands(), hostCommands()}
	for _, f := range []func() (map[string]app.Command, error){
		tasks.Commands,
		inbox.Commands,
		func() (map[string]app.Command, error) {
			return agents.Commands(chat.StateLabel, pkg.PersonasPackPointer, pkg.PersonasBundledCards, pkg.PersonasFileKinds)
		},
		func() (map[string]app.Command, error) { return trust.Commands(system.PrintDoctor) },
		func() (map[string]app.Command, error) {
			// 角色庫的兩項：角色包待補（PRS-6）、我的卡跟官方角色同代號（PRS-27，要知道跟著程式的公開卡在哪）
			return system.Commands(preflightCommand, launchdCheck, tailscaleCheck, persona.OfflineChecks, persona.SlugClashChecks(pkg.PersonasBundledCards))
		},
		packages.Commands,
	} {
		set, err := f()
		if err != nil {
			return nil, err
		}
		sets = append(sets, set)
	}
	return app.New().Commands(sets...)
}
