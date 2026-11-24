package main

import (
	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/packages/pkg"
	"github.com/minglight/evergenie/internal/feature/system/update"
	"github.com/minglight/evergenie/internal/feature/tasks/job"
	"github.com/minglight/evergenie/internal/feature/tasks/task"
	"github.com/minglight/evergenie/sdk"
)

// homeMigrations 是核心要跑的家的遷移（照順序）。各業務模組只匯出「怎麼整理」的函式，業務模組之間不 import，
// 所以由組裝點在 main 一開始交給 update（serve 啟動前與 `migrate-home` 都看 update.HomeMigrations）。
func homeMigrations() []update.HomeMigration {
	return []update.HomeMigration{
		// 任務單狀態九種收成六種、拿掉 site（2026-11-01，設計 design-goal-task.md；轉法見 tasks/task/legacy.go）。
		{ID: "2026-11-01-task-statuses", Description: "任務單狀態收成六種（待做、進行中、等你、等外部、完成、不做了）", Run: task.MigrateLegacyStatuses},
		// 舊的 [incidents] dispatcher 抄成 owner（2026-11-01 預演三個家升級時發現；轉法見 platform/incidents.go）。
		{ID: "2026-11-01-incidents-owner", Description: "事故負責人：舊設定的 [incidents] dispatcher 抄成 owner", Run: platform.MigrateIncidentsOwner},
		// 官方套件帶來的職位，職責改成跟著程式走；主人寫在職責內文裡的客製原樣搬到附加規則（DEV-154；轉法見 agent/duties_migrate.go）。
		{ID: agent.OfficialDutiesMigration, Description: "官方職責改成跟著程式走，家裡只留附加規則（寫在職責裡的客製原樣搬到附加規則）", Run: migrateOfficialDuties},
		// 共用的使用者 profile 退休，內文搬進主題筆記「主人」（DEV-161；轉法見 agent/profile_migrate.go）。
		{ID: agent.OwnerTopicMigration, Description: "使用者 profile（vault/profile.md）搬進主題筆記「主人」（先備份，舊檔留著）", Run: agent.MigrateProfileToOwnerTopic,
			// 退回到這一版之前：核心產生的 索引.md 舊版不認得（會當成壞檔），收掉；新版再啟動會重產。
			Undo: agent.RemoveGeneratedNoteIndexes},
		// 每週的備份掃描排程放進 vault/jobs/（DEV-163；排程本身在 tasks/job，做事的是 system 塊的 backupsweep）。
		{ID: job.BackupSweepMigration, Description: "加上每週的備份掃描排程（backup-sweep：週日 04:30，只出報告）", Run: job.AddBackupSweepJob,
			// 退回到這一版之前：舊版不認得 action backup_sweep，收掉遷移放的那一份。
			Undo: job.RemoveBackupSweepJob},
	}
}

// packageMigrations 把官方套件 package.toml 宣告的資料轉換（[[migration]]，DEV-154）交給 update：官方套件改了紀錄格式
// 要附轉換，升級時跟核心的遷移一起跑、一起記。它們是同一組（Group "pkg"）：還沒跑過的一次交給 pkg.RunDataMigrations，
// 整批先預演，有一筆轉不過去就一個檔都不寫。主人自己的、別人分享的套件不在這裡：它們的轉換在套件載入時跑，
// 轉不過去只停那個套件，不擋升級也不擋啟動（法律第 3 條，壞了只壞它自己）。
func packageMigrations(home string) []update.HomeMigration {
	var all []pkg.DataMigration
	for _, m := range pkg.DataMigrations(home) {
		if m.Tier == sdk.TierOfficial {
			all = append(all, m)
		}
	}
	runGroup := func(home string, ids []string) error {
		want := map[string]bool{}
		for _, id := range ids {
			want[id] = true
		}
		var batch []pkg.DataMigration
		for _, m := range all {
			if want[m.ID] {
				batch = append(batch, m)
			}
		}
		return pkg.RunDataMigrations(home, batch)
	}
	var out []update.HomeMigration
	for _, m := range all {
		out = append(out, update.HomeMigration{ID: m.ID, Description: "套件「" + m.Package + "」：" + m.Description, Run: m.Run, Group: "pkg", RunGroup: runGroup})
	}
	return out
}

// migrationLog 把 update 的遷移紀錄（settings/migrations.toml）接給套件模組（pkg.MigrationLog）。
type migrationLog struct{}

func (migrationLog) Applied(home string) ([]string, error) { return update.AppliedMigrations(home) }
func (migrationLog) Mark(home string, ids []string) error {
	return update.MarkMigrationsApplied(home, ids)
}

// officialDutiesMigrated 是遷移 2026-11-12-official-duties 認得的職位：寫這個遷移時就有的官方樣板
// （secretaries 的 life、work、code、system，health 的 health；旁邊都有 history.txt），以前抄進過家裡，才有東西要轉。
// 之後才加的樣板（2026-11-14 的 art，DEV-157；2026-11-15 起改放開發工坊 admin，DEV-160）從來沒抄進家裡：家裡同 id 的職位
// 是主人自己建的，職責一個字都不能動（AGT-38、ADM-39）。之後新增官方職位樣板不要加進這份清單。
var officialDutiesMigrated = map[string]bool{"life": true, "work": true, "code": true, "system": true, "health": true}

// migrateOfficialDuties 把套件模組讀到的官方職位樣板轉給名冊的遷移（名冊不 import 套件）；只轉 officialDutiesMigrated 裡的職位。
func migrateOfficialDuties(home string) error {
	official := map[string]agent.OfficialDutyTemplate{}
	for id, d := range pkg.OfficialDuties(home) {
		if !officialDutiesMigrated[id] {
			continue
		}
		official[id] = agent.OfficialDutyTemplate{Ref: d.Ref(), Known: d.Known}
	}
	return agent.MigrateOfficialDuties(home, official)
}

// movedDuties 給升級報告：這次跑了職責遷移時，列出客製被搬到附加規則的職位。
func movedDuties(home string, ran []string) []string {
	for _, id := range ran {
		if id == agent.OfficialDutiesMigration {
			return agent.MovedDuties(home)
		}
	}
	return nil
}
