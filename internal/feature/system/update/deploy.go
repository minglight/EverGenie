package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 部署工具（scripts/deploy.sh）換版時的資料保護（DEV-154）：部署不走 Upgrader.Apply（它自己編、自己重開、一次顧好幾個家），
// 但資料整理一樣在新版 serve 啟動時跑，所以一樣要「先備份、沒過就還原、整理過資料就記下來、可以連資料退回」。分三個時間點，
// 都用新編好的執行檔跑（`evergenie upgrade --deploy begin|undo|done`）：
//   - begin：重開服務之前。備份家、記下現在跑過哪些遷移（DeployMarkFile）。
//   - undo ：部署沒過、要換回舊版之前。這段期間有遷移跑過、或 begin 時就有等著跑的（可能跑到一半）就用備份把資料還原；備份與記號刪掉。
//   - done ：部署後檢查過了。更早的更新前備份收掉、只留這次的（UPD-16）。這段期間沒有遷移跑過：不留紀錄；
//            有：記一筆升級紀錄（帶 Migrations 與 Backup），之後 `--rollback` 跟一般升級一樣；不用請主人確認，更新前的備份只留最新一份（UPD-16）。

// DeployMarkFile 是「部署做到一半」的記號（在升級資料夾裡）。
const DeployMarkFile = "deploy.json"

type deployMark struct {
	At         time.Time `json:"at"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	FromCommit string    `json:"from_commit,omitempty"`
	ToCommit   string    `json:"to_commit,omitempty"`
	Backup     string    `json:"backup"`
	Applied    []string  `json:"applied"`
	Pending    bool      `json:"pending,omitempty"` // begin 的時候有還沒跑的遷移（重開時會跑）
}

func deployMarkPath(home string) string { return filepath.Join(UpgradeDir(home), DeployMarkFile) }

func readDeployMark(home string) *deployMark {
	var m deployMark
	if ok, err := platform.ReadJSONFile(deployMarkPath(home), &m); !ok || err != nil || m.Backup == "" {
		return nil
	}
	return &m
}

// newSince 回 now 裡 before 沒有的 id（保持順序）。
func newSince(before, now []string) []string {
	had := map[string]bool{}
	for _, id := range before {
		had[id] = true
	}
	var out []string
	for _, id := range now {
		if !had[id] {
			out = append(out, id)
		}
	}
	return out
}

// DeployBegin 備份家並留下記號；回備份放在哪。from 是部署前的版本、fromCommit 是部署前的 commit（退回時要用，可以是空的）。
// 上一次部署留下沒收的記號（部署工具中途被打斷）先照 done 的規則收尾。
func (u *Upgrader) DeployBegin(ctx context.Context, from, fromCommit string) (string, error) {
	unlock, err := u.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if old := readDeployMark(u.Home); old != nil {
		// 上一次部署沒收尾（部署工具中途斷線，或 done 沒做成）：那次整理過資料就照 done 記一筆；備份都留著（不知道那次成功沒，不收舊的），等這次部署 done 再照 UPD-16 收。
		u.logf("上一次部署沒有收尾（%s → %s），先替它收尾", old.From, old.To)
		if _, err := u.finishDeploy(old, false); err != nil {
			return "", upgradeFail(UpgradeExitFailed, "上一次部署的收尾沒做成，先不部署：%s", err.Error())
		}
	}
	if from == "" {
		from = "上一版"
	}
	to := platform.Version()
	toCommit := ""
	if u.Core != "" && !u.Download {
		if out, err := u.git(ctx, 10*time.Second, "rev-parse", "HEAD"); err == nil {
			toCommit = strings.TrimSpace(out)
		}
	}
	applied, err := AppliedMigrations(u.Home)
	if err != nil {
		return "", err
	}
	pending, _ := PendingMigrations(u.Home, AllMigrations(u.Home))
	// 檔名帶時間：同一對版本重複部署時，不能蓋到上一次還在等主人確認的備份。
	stem := "deploy-" + safeName(from) + "-" + safeName(to) + "-" + u.now().Format("20060102-150405")
	backup := platform.RuntimeDir(u.Home, "backups", stem+".tgz")
	for n := 2; ; n++ {
		if _, err := os.Lstat(backup); err != nil {
			break
		}
		backup = platform.RuntimeDir(u.Home, "backups", fmt.Sprintf("%s-%d.tgz", stem, n))
	}
	if err := u.backupHome(backup); err != nil {
		return "", upgradeFail(UpgradeExitFailed, "備份失敗，還沒有動任何東西：%s", err.Error())
	}
	mark := deployMark{At: u.now(), From: from, To: to, FromCommit: fromCommit, ToCommit: toCommit, Backup: backup, Applied: applied, Pending: len(pending) > 0}
	if err := platform.WriteJSONFile(deployMarkPath(u.Home), mark); err != nil {
		_ = os.Remove(backup)
		return "", upgradeFail(UpgradeExitFailed, "寫不了部署的記號：%s", err.Error())
	}
	return backup, nil
}

// DeployUndo：部署沒過。begin 之後有遷移跑過，或 begin 時就有等著跑的遷移（可能跑到一半失敗、沒記下來），
// 就把資料還原到備份（只寫回、不拿掉多出來的檔，同自動換回）；回還原了沒。
// 沒有記號（沒 begin 過）什麼都不做。
func (u *Upgrader) DeployUndo() (restored bool, err error) {
	unlock, err := u.lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	mark := readDeployMark(u.Home)
	if mark == nil {
		return false, nil
	}
	now, _ := AppliedMigrations(u.Home)
	if mark.Pending || len(newSince(mark.Applied, now)) > 0 {
		if err := u.restoreHome(mark.Backup, false); err != nil {
			return false, upgradeFail(UpgradeExitStranded, "資料還原失敗（備份還在 %s）：%s", mark.Backup, err.Error())
		}
		restored = true
	}
	_ = os.Remove(mark.Backup)
	_ = os.Remove(deployMarkPath(u.Home))
	return restored, nil
}

// DeployDone：部署後檢查過了。回這次整理了資料的那筆升級紀錄；沒整理資料回 nil（不留紀錄；備份照樣留著當最新一份）。
func (u *Upgrader) DeployDone() (*UpgradeHistory, error) {
	unlock, err := u.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	// 部署換好、檢查過了：之前升級換不回去的紀錄用不到了（首頁不再擺「退回上一版」）。
	removeRescue(u.Home)
	mark := readDeployMark(u.Home)
	if mark == nil {
		return nil, nil
	}
	return u.finishDeploy(mark, true)
}

// finishDeploy 收一次部署的尾（要先拿到鎖）。先寫紀錄、最後才拿掉記號：中途失敗的話記號還在，下次 begin 會再收一次。
func (u *Upgrader) finishDeploy(mark *deployMark, clean bool) (*UpgradeHistory, error) {
	now, _ := AppliedMigrations(u.Home)
	ran := newSince(mark.Applied, now)
	if len(ran) == 0 {
		// 有等著跑的遷移卻一個都沒記下，而且不是正常收尾（工具死在半路）：可能轉到一半，備份留著不刪。
		if mark.Pending && !clean {
			u.logf("上一次部署可能把資料整理到一半，備份留著：%s", mark.Backup)
		} else if clean {
			keepNewestUpdateBackup(u.Home, mark.Backup) // 沒整理資料也留這份：更新前的備份只留最新一份（UPD-16）
		}
		_ = os.Remove(deployMarkPath(u.Home))
		return nil, nil
	}
	h := UpgradeHistory{At: u.now(), Kind: "upgrade", From: mark.From, To: mark.To, FromCommit: mark.FromCommit, ToCommit: mark.ToCommit, OK: true, Migrations: ran, Backup: mark.Backup}
	if err := AppendUpgradeHistory(u.Home, h); err != nil {
		return nil, err
	}
	// 更新前的備份只留這次這份（UPD-16）：只能退最近一次，更早的用不到。紀錄先寫，這次的才是「退回用的那份」。
	// 替上一次沒收尾的部署收（clean=false）不收舊的：不知道那次成功沒。
	if clean {
		keepNewestUpdateBackup(u.Home, mark.Backup)
	}
	_ = os.Remove(deployMarkPath(u.Home))
	u.notify("更新完成，這次整理了資料格式："+mark.From+" → "+mark.To, "不用做什麼。更新前的備份留著最新一份；用起來有問題就跟秘書說要退回 "+mark.From+"，資料也會一起回到更新前。")
	return &h, nil
}
