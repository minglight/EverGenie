package rules

import (
	"testing"
	"time"
)

// 時段：HH:MM-HH:MM，結尾可以 24:00，可以跨午夜；開頭算、結尾不算；頭尾一樣或格式錯都不收。
func TestVariantWindowParseAndContains(t *testing.T) {
	night, err := ParseWindow("22:00-02:00")
	if err != nil {
		t.Fatal(err)
	}
	for m, want := range map[int]bool{22 * 60: true, 23*60 + 59: true, 0: true, 1*60 + 59: true, 2 * 60: false, 12 * 60: false, 21*60 + 59: false} {
		if night.Contains(m) != want {
			t.Errorf("22:00-02:00 在 %d 分：要 %v", m, want)
		}
	}
	day, err := ParseWindow(" 09:30 - 24:00 ")
	if err != nil {
		t.Fatal(err)
	}
	if !day.Contains(23*60+59) || day.Contains(9*60+29) || !day.Contains(9*60+30) {
		t.Errorf("09:30-24:00：%+v", day)
	}
	for _, bad := range []string{"", "22:00", "9:00-10:00", "24:00-02:00", "10:60-11:00", "10:00-10:00", "00:00-24:00", "aa:bb-cc:dd", "10:00-24:30"} {
		if _, err := ParseWindow(bad); err == nil {
			t.Errorf("「%s」不該收", bad)
		}
	}
}

// 資料夾只能是 personas/ 底下的相對路徑：不能絕對路徑、不能有 . 開頭的段（含 ..）、不能反斜線或空段。
func TestVariantDirStaysUnderPersonas(t *testing.T) {
	for _, ok := range []string{"avatars/hidden", "hidden", "avatars/隱藏/甲"} {
		if !ValidVariantDir(ok) {
			t.Errorf("「%s」要收", ok)
		}
	}
	for _, bad := range []string{"", "/etc", "../vault", "avatars/../../vault", "avatars/.secret", ".", "a//b", `a\b`, "a/"} {
		if ValidVariantDir(bad) {
			t.Errorf("「%s」不該收", bad)
		}
	}
}

// 欄位轉換：名稱、資料夾必填，時段錯一個整筆不收；開著＝開關開或在時段裡。
func TestVariantFromFieldsAndActive(t *testing.T) {
	v, err := VariantFromFields("hidden", map[string]any{"name": "隱藏版", "dir": "avatars/hidden", "windows": []any{"22:00-02:00"}, "button": true, "order": 2})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Button || v.On || !v.HasOrder || v.Order != 2 || len(v.Windows) != 1 {
		t.Fatalf("%+v", v)
	}
	at := func(h, m int) time.Time { return time.Date(2026, 11, 4, h, m, 0, 0, time.Local) }
	if !v.Active(at(23, 0)) || v.Active(at(12, 0)) {
		t.Error("時段內開、時段外關")
	}
	v.On = true
	if !v.Active(at(12, 0)) {
		t.Error("開關開著時段外也開")
	}
	for name, f := range map[string]map[string]any{
		"沒名稱":   {"dir": "a"},
		"沒資料夾":  {"name": "x"},
		"資料夾越界": {"name": "x", "dir": "../vault"},
		"時段壞":   {"name": "x", "dir": "a", "windows": []any{"22:00-02:00", "晚上"}},
	} {
		if _, err := VariantFromFields("hidden", f); err == nil {
			t.Errorf("%s 不該收", name)
		}
	}
	if _, err := VariantFromFields("Bad_ID", map[string]any{"name": "x", "dir": "a"}); err == nil {
		t.Error("編號不合格不該收")
	}
}

// 排序：有 order 的照 order，沒寫的排最後再照編號。
func TestSortVariants(t *testing.T) {
	list := []Variant{{ID: "c"}, {ID: "b", Order: 5, HasOrder: true}, {ID: "a"}, {ID: "d", Order: 1, HasOrder: true}}
	SortVariants(list)
	got := ""
	for _, v := range list {
		got += v.ID
	}
	if got != "dbac" {
		t.Fatalf("順序 %s", got)
	}
}

// 下一個時段邊界：今天還沒到的開頭或結尾；今天都過了就是明天的；沒有時段回零值。
func TestNextBoundary(t *testing.T) {
	w1, _ := ParseWindow("22:00-02:00")
	w2, _ := ParseWindow("12:15-13:00")
	list := []Variant{{ID: "a", Windows: []Window{w1}}, {ID: "b", Windows: []Window{w2}}}
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, time.Local) }
	for now, want := range map[time.Time]time.Time{
		at(4, 10, 0):  at(4, 12, 15),
		at(4, 12, 15): at(4, 13, 0),
		at(4, 13, 0):  at(4, 22, 0),
		at(4, 23, 0):  at(5, 2, 0),
		at(5, 1, 0):   at(5, 2, 0),
	} {
		if got := NextBoundary(list, now); !got.Equal(want) {
			t.Errorf("%v 之後：%v，要 %v", now, got, want)
		}
	}
	if !NextBoundary([]Variant{{ID: "x"}}, at(4, 10, 0)).IsZero() {
		t.Error("沒有時段要回零值")
	}
}
