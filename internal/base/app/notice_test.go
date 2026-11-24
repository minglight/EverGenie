package app

import "testing"

type noticeOwner struct {
	fake
	sources []NoticeSource
}

func (n *noticeOwner) NoticeSources() []NoticeSource { return n.sources }

// 通知來源依模組順序收集；沒實作的模組不算；名稱空的或重複回錯（只有一份清單，notify 與 session 都看它）。
func TestNoticeSourcesCollectedOnce(t *testing.T) {
	log := &eventLog{}
	a := New(&noticeOwner{fake: fake{name: "goals", log: log}, sources: []NoticeSource{{Name: "goals", Changed: "goal.changed"}}},
		&fake{name: "plain", log: log},
		&noticeOwner{fake: fake{name: "packages", log: log}, sources: []NoticeSource{{Name: "package"}}})
	got, err := a.NoticeSources()
	if err != nil || len(got) != 2 || got[0].Name != "goals" || got[1].Name != "package" {
		t.Fatal(got, err)
	}
	dup := New(&noticeOwner{fake: fake{name: "a", log: log}, sources: []NoticeSource{{Name: "x"}}}, &noticeOwner{fake: fake{name: "b", log: log}, sources: []NoticeSource{{Name: "x"}}})
	if _, err := dup.NoticeSources(); err == nil {
		t.Fatal("重複的來源要回錯")
	}
	empty := New(&noticeOwner{fake: fake{name: "a", log: log}, sources: []NoticeSource{{}}})
	if _, err := empty.NoticeSources(); err == nil {
		t.Fatal("沒有名稱要回錯")
	}
}
