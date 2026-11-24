package platform

import "testing"

// 版號的各種寫法都認得，短碼與 dev 認不出；String 還原成同一個樣子。
// Guards: REQ-277
func TestParseVersion(t *testing.T) {
	good := []struct {
		in      string
		want    CoreVersion
		out     string
		release bool
	}{
		{"0.2.0", CoreVersion{Major: 0, Minor: 2, Patch: 0}, "0.2.0", true},
		{"v0.2.0", CoreVersion{Minor: 2}, "0.2.0", true},
		{"1.10.3", CoreVersion{Major: 1, Minor: 10, Patch: 3}, "1.10.3", true},
		{"0.2.0-3-gabc1234", CoreVersion{Minor: 2, Ahead: 3, Commit: "abc1234"}, "0.2.0-3-gabc1234", false},
		{"0.2.0-3-gabc1234-dirty", CoreVersion{Minor: 2, Ahead: 3, Commit: "abc1234", Dirty: true}, "0.2.0-3-gabc1234-dirty", false},
		{"0.2.0-dirty", CoreVersion{Minor: 2, Dirty: true}, "0.2.0-dirty", false},
		{"0.2.0-rc1", CoreVersion{Minor: 2, Pre: "rc1"}, "0.2.0-rc1", false},
		{"0.2.0-rc.1-2-gdeadbee", CoreVersion{Minor: 2, Pre: "rc.1", Ahead: 2, Commit: "deadbee"}, "0.2.0-rc.1-2-gdeadbee", false},
	}
	for _, c := range good {
		got, ok := ParseVersion(c.in)
		if !ok || got != c.want {
			t.Fatalf("%q → %+v ok=%v，想要 %+v", c.in, got, ok, c.want)
		}
		if got.String() != c.out || got.Release() != c.release {
			t.Fatalf("%q：String=%q Release=%v，想要 %q %v", c.in, got.String(), got.Release(), c.out, c.release)
		}
	}
	for _, in := range []string{"", "dev", "abc1234", "abc1234-dirty", "0.2", "1.2.x", "v", "0.2.0-bad tag", "0.2.0-"} {
		if v, ok := ParseVersion(in); ok {
			t.Fatalf("%q 不該認得，卻得到 %+v", in, v)
		}
	}
}

// 比較：先數字、預發布比正式版小、tag 之後的 commit 越多越新；Commit 與 Dirty 不參與。
// Guards: REQ-277
func TestVersionCompare(t *testing.T) {
	p := func(s string) CoreVersion {
		v, ok := ParseVersion(s)
		if !ok {
			t.Fatalf("%q 認不出", s)
		}
		return v
	}
	order := []string{"0.1.9", "0.2.0-rc1", "0.2.0-rc2", "0.2.0", "0.2.0-1-gaaaaaaa", "0.2.0-3-gbbbbbbb", "0.2.1", "0.10.0", "1.0.0"}
	for i, a := range order {
		for j, b := range order {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := p(a).Compare(p(b)); got != want {
				t.Fatalf("Compare(%s, %s) = %d，想要 %d", a, b, got, want)
			}
		}
	}
	if p("0.2.0-3-gabc1234").Compare(p("0.2.0-3-gabc1234-dirty")) != 0 || p("0.2.0").Compare(p("v0.2.0")) != 0 {
		t.Fatal("Dirty 與開頭的 v 不該影響比較")
	}
}
