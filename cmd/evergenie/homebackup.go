package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system"
	"github.com/minglight/evergenie/internal/feature/system/homebackup"
	"github.com/minglight/evergenie/internal/feature/system/update"
)

// 整個家的還原在 serve 這裡接兩個點（DEV-153，說明卡 ai_docs/modules/system.md 的 HBK-5、HBK-6）：
// 啟動時、還沒開任何模組前，把準備好的還原換上去；收工時如果是為了還原要重開，就原地重開。

// verifyRestored 是備份換上去之後的檢查：家設定讀得懂、資料整理跑得完。回錯＝這份備份在這台跑不起來，會全部換回去。
// 資料整理照法律 3.1 先在複本上預演（update.PreviewMigrations，真的家一個檔都不動），預演過了才在換上來的家裡真的跑：
// 舊備份常常有還沒跑過的整理，沒過就不讓它在家裡跑到一半。資料整理只准改 home.toml、settings/、vault/
// （update.HomeMigration 的規矩），都在還原換上的那幾塊裡，真的跑到一半失敗時跟著一起換回去（HBK-6）。
func verifyRestored(home string) error {
	if _, err := platform.LoadHome(home); err != nil {
		return err
	}
	if _, err := update.PreviewMigrations(home); err != nil {
		return err
	}
	_, err := update.RunHomeMigrations(home)
	return err
}

// applyPendingRestore 把等著的還原換上去；回 false＝家的狀態不確定（連換回去都失敗），serve 不要啟動。
func applyPendingRestore(home string, out, errOut io.Writer) bool {
	res, err := homebackup.ApplyPending(homebackup.ApplyOptions{Home: home, Verify: verifyRestored})
	if err != nil {
		fmt.Fprintln(errOut, "serve 拒絕啟動："+err.Error())
		return false
	}
	if res != nil {
		fmt.Fprintf(out, "restore ok=%v\n", res.OK)
	}
	return true
}

// restartForRestore：這次收工是還原要求的，原地換成一個新的自己（同一個行程編號，launchd 不會當成掛掉）。
// 換不了就回 false，由呼叫的人用非零結束碼收工，讓 launchd 重開。
func restartForRestore(errOut io.Writer) bool {
	exe, err := os.Executable()
	if err == nil {
		err = syscall.Exec(exe, os.Args, os.Environ())
	}
	fmt.Fprintln(errOut, "還原後重開服務沒成功：", err)
	return false
}

// ---- 從備份建家（evergenie init --from-backup，HBK-8）----

// checkBackupFile 在建家之前先確認備份檔讀得懂、這個程式吃得下；回絕對路徑。
func checkBackupFile(path string) (string, error) {
	abs, err := filepath.Abs(platform.ExpandHome(path))
	if err != nil {
		return "", platform.Invalid("from-backup", "備份檔的位置不對："+path)
	}
	m, err := homebackup.Inspect(abs)
	if err != nil {
		return "", err
	}
	if why := homebackup.Problem(m, system.KnownMigrations()); why != "" {
		return "", platform.Conflict(why)
	}
	return abs, nil
}

// homeFromBackup 把備份放進剛建好的空家：綁機器的設定（看板的埠、狀態資料夾、秘密檔位置…）用新家的，其他全部用備份的。
func homeFromBackup(home, archive string) ([]string, error) {
	// Stage 會先整份讀一遍看空間夠不夠（checkBackupFile 也讀過一次；家還沒建時不知道要放哪顆磁碟，所以分兩次）。
	if _, err := homebackup.Stage(homebackup.StageOptions{Home: home, Archive: archive, NewHome: true, KnownMigrations: system.KnownMigrations()}); err != nil {
		return nil, err
	}
	res, err := homebackup.ApplyPending(homebackup.ApplyOptions{Home: home, Verify: verifyRestored})
	if err != nil {
		return nil, err
	}
	if res == nil || !res.OK {
		return nil, platform.Unavailable("備份放不進新家：這份備份在這台電腦上跑不起來")
	}
	return res.Todo, nil
}

// finishHomeFromBackup 是 init --from-backup 建好空家之後的那一段：放備份、印結果；放不進去就把剛建的東西收掉
// （跟 init 其他失敗一樣不留半個家）。只刪這次才建的（比照 createHome）：家或狀態資料夾建家之前就在（createHome 只接受空的），
// 就只清裡面、資料夾本身留著。
func finishHomeFromBackup(r cmdline.HostRequest, home, archive, note string, homeWasThere, stateWasThere bool) int {
	state := platform.LayoutFor(home).State
	todo, err := homeFromBackup(home, archive)
	if err != nil {
		removeBuilt(home, homeWasThere)
		if state != "" {
			removeBuilt(state, stateWasThere)
		}
		return fail(r.Err, err)
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return fail(r.Err, err)
	}
	fmt.Fprintf(r.Out, "家建好了（用備份）：%s\n看板網址：http://127.0.0.1:%d/\n", home, h.Web.Port)
	if note != "" {
		fmt.Fprintln(r.Out, note)
	}
	fmt.Fprintln(r.Out, "你的筆記、設定、角色、附件都搬過來了。換了電腦，還有幾件事要你自己做：")
	for _, t := range todo {
		fmt.Fprintln(r.Out, "注意："+t)
	}
	fmt.Fprintf(r.Out, "下一步：evergenie service install --home %s（裝成服務），再打開看板網址。\n", home)
	return 0
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// plannedState 是 createHome 會用的狀態資料夾（同一個算法：有給 --state 用它，沒給照佈局的預設）。
func plannedState(o initOptions) string {
	state := strings.TrimSpace(o.State)
	if state != "" {
		state = platform.ExpandHome(state)
	}
	return platform.NewLayout(o.Home, platform.LayoutConfig{Version: 2, State: state}).State
}

// removeBuilt 收掉這次建的東西：資料夾是這次才建的整個刪；本來就在的只清裡面（收不掉也只是留下空資料夾或幾個檔）。
func removeBuilt(dir string, wasThere bool) {
	if !wasThere {
		_ = os.RemoveAll(dir) // 這次才建的
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(dir, e.Name())) // 本來是空的資料夾：裡面都是這次放的
	}
}
