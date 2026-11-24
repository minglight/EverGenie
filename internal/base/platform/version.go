package platform

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CoreVersion 是核心的版本號（REQ-277）：從 git tag `vX.Y.Z` 來，由 scripts/version.sh 產生字串、make build 用 -ldflags 填進 BuildVersion。
//
//	0.2.0                    正式版（tag 本身）
//	0.2.0-3-gabc1234         tag 之後又有 3 個 commit（開發版）
//	0.2.0-3-gabc1234-dirty   同上，還有沒提交的改動
//	0.2.0-rc1                有預發布標記的 tag（少用）
//
// 短碼（還沒有任何 v tag）、`dev` 認不出來：ParseVersion 回 false。
// 套件的版本限制用 internal/feature/packages 的 semver（那邊是範圍比較，這裡是「這個核心是第幾版」），兩邊不要合併。
// （型別叫 CoreVersion 不叫 Version：platform.Version() 這個函式早就有了，回傳目前執行檔的版本字串。）
type CoreVersion struct {
	Major, Minor, Patch int
	Pre                 string // 預發布標記（0.2.0-rc1 的 rc1）；正式版是空的
	Ahead               int    // tag 之後又有幾個 commit；剛好在 tag 上是 0
	Commit              string // git describe 的 g<短碼>（去掉 g）；在 tag 上是空的
	Dirty               bool   // 有沒提交的改動
}

var (
	versionCore   = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-(.+))?$`)
	versionAhead  = regexp.MustCompile(`^(?:(.*)-)?(\d+)-g([0-9a-f]{4,40})$`)
	versionPreTag = regexp.MustCompile(`^[0-9A-Za-z.]+$`)
)

// ParseVersion 認 `0.2.0`、`v0.2.0`、`0.2.0-3-gabc1234`、`0.2.0-3-gabc1234-dirty`、`0.2.0-rc1`；認不出（短碼、dev、空字串）回 (零值, false)。
func ParseVersion(s string) (CoreVersion, bool) {
	s = strings.TrimSpace(s)
	var v CoreVersion
	if rest, ok := strings.CutSuffix(s, "-dirty"); ok {
		v.Dirty, s = true, rest
	}
	m := versionCore.FindStringSubmatch(s)
	if m == nil {
		return CoreVersion{}, false
	}
	var err error
	if v.Major, err = strconv.Atoi(m[1]); err != nil {
		return CoreVersion{}, false
	}
	if v.Minor, err = strconv.Atoi(m[2]); err != nil {
		return CoreVersion{}, false
	}
	if v.Patch, err = strconv.Atoi(m[3]); err != nil {
		return CoreVersion{}, false
	}
	if rest := m[4]; rest != "" {
		if a := versionAhead.FindStringSubmatch(rest); a != nil {
			v.Pre = a[1]
			v.Ahead, _ = strconv.Atoi(a[2])
			v.Commit = a[3]
		} else {
			v.Pre = rest
		}
		if v.Pre != "" && !versionPreTag.MatchString(v.Pre) {
			return CoreVersion{}, false
		}
	}
	return v, true
}

// Compare 比較兩個版本：-1（小於）、0、1。依序比 Major、Minor、Patch；同一組數字時，有 Pre 的比沒有的小（0.2.0-rc1 < 0.2.0），
// 兩個都有 Pre 就比字串；最後比 Ahead（tag 之後的 commit 越多越新）。Commit 與 Dirty 不參與。
func (v CoreVersion) Compare(o CoreVersion) int {
	for _, p := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case v.Pre == o.Pre:
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	case v.Pre < o.Pre:
		return -1
	default:
		return 1
	}
	switch {
	case v.Ahead < o.Ahead:
		return -1
	case v.Ahead > o.Ahead:
		return 1
	}
	return 0
}

// Release 是不是正式版：沒有 Pre、剛好在 tag 上（Ahead == 0）、沒有未提交的改動。
func (v CoreVersion) Release() bool { return v.Pre == "" && v.Ahead == 0 && !v.Dirty }

// Core 是 "X.Y.Z"（只有數字那一段）。
func (v CoreVersion) Core() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// String 還原成 scripts/version.sh 印出的樣子（沒有開頭的 v）。
func (v CoreVersion) String() string {
	s := v.Core()
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	if v.Ahead > 0 {
		s += fmt.Sprintf("-%d-g%s", v.Ahead, v.Commit)
	}
	if v.Dirty {
		s += "-dirty"
	}
	return s
}
