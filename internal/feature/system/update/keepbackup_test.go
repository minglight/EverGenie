package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 更新前的備份只留最新一份（UPD-16）：更早的 upgrade-*／deploy-* 收掉；換不回去的紀錄要用的、
// 退回還用得到的（整理過資料、現在就是那一版）不收；別種備份（rollback-*、資料夾裡的小備份）不碰。
func TestKeepNewestUpdateBackup(t *testing.T) {
	e := newUpEnv(t)
	dir := platform.RuntimeDir(e.home, "backups")
	if err := os.MkdirAll(filepath.Join(dir, "trust"), 0o700); err != nil {
		t.Fatal(err)
	}
	touch := func(name string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	oldUp := touch("upgrade-0.1.0-0.2.0.tgz")
	oldDeploy := touch("deploy-0.2.0-0.3.0-20261101-010101.tgz")
	rescued := touch("upgrade-0.0.9-0.1.0.tgz")
	needed := touch("deploy-0.3.0-0.4.0-20261110-010101.tgz")
	other := touch("rollback-20261101-0.1.0.tgz")
	small := touch(filepath.Join("trust", "20261101.toml"))
	keep := touch("deploy-0.4.0-0.4.0-20261121-010101.tgz")

	if err := writeRescue(e.home, upgradeRescue{From: "0.0.9", To: "0.1.0", Backup: rescued}); err != nil {
		t.Fatal(err)
	}
	if err := AppendUpgradeHistory(e.home, UpgradeHistory{At: time.Now(), Kind: "upgrade", From: "0.3.0", To: "0.4.0", OK: true, Migrations: []string{"m"}, Backup: needed}); err != nil {
		t.Fatal(err)
	}

	keepNewestUpdateBackup(e.home, keep)
	if exists(oldUp) || exists(oldDeploy) {
		t.Fatal("更早的更新前備份要收掉")
	}
	for _, p := range []string{keep, rescued, needed, other, small} {
		if !exists(p) {
			t.Fatalf("%s 不該刪", filepath.Base(p))
		}
	}

	// 換不回去的紀錄清掉、又有一次整理過資料的更新：只留最新那份
	removeRescue(e.home)
	newer := touch("deploy-0.4.0-0.5.0-20261122-010101.tgz")
	if err := AppendUpgradeHistory(e.home, UpgradeHistory{At: time.Now(), Kind: "upgrade", From: "0.4.0", To: "0.5.0", OK: true, Migrations: []string{"n"}, Backup: newer}); err != nil {
		t.Fatal(err)
	}
	keepNewestUpdateBackup(e.home, newer)
	if exists(needed) || exists(keep) || exists(rescued) || !exists(newer) || !exists(other) {
		t.Fatal("只留最新那份更新前備份")
	}
}
