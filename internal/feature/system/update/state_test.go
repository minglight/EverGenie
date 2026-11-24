package update

import (
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 今晚更新：記下下一個 tonight_at（已經過了就明天）。
// Guards: REQ-280
func TestDecideTonight(t *testing.T) {
	h, err := platform.LoadHome(testHome(t))
	if err != nil {
		t.Fatal(err)
	}
	loc := h.Location()
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, loc)
	at, err := DecideTonight(h, "0.2.0", now)
	if err != nil || at != time.Date(2026, 10, 30, 3, 30, 0, 0, loc) {
		t.Fatalf("%v %v", at, err)
	}
	d, _ := ReadUpgradeDecision(h.Dir)
	if d == nil || d.Version != "0.2.0" || d.Decision != "tonight" || !d.At.Equal(at) {
		t.Fatalf("%+v", d)
	}
	early := time.Date(2026, 10, 29, 1, 0, 0, 0, loc)
	if at, _ = DecideTonight(h, "0.2.0", early); at != time.Date(2026, 10, 29, 3, 30, 0, 0, loc) {
		t.Fatalf("凌晨一點要當天 3:30：%v", at)
	}
}
