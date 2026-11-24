package backupsweep

import (
	"regexp"
	"time"
)

// 備份的命名規則（備份規則的「命名」，system 卡 BSW-4）：<原名>.bak-<YYYYMMDD>[-<HHMM>]-<原因>[-until<YYYYMMDD>]，
// 原因是小寫英文、數字與連字號；預設留到日期後 30 天，有 -until 就留到那天。
// 原因裡可以有版本號的點（v0.1.0）；打包起來的備份最後可以帶副檔名（例 .tgz、.tar.gz，在 -until 之後）。

// KeepDays 是沒寫 -until 時，日期之後留幾天。
const KeepDays = 30

var (
	bakName   = regexp.MustCompile(`^(.+)\.bak-(\d{8})(?:-(\d{4}))?-([a-z0-9][a-z0-9.-]*?)(?:-until(\d{8}))?(?:\.[A-Za-z0-9]{1,6}){0,3}$`)
	hasLetter = regexp.MustCompile(`[a-z]`)
	bakDate   = regexp.MustCompile(`\.bak-\d{8}`)
	anyDate   = regexp.MustCompile(`(?:^|\D)(20\d{6})(?:\D|$)`)
	// bakTail 是名字裡「備份」那一段與後面的東西，拿掉之後猜原名。
	bakTail = regexp.MustCompile(`(?i)[._-]*(\.bak|bak|backups?)([._-].*)?$`)
)

var (
	// bakMark：「.bak」後面不是英文字母（.bak、.bak-…、.bak2、.bak.tgz；不含 .bakery 這種剛好同字首的）。
	bakMark = regexp.MustCompile(`(?i)\.bak(?:[^a-z]|$)`)
	// backupDated：「<原名>-backup-<日期>」這種照日期留的複本（前面要有原名；日期是 YYYYMMDD 或 YYYY-MM-DD）。
	backupDated = regexp.MustCompile(`(?i).[._-]backups?[._-]?(?:20\d{6}|20\d{2}-\d{2}-\d{2})(?:\D|$)`)
	// backupExt：結尾是 .backup（後面可以有數字與打包的副檔名，例 .backup2、.backup.1、.backup.tgz）。
	backupExt = regexp.MustCompile(`(?i).\.backups?(?:[._-]?\d+)*(?:\.(?:tar|tgz|gz|zip|bz2|xz|7z))*$`)
	// logFile：紀錄檔（備份程式每天寫的 xxx-backup-<日期>.log）不是備份。
	logFile = regexp.MustCompile(`(?i)\.log(?:\.\d+)?$`)
)

// looksLikeBackup：名字看起來是「某個東西的複本」才要看（不分大小寫）：有 .bak、結尾是 .backup、或「<原名>-backup-<日期>」。
// 名字裡只是剛好有 backup 這個字的不算（backup-list.txt、icloud-backup.log、放備份的資料夾 backups/、程式裡的 FeedbackUpload…）：
// 那些是程式、紀錄或容器，不是誰留下來的舊檔，列出來只會叫人去改不該改的名字。
func looksLikeBackup(name string) bool {
	return bakMark.MatchString(name) || backupExt.MatchString(name) || (backupDated.MatchString(name) && !logFile.MatchString(name))
}

// parsedName 是合規名字拆出來的東西。
type parsedName struct {
	date    time.Time // 名字裡的日期（loc 的 0 點）
	expires time.Time // 留到哪一天（loc 的 0 點；這一天過完才算到期）
}

// parseName 照命名規則拆名字；不合規時第二個回傳值寫哪裡不對，合規是空字串。
func parseName(name string, loc *time.Location) (parsedName, string) {
	m := bakName.FindStringSubmatch(name)
	if m == nil {
		if !bakMark.MatchString(name) {
			return parsedName{}, "名字沒照「<原名>.bak-<日期>-<原因>」寫"
		}
		if bakDate.MatchString(name) {
			return parsedName{}, "日期後面沒寫原因（原因用小寫英文與連字號）"
		}
		return parsedName{}, "沒有日期（要寫成 .bak-YYYYMMDD-<原因>）"
	}
	date, err := time.ParseInLocation("20060102", m[2], loc)
	if err != nil {
		return parsedName{}, "日期不對：" + m[2]
	}
	if hhmm := m[3]; hhmm != "" {
		if _, err := time.Parse("1504", hhmm); err != nil {
			return parsedName{}, "時間不對：" + hhmm
		}
	}
	if !hasLetter.MatchString(m[4]) {
		return parsedName{}, "日期後面沒寫原因（原因用小寫英文與連字號）"
	}
	out := parsedName{date: date, expires: date.AddDate(0, 0, KeepDays)}
	if m[5] != "" {
		until, err := time.ParseInLocation("20060102", m[5], loc)
		if err != nil || until.Before(date) {
			return parsedName{}, "-until 的日期不對：" + m[5]
		}
		out.expires = until
	}
	return out, ""
}

// expired：今天（loc）已經過了留到的那一天。
func (p parsedName) expired(now time.Time, loc *time.Location) bool {
	y, mo, d := now.In(loc).Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, loc)
	return today.After(p.expires)
}

// suggestName 給不合規的名字一個建議的新名字：原名照猜、日期用名字裡的日期（沒有就用最後修改日）、原因留給人填。
func suggestName(name string, mod time.Time, loc *time.Location) string {
	orig := bakTail.ReplaceAllString(name, "")
	if orig == "" || orig == name {
		orig = name
	}
	date := mod.In(loc).Format("20060102")
	if m := anyDate.FindStringSubmatch(name); m != nil {
		if _, err := time.ParseInLocation("20060102", m[1], loc); err == nil {
			date = m[1]
		}
	}
	return orig + ".bak-" + date + "-<原因>"
}
