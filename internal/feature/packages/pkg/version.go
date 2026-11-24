package pkg

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type semver struct {
	major, minor, patch int
	pre                 string
}

func parseSemver(s string) (semver, bool) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return semver{}, false
	}
	n := func(x string) int { v, _ := strconv.Atoi(x); return v }
	return semver{n(m[1]), n(m[2]), n(m[3]), strings.TrimPrefix(m[4], "-")}, true
}

// compare 回傳 -1、0、1；沒有預發行標記的版本比有的大。
func (a semver) compare(b semver) int {
	for _, p := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	case a.pre < b.pre:
		return -1
	}
	return 1
}

// constraint 是 requires[].version 的寫法：空白＝任何版本；">=1.2.0"＝至少；"1.2.0" 或 "^1.2.0"＝同一個大版本、至少這個版本。
type constraint struct {
	min      semver
	any      bool
	sameMajr bool
}

func parseConstraint(s string) (constraint, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return constraint{any: true}, nil
	}
	c := constraint{sameMajr: true}
	switch {
	case strings.HasPrefix(s, ">="):
		c.sameMajr = false
		s = strings.TrimSpace(s[2:])
	case strings.HasPrefix(s, "^"):
		s = s[1:]
	}
	v, ok := parseSemver(s)
	if !ok {
		return c, fmt.Errorf("bad version %q", s)
	}
	c.min = v
	return c, nil
}

func (c constraint) allows(version string) bool {
	if c.any {
		return true
	}
	v, ok := parseSemver(version)
	if !ok || v.compare(c.min) < 0 {
		return false
	}
	return !c.sameMajr || v.major == c.min.major
}

func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

// describePattern 認得核心版本字串：`0.2.0`、`v0.2.0`、`0.2.0-3-gabc1234`、`0.2.0-3-gabc1234-dirty`（git describe 的樣子）。
var describePattern = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)(?:-(?:\d+-g[0-9a-f]+(?:-dirty)?|[0-9A-Za-z.-]+))?$`)

// coreSemver 把核心的版本字串變成 semver 來比較版本限制：`0.2.0-3-gabc1234` 是 0.2.0 之後的開發版，當作 0.2.0；
// 只有短碼或 `dev` 的開發版認不出版本，回 false（呼叫者不擋，只警告）。
func coreSemver(s string) (semver, bool) {
	m := describePattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return semver{}, false
	}
	v, ok := parseSemver(m[1])
	if !ok {
		return semver{}, false
	}
	// 真的預發行標記（0.2.0-rc1）才算預發行；git describe 的 -N-g<sha> 不算。
	rest := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "v"), m[1])
	if rest != "" && !regexp.MustCompile(`^-\d+-g[0-9a-f]+(-dirty)?$`).MatchString(rest) {
		v.pre = strings.TrimPrefix(rest, "-")
	}
	return v, true
}

// satisfiesCore：核心版本字串符不符合限制（空限制＝符合）。認不出核心版本時 known 是 false，ok 是 true（不擋）。
func satisfiesCore(limit, coreVersion string) (ok, known bool) {
	c, err := parseConstraint(limit)
	if err != nil || c.any {
		return true, true
	}
	v, parsed := coreSemver(coreVersion)
	if !parsed {
		return true, false
	}
	if v.compare(c.min) < 0 || (c.sameMajr && v.major != c.min.major) {
		return false, true
	}
	return true, true
}
