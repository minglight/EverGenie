package job

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
)

// 每週的備份掃描排程（DEV-163，system 塊的 backupsweep 做事）：核心要有、每個家都要有（規格：兩台都看得到），
// 所以升級時由家的遷移放進 vault/jobs/（BackupSweepMigration），之後跟其他系統級排程一樣可以關、改時間；`jobs rm` 只會關掉它。

// BackupSweepJobID 是排程的 id（系統級，沒有職位）。
const BackupSweepJobID = "backup-sweep"

// BackupSweepMigration 是放這個排程的家的遷移 id。
const BackupSweepMigration = "2026-11-20-backup-sweep-job"

func backupSweepPath(home string) string {
	return filepath.Join(home, "vault", "jobs", BackupSweepJobID+".md")
}

// AddBackupSweepJob 是家的遷移：vault/jobs/ 還沒有 backup-sweep 就放一份（範本 backup-sweep：每週日 04:30、錯過補一次、不推播）。
// 已經有了（主人自己加過、或跑過一次）就不動。能重跑。
func AddBackupSweepJob(home string) error {
	path := backupSweepPath(home)
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return platform.Unavailable("讀不了 vault/jobs/")
	}
	tpl := jobTemplates[BackupSweepJobID]
	body, err := jobTemplateFiles.ReadFile("jobtemplates/" + BackupSweepJobID + ".md")
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339Nano)
	fields := map[string]any{
		"schema": 1, "id": BackupSweepJobID, "created_at": now, "updated_at": now, "by": "system",
		"name": tpl.name, "cron": tpl.job.Cron, "enabled": true, "catchup": tpl.job.Catchup, "action": tpl.job.Action,
		"silent": tpl.job.Silent, "source": "core",
	}
	data := vault.Encode(vault.Record{Kind: "job", ID: BackupSweepJobID, Fields: fields, Body: string(body)}, nil, jobSpec())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return platform.Unavailable("建不了 vault/jobs/")
	}
	return platform.AtomicWrite(path, data, 0o644)
}

// RemoveBackupSweepJob 是退回到這個遷移之前時的收尾：舊版不認得 action backup_sweep，把遷移放的那一份收掉
// （只收 source: core 的；主人自己加的同名排程不動）。
func RemoveBackupSweepJob(home string) error {
	path := backupSweepPath(home)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	r, _, err := vault.Decode(data)
	if err != nil || r.Fields["source"] != "core" {
		return nil
	}
	return os.Remove(path)
}
