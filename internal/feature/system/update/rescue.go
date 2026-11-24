package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 換不回去之後的「退回上一版」（DEV-155 S6，system 卡 UPD-14）：升級沒成功、自動換回也沒成功時（結束碼 3），
// 記下要換回哪裡（runtime/upgrade/rescue.json）。主人在看板首頁按「退回上一版」（update_rollback），
// 或秘書跑 `evergenie upgrade --rollback`，就照這份紀錄再試一次換回：程式換回升級前那一版、整理過資料就用升級前的備份還原、
// 沒升成的那一版放進跳過清單、重開、檢查。成功就刪掉這份紀錄；又沒成功就留著，可以再試。

// UpgradeRescueFile 是換不回去時的紀錄（在 UpgradeDir 底下）。
const UpgradeRescueFile = "rescue.json"

// upgradeRescue 是 rescue.json：要換回哪一版、怎麼換。
type upgradeRescue struct {
	From    string `json:"from"`              // 要換回的版本（升級前）
	To      string `json:"to"`                // 沒升成的版本（換回後跳過它）
	Install string `json:"install,omitempty"` // download＝下載版；空＝原始碼版
	Dir     string `json:"dir,omitempty"`     // 下載版：舊版在 versions/ 底下的資料夾名稱
	Commit  string `json:"commit,omitempty"`  // 原始碼版：升級前的 commit
	Backup  string `json:"backup,omitempty"`  // 升級前的備份
	Restore bool   `json:"restore,omitempty"` // 資料整理過：換回時要用備份還原
	// Restored：資料已經用備份還原過了（自動換回時還原成功、之後重開或檢查才沒過；或上一次「退回上一版」還原過了）。
	// 再試一次換回時不再還原：中間這段時間寫的資料不能又被升級前的備份蓋掉。
	Restored bool `json:"restored,omitempty"`
	// Kept：「退回上一版」還原資料之前留的那份現在的家；再試的時候已經有就不再留（留的是還原之前的樣子）。
	Kept string    `json:"kept,omitempty"`
	At   time.Time `json:"at"`
}

func rescuePath(home string) string { return filepath.Join(UpgradeDir(home), UpgradeRescueFile) }

// readRescue 讀 rescue.json；沒有（或讀不懂）回 nil。
func readRescue(home string) *upgradeRescue {
	data, err := os.ReadFile(rescuePath(home))
	if err != nil {
		return nil
	}
	var r upgradeRescue
	if json.Unmarshal(data, &r) != nil || r.From == "" {
		return nil
	}
	return &r
}

func writeRescue(home string, r upgradeRescue) error {
	return platform.WriteJSONFile(rescuePath(home), r)
}

// removeRescue 刪掉 rescue.json（換回成功、或之後升級成功）；刪不掉也不算錯：再試一次換回只會再換一次同一版。
func removeRescue(home string) { _ = os.Remove(rescuePath(home)) }

// stale：這份紀錄已經不用處理了（現在跑的既不是要換回的那一版、也不是沒升成的那一版：之後有人手動修好、又換過版）。
func (r *upgradeRescue) stale(running string) bool { return running != r.From && running != r.To }

// stranded 回「換不回去」的收尾：記下要換回哪裡（rescue.json）、這一版放進跳過清單、寫進度（outcome stranded）、
// 發通知、回結束碼 3。back 帶著換回要用的東西（下載版的舊版資料夾或原始碼版的 commit、備份、要不要還原資料）；
// 收尾時才讀它（呼叫的人在資料還原成功之後設 Restored）。
func (u *Upgrader) stranded(st *UpgradeState, toVersion, reason string, back *upgradeRescue) func(why string) error {
	return func(why string) error {
		r := *back
		r.From, r.To, r.At = u.From, toVersion, u.now()
		if err := writeRescue(u.Home, r); err != nil {
			u.detail("write %s: %v", UpgradeRescueFile, err)
		}
		_ = AddUpgradeSkipped(u.Home, toVersion)
		msg := fmt.Sprintf("升級到 %s 沒成功，而且換不回 %s", toVersion, u.From)
		detail := strings.TrimRight(reason, "。") + "；換回時" + why
		err := u.end(st, UpgradeExitStranded, UpgradePhaseFailed, sdk.UpdateOutcomeStranded, detail, msg+"："+detail, nextRescue)
		u.notify("升級到 "+toVersion+" 沒成功，而且換不回去", sentence(msg+"："+detail, nextRescue)+"。"+u.logNote())
		return err
	}
}

// rescue 照 rescue.json 再試一次換回（鎖已經拿了）。要還原資料時，動任何東西之前先把現在的家留一份（rollback-*.tgz，跟手動退回一樣），
// 還原成功就在 rescue.json 記下 Restored（再試也不會再還原一次）。成功：刪掉 rescue.json、結果記成「已換回」
// （phase rolled_back、outcome rolled_back）、發通知、回 nil；又沒成功：留著 rescue.json、結果還是「換不回去」、結束碼 3。
func (u *Upgrader) rescue(ctx context.Context, r *upgradeRescue) error {
	st := &UpgradeState{Kind: sdk.UpdateKindRescue, From: r.From, To: r.To, StartedAt: u.now()}
	strand := func(why string) error {
		msg := "還是沒能換回 " + r.From
		err := u.end(st, UpgradeExitStranded, UpgradePhaseFailed, sdk.UpdateOutcomeStranded, why, msg+"："+why, nextRescue)
		u.notify(msg, sentence(msg+"："+why, nextRescue)+"。"+u.logNote())
		return err
	}
	// 紀錄寫的東西要先確認能用，動任何東西之前（換版、留一份現在的家都還沒做）：不行就「換不回去」、什麼都不動（UPD-14）。
	broken := func(why string) error {
		msg, reason := "還是沒能換回 "+r.From, why+"，沒有動任何東西"
		err := u.end(st, UpgradeExitStranded, UpgradePhaseFailed, sdk.UpdateOutcomeStranded, reason, msg+"："+reason, nextRescueBroken)
		u.notify(msg, sentence(msg+"："+reason, nextRescueBroken)+"。"+u.logNote())
		return err
	}
	if (r.Install == InstallDownload) != u.Download {
		u.detail("rescue.json install %q, this install download=%v (%s)", r.Install, u.Download, u.Core)
		return broken("程式資料夾的樣子跟升級時不一樣（細節記在升級日誌）")
	}
	restore := r.Restore && !r.Restored
	if why := u.rescueProblem(r, restore); why != "" {
		return broken(why)
	}
	if u.Baseline != nil {
		u.Baseline(ctx)
	}
	kept := r.Kept
	if _, err := os.Stat(kept); restore && (kept == "" || err != nil) {
		// 動任何東西之前，先把現在的家留一份（沒換回的這段時間新增、改過的東西都在裡面）。
		u.setState(st, UpgradePhaseBackup, "正在把現在的資料留一份")
		kept = platform.RuntimeDir(u.Home, "backups", "rollback-"+safeName(r.To)+"-"+safeName(r.From)+"-"+u.now().Format("20060102-150405")+".tgz")
		if err := u.backupHome(kept); err != nil {
			reason := u.explain("留不下換回前的資料備份，沒有動任何東西", err)
			err := u.end(st, UpgradeExitStranded, UpgradePhaseFailed, sdk.UpdateOutcomeStranded, reason, "還是沒能換回 "+r.From+"："+reason, nextRescueDisk)
			u.notify("還是沒能換回 "+r.From, sentence("還是沒能換回 "+r.From+"："+reason, nextRescueDisk)+"。"+u.logNote())
			return err
		}
		r.Kept = kept
		if err := writeRescue(u.Home, *r); err != nil {
			u.detail("write %s: %v", UpgradeRescueFile, err)
		}
	}
	u.setState(st, UpgradePhaseBuild, "正在換回 "+r.From)
	if u.Download {
		// 固定位置的檔（UPD-15）先對成要換回的那一版：內容一樣的不動（主人不用再按一次允許）；放不了不擋，原因只進升級日誌。
		if err := u.switchCurrentFixed(r.Dir); err != nil {
			return strand(u.explain("換不回舊版的資料夾", err))
		}
	} else {
		if _, err := u.git(ctx, 60*time.Second, "checkout", "--quiet", "--detach", r.Commit); err != nil {
			return strand(u.explain("程式回不到升級前的版本", err))
		}
		if err := u.binBack(ctx, r.From); err != nil {
			return strand(u.explain("換不回舊版的執行檔", err))
		}
	}
	if restore {
		u.setState(st, UpgradePhaseBackup, "正在把資料還原到升級前")
		if err := u.restoreHome(r.Backup, false); err != nil {
			return strand(u.explain("還原備份失敗（換回前的資料在 "+kept+"）", err))
		}
		r.Restored = true
		if err := writeRescue(u.Home, *r); err != nil {
			u.detail("write %s: %v", UpgradeRescueFile, err)
		}
	}
	_ = AddUpgradeSkipped(u.Home, r.To)
	u.setState(st, UpgradePhaseRestart, "正在重開")
	if err := u.restart(ctx); err != nil {
		return strand(u.explain("重開舊版失敗", err))
	}
	u.setState(st, UpgradePhaseVerify, "正在檢查舊版有沒有正常")
	if u.Verify != nil {
		if err := u.Verify(ctx, r.From); err != nil {
			return strand(u.explain("舊版重開後檢查沒過", err))
		}
	}
	removeRescue(u.Home)
	st.Outcome, st.Restored, st.Next, st.Log = sdk.UpdateOutcomeRolledBack, r.Restore, nextSkipped, u.logPath()
	msg := fmt.Sprintf("已換回 %s；%s 這一版先跳過", r.From, r.To)
	u.setState(st, UpgradePhaseRolledBack, msg)
	note := "換回 " + r.From + " 了，" + r.To + " 這一版先跳過，已經請秘書查原因。"
	if r.Restore {
		note += "資料也回到升級前了。"
	}
	if restore {
		note += "換回前的資料另外留了一份在 " + kept + "，要找回來請秘書幫忙。"
	}
	u.notify("已換回 "+r.From, note+u.logNote())
	if u.Report != nil {
		u.Report(ctx, UpgradeReport{From: r.From, To: r.To, Reason: "升級沒成功、自動換回也沒成功，後來再試一次才換回", Log: u.logPath()})
	}
	return nil
}

// gitCommit 是 rescue.json 收的 commit：完整的 40 碼十六進位（不收分支名、tag 或 git 的選項）。
var gitCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// rescueProblem 檢查 rescue.json 寫的東西能不能用（rescue.json 是讀進來的檔，不能照單全收）；能用回空字串，不能用回白話原因
// （英文細節進升級日誌）。原始碼版的 commit 要是 40 碼十六進位；下載版的舊版資料夾名稱要合法、而且還在；
// 要還原資料時，備份要在這個家的備份資料夾（runtime/backups/）底下、而且整份讀得開。
func (u *Upgrader) rescueProblem(r *upgradeRescue, restore bool) string {
	if u.Download {
		if !safeVersionName(r.Dir) {
			u.detail("rescue.json dir %q is not a version folder name", r.Dir)
			return "換不回去的紀錄寫的舊版資料夾不對"
		}
		if fi, err := os.Stat(filepath.Join(u.Core, versionsDirName, r.Dir)); err != nil || !fi.IsDir() {
			u.detail("rescue.json dir %s: %v", r.Dir, err)
			return "舊版 " + r.From + " 的程式資料夾已經不在了"
		}
	} else if !gitCommit.MatchString(r.Commit) {
		u.detail("rescue.json commit %q is not a 40-hex commit", r.Commit)
		return "換不回去的紀錄寫的程式版本不對"
	}
	if !restore {
		return ""
	}
	if !u.inBackups(r.Backup) {
		u.detail("rescue.json backup %q is not under %s", r.Backup, platform.RuntimeDir(u.Home, "backups"))
		return "換不回去的紀錄寫的備份不在這個家的備份資料夾裡"
	}
	if err := readableBackup(r.Backup); err != nil {
		u.detail("rescue.json backup %s: %v", r.Backup, err)
		return "升級前的備份不見了或讀不開，資料沒辦法回到升級前"
	}
	return ""
}

// inBackups：path 是這個家的備份資料夾（runtime/backups/）底下的檔（解開符號連結之後也是）。
func (u *Upgrader) inBackups(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	root, err := filepath.EvalSymlinks(platform.RuntimeDir(u.Home, "backups"))
	if err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) { // 檔不在（或是斷掉的連結）：看它的資料夾；之後讀的時候會說備份不見了
		dir, derr := filepath.EvalSymlinks(filepath.Dir(path))
		if derr != nil {
			return false
		}
		real, err = filepath.Join(dir, filepath.Base(path)), nil
	}
	if err != nil {
		return false
	}
	return strings.HasPrefix(real, root+string(filepath.Separator))
}

// readableBackup 把備份（tar.gz）整份讀一遍：打得開、解得開、每個檔都讀得完才算數。
func readableBackup(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		if _, err := tr.Next(); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return err
		}
	}
}

// RescueBackups 回這個家「換不回去的紀錄」（rescue.json）還要用的備份（升級前的備份、換回前留的那份；絕對路徑）。
// 整理備份（settings 的 backup prune）不能刪它們（UPD-14）。沒有紀錄回 nil。
func RescueBackups(home string) []string {
	r := readRescue(home)
	if r == nil {
		return nil
	}
	var out []string
	for _, p := range []string{r.Backup, r.Kept} {
		if p != "" {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

// binBack（原始碼版）讓 bin/ 變回 want 那一版：已經是就不動；bin.prev/ 是那一版就換回來；都不是就照現在的 commit 重新編。
func (u *Upgrader) binBack(ctx context.Context, want string) error {
	version := func(dir string) string {
		out, code := u.run(ctx, time.Minute, filepath.Join(u.Core, dir, platform.CoreExe), "version")
		if code != 0 {
			return ""
		}
		return strings.TrimSpace(out)
	}
	if version("bin") == want {
		return nil
	}
	if version("bin.prev") == want {
		return u.swapBack()
	}
	return u.build(ctx)
}
