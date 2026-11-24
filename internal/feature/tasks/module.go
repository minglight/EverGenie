// internal/feature/tasks/module.go — 說明卡 ai_docs/modules/tasks.md。

// Package tasks 是業務塊 tasks 的組裝（S8-3）：任務單（task）、待辦（reminder）、Goal（goal）、使用者排程（job）四個子套件，
// 資料 task、reminder、goal、job 都屬於這一塊。這個檔只組裝：規則都在子套件。
package tasks

import (
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/feature/tasks/goal"
	"github.com/minglight/evergenie/internal/feature/tasks/job"
	"github.com/minglight/evergenie/internal/feature/tasks/reminder"
	"github.com/minglight/evergenie/internal/feature/tasks/task"
)

// Vault 是四個子模組要的資料庫操作（底層 vault）。
type Vault interface {
	task.VaultStore
	reminder.VaultStore
	goal.VaultStore
	job.VaultStore
}

// Bus 是事件與封包（底層 bus）：goal 寄確認信的 Mail 也是它。
type Bus interface {
	task.BusSender
	reminder.BusSender
	goal.Bus
	goal.Mailer
	job.BusSender
}

// Timer 是共用的計時迴圈（底層 timer 的 Scheduler）：tasks 的每日跟催、待辦的到期、排程。
type Timer interface {
	task.SchedulerAccess
	reminder.SchedulerAccess
	job.Timer
}

// Agents 是名冊（底層 agent）：認秘書、套排程內文的 slot、寫「主人完成」日誌。
type Agents interface {
	task.AgentLookup
	reminder.AgentLookup
	reminder.JournalWriter
	goal.AgentLookup
	job.JobRenderer
}

// Notify 是送通知（inbox 塊的 notify）。
type Notify interface {
	task.NotifierAccess
	reminder.NotifierAccess
	goal.Notifier
}

// Sessions 是底層 session 給這一塊的能力：單變進行中開工作 session、待辦的查證對話、夜間整理後重開對話。
type Sessions interface {
	task.WorkSessions
	reminder.SessionAccess
	job.SessionRotator
}

// Clock 是時鐘。
type Clock interface{ Now() time.Time }

// Deps 只列塊外的東西；task、reminder 掛 Goal 時直接讀同塊的 goal 資料（goal.Read），不用接。
// session 反過來要讀這一塊的單與 Goal（session.Deps），所以不在這裡：建好 session 之後用 SetSessions 接。
type Deps struct {
	Vault  Vault
	Bus    Bus
	Timer  Timer
	Agents Agents
	Notify Notify
	Clock  Clock
}

// Module 是 tasks 塊：Start 依 job → task → reminder → goal（registry 原本的先後），Stop 反序；登記合併四個子模組的。
type Module struct {
	*app.Group
	job      *job.Module
	task     *task.Module
	reminder *reminder.Module
	goal     *goal.Module
}

// New 建四個子模組；不做事（MOD-N4）。
func New(d Deps) *Module {
	g := goal.New(goal.Deps{Vault: d.Vault, Notify: d.Notify, Bus: d.Bus, Clock: d.Clock, Agents: d.Agents, Mail: d.Bus})
	t := task.New(task.Deps{Scheduler: d.Timer, Agents: d.Agents, Notify: d.Notify, Bus: d.Bus, Vault: d.Vault, Clock: d.Clock})
	r := reminder.New(reminder.Deps{Agents: d.Agents, Notify: d.Notify, Scheduler: d.Timer, Bus: d.Bus, Vault: d.Vault, Journal: d.Agents, Clock: d.Clock})
	j := job.New(job.Deps{Bus: d.Bus, Vault: d.Vault, Timer: d.Timer, Agents: d.Agents, Clock: d.Clock})
	return &Module{Group: app.NewGroup("tasks", j, t, r, g), job: j, task: t, reminder: r, goal: g}
}

// SetSessions 接上 session（它要讀這一塊的單與 Goal，所以建在塊之後；在 Start 之前呼叫）：
// task 的工作 session（SetWork）、reminder 的查證對話、job 的重開對話。沒接就都不做（子模組原本的「沒接」行為）。
func (m *Module) SetSessions(s Sessions) {
	m.task.SetWork(s)
	m.reminder.SetSessions(s)
	m.job.SetSessions(s)
}

// TaskStore 是任務單子模組（session 讀單、notify 記「還在等你」、chat 開對話要讀負責人）。
func (m *Module) TaskStore() *task.Module { return m.task }

// ReminderStore 是待辦子模組。
func (m *Module) ReminderStore() *reminder.Module { return m.reminder }

// GoalStore 是 Goal 子模組（session 問這一輪授權了嗎）。
func (m *Module) GoalStore() *goal.Module { return m.goal }

// JobStore 是排程子模組（session 的「今天排了什麼」、名冊管理的名下排程、套件同步）。
func (m *Module) JobStore() *job.Module { return m.job }

// Commands 合併四個子套件的指令（套件層級，指令行程不建 Module）；名稱重複回錯。
func Commands() (map[string]app.Command, error) {
	return cmdline.Merge(task.Commands(), reminder.Commands(), goal.Commands(), job.Commands())
}

var _ app.Module = (*Module)(nil)
