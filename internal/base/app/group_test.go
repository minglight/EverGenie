package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 塊放進 App：塊內依序啟動、反序停止，整個塊在 App 的清單裡只佔一個位置。
func TestGroupStartsInOrderStopsInReverseInsideApp(t *testing.T) {
	l := &eventLog{}
	g := NewGroup("blk", mods(l, "x", "y", "z")...)
	a := New(&fake{name: "base", log: l}, g, &fake{name: "after", log: l})
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "start:base,start:x,start:y,start:z,start:after,stop:after,stop:z,stop:y,stop:x,stop:base"
	if got := l.get(); got != want {
		t.Fatalf("順序 %s，要 %s", got, want)
	}
	if g.Name() != "blk" || len(g.Modules()) != 3 {
		t.Fatalf("名稱與子模組：%s %d", g.Name(), len(g.Modules()))
	}
}

// 某個子模組啟動失敗：已啟動的反序停掉，錯誤講出是哪個子模組，失敗的與後面的都不動；App 也不再啟動後面的模組。
func TestGroupStartFailureRollsBackStartedSubmodules(t *testing.T) {
	l := &eventLog{}
	ms := mods(l, "x", "y", "z")
	ms[1].(*fake).startErr = errors.New("壞了")
	a := New(NewGroup("blk", ms...), &fake{name: "after", log: l})
	err := a.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "啟動 blk 失敗：y：壞了") {
		t.Fatalf("錯誤要講出塊與子模組：%v", err)
	}
	if got := l.get(); got != "start:x,start:y,stop:x" {
		t.Fatalf("回滾順序：%s", got)
	}
}

// Stop：一個子模組失敗或卡住不影響其他的，錯誤一起回；重複 Stop 不再停。
func TestGroupStopContinuesAfterFailureAndTimeout(t *testing.T) {
	l := &eventLog{}
	ms := mods(l, "x", "y", "z")
	ms[1].(*fake).stopErr = errors.New("停不了")
	hang := ms[2].(*fake)
	hang.stopHang, hang.release = true, make(chan struct{})
	defer close(hang.release)
	g := NewGroup("blk", ms...)
	g.stopTimeout = 20 * time.Millisecond
	if err := g.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := g.Stop(context.Background())
	if err == nil || !strings.Contains(err.Error(), "停止 z 逾時") || !strings.Contains(err.Error(), "停不了") {
		t.Fatalf("錯誤要收齊：%v", err)
	}
	if got := l.get(); got != "start:x,start:y,start:z,stop:z,stop:y,stop:x" {
		t.Fatalf("停止順序：%s", got)
	}
	if err := g.Stop(context.Background()); err != nil {
		t.Fatalf("停過再停什麼都不做：%v", err)
	}
}

// 重複 Start、壞的子模組清單（nil、同名）一個都不啟動就回錯。
func TestGroupRejectsDoubleStartAndBadList(t *testing.T) {
	l := &eventLog{}
	g := NewGroup("blk", mods(l, "x")...)
	if err := g.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := g.Start(context.Background()); err == nil {
		t.Fatal("重複啟動要回錯")
	}
	_ = g.Stop(context.Background())
	l2 := &eventLog{}
	if err := NewGroup("blk", &fake{name: "a", log: l2}, &fake{name: "a", log: l2}).Start(context.Background()); err == nil || l2.get() != "" {
		t.Fatalf("同名要擋、不啟動：%v %s", err, l2.get())
	}
	if err := NewGroup("blk", nil).Start(context.Background()); err == nil {
		t.Fatal("nil 子模組要擋")
	}
}

type homeFake struct {
	fake
	home string
}

func (h *homeFake) SetHome(dir string) { h.home = dir }

// 登記照子模組順序合併；SetHome 只交給有 SetHome 的子模組。
func TestGroupMergesRegistrationsAndForwardsSetHome(t *testing.T) {
	l := &eventLog{}
	run := func(context.Context) error { return nil }
	ok := func(context.Context) CheckResult { return CheckResult{} }
	load := func(context.Context) (any, error) { return 1, nil }
	first := &rich{fake: fake{name: "a", log: l}, mounts: []Mount{{Service: "reminders", Impl: &fakeReminders{}}},
		checks: []Check{{Name: "a.one", Run: ok}}, jobs: []Job{{Name: "a.tick", Every: time.Minute, Run: run}},
		board: []BoardPart{{Key: "a", Load: load}}}
	second := &rich{fake: fake{name: "b", log: l}, checks: []Check{{Name: "b.one", Run: ok}}, board: []BoardPart{{Key: "b", Load: load}}}
	h := &homeFake{fake: fake{name: "h", log: l}}
	src := &noticeFake{fake: fake{name: "n", log: l}, sources: []NoticeSource{{Name: "src"}}}
	g := NewGroup("blk", first, h, second, src)
	g.SetHome("/tmp/x")
	if h.home != "/tmp/x" {
		t.Fatalf("SetHome 沒交到：%q", h.home)
	}
	if r := g.Routes(); len(r) != 1 || r[0].Service != "reminders" {
		t.Fatalf("routes %+v", r)
	}
	if c := g.Checks(); len(c) != 2 || c[0].Name != "a.one" || c[1].Name != "b.one" {
		t.Fatalf("checks %+v", c)
	}
	if j := g.Jobs(); len(j) != 1 || j[0].Name != "a.tick" {
		t.Fatalf("jobs %+v", j)
	}
	if b := g.Board(); len(b) != 2 || b[0].Key != "a" || b[1].Key != "b" {
		t.Fatalf("board %+v", b)
	}
	if s := g.NoticeSources(); len(s) != 1 || s[0].Name != "src" {
		t.Fatalf("sources %+v", s)
	}
	// 放進 App：收集與重名檢查照樣做（重名的錯誤講塊名）。
	a := New(g)
	if mounts, err := a.Mounts(); err != nil || len(mounts) != 1 {
		t.Fatal(mounts, err)
	}
	dup := &rich{fake: fake{name: "c", log: l}, checks: []Check{{Name: "a.one", Run: ok}}}
	if _, err := New(g, dup).Checks(); err == nil || !strings.Contains(err.Error(), "blk") {
		t.Fatalf("跨塊重名要擋：%v", err)
	}
}

type noticeFake struct {
	fake
	sources  []NoticeSource
	consumed []NoticeSource
}

func (n *noticeFake) NoticeSources() []NoticeSource { return n.sources }
func (n *noticeFake) ConsumeNoticeSources(s []NoticeSource) error {
	n.consumed = s
	return nil
}

// 塊裡的消費者（例 inbox 的 notify、overview，system 的 doctor）經塊收到 App 合併後的登記；消費者回錯，錯誤講出塊與子模組。
func TestGroupForwardsConsumersDuringAssemble(t *testing.T) {
	l := &eventLog{}
	run := func(context.Context) error { return nil }
	feat := &rich{fake: fake{name: "feat", log: l},
		checks: []Check{{Name: "a.one", Run: func(context.Context) CheckResult { return CheckResult{} }}},
		jobs:   []Job{{Name: "feat.tick", Every: time.Minute, Run: run}},
		board:  []BoardPart{{Key: "k", Load: func(context.Context) (any, error) { return 1, nil }}}}
	c := &collector{fake: fake{name: "c", log: l}}
	n := &noticeFake{fake: fake{name: "n", log: l}, sources: []NoticeSource{{Name: "src"}}}
	sink := &jobSink{fake: fake{name: "runner", log: l}}
	a := New(feat, NewGroup("blk", c, n, sink))
	if err := a.Assemble(); err != nil {
		t.Fatal(err)
	}
	if len(c.checks) != 1 || len(c.parts) != 1 || len(n.consumed) != 1 || n.consumed[0].Name != "src" || sink.seen != 1 || len(sink.got) != 1 {
		t.Fatalf("消費者沒收到：%+v %+v %+v %d", c.checks, c.parts, n.consumed, sink.seen)
	}
	bad := &collector{fake: fake{name: "c", log: l}, err: errors.New("不認得")}
	if err := New(feat, NewGroup("blk", bad)).Assemble(); err == nil || !strings.Contains(err.Error(), "blk 不收健檢：c：不認得") {
		t.Fatalf("消費者回錯要擋：%v", err)
	}
	// 沒有消費者的塊轉了也什麼都不做。
	if err := New(feat, NewGroup("plain", mods(l, "p")...)).Assemble(); err != nil {
		t.Fatal(err)
	}
}

var _ sdk.RemindersAPI = (*fakeReminders)(nil)

// slowStop 的 Stop 花 d 才回來（比逾時短）。
type slowStop struct {
	fake
	d time.Duration
}

func (s *slowStop) Stop(ctx context.Context) error {
	s.log.add("stop:" + s.name)
	time.Sleep(s.d)
	return nil
}

// 塊放在 App 裡：App 給整塊「子模組數 × 每個的逾時」，子模組照 App 的 WithStopTimeout 各自計時；
// 子模組慢或卡住時，塊裡其他子模組一定先停完，App 才去停下層（不會底層停了塊還在停）。
func TestGroupInsideAppStopsAllSubmodulesBeforeLowerModules(t *testing.T) {
	l := &eventLog{}
	lower := &fake{name: "lower", log: l}
	a := &slowStop{fake: fake{name: "a", log: l}, d: 40 * time.Millisecond}
	b := &slowStop{fake: fake{name: "b", log: l}, d: 40 * time.Millisecond}
	all := New(lower, NewGroup("blk", a, b)).WithStopTimeout(60 * time.Millisecond)
	if err := all.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := all.Stop(context.Background()); err != nil {
		t.Fatalf("每個子模組都在自己的逾時內停完，整塊不算逾時：%v", err)
	}
	if got := l.get(); got != "start:lower,start:a,start:b,stop:b,stop:a,stop:lower" {
		t.Fatalf("停止順序：%s", got)
	}

	l2 := &eventLog{}
	lower2 := &fake{name: "lower", log: l2}
	x := &fake{name: "x", log: l2}
	hang := &fake{name: "hang", log: l2, stopHang: true, release: make(chan struct{})}
	defer close(hang.release)
	all2 := New(lower2, NewGroup("blk", x, hang)).WithStopTimeout(30 * time.Millisecond)
	if err := all2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := all2.Stop(context.Background())
	if err == nil || !strings.Contains(err.Error(), "停止 hang 逾時（30ms）") {
		t.Fatalf("子模組照 App 的逾時算：%v", err)
	}
	if got := l2.get(); got != "start:lower,start:x,start:hang,stop:hang,stop:x,stop:lower" {
		t.Fatalf("卡住的子模組逾時後塊裡其他的先停，才停下層：%s", got)
	}
}
