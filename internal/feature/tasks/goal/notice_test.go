package goal

import (
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-374
// goals 登記自己的通知來源；確認通知在 Goal 還是草稿時算在等，主人回答（不再是草稿）就不等了，notify 據此收掉。
func TestNoticeSourceWaitingFollowsDraft(t *testing.T) {
	m, _, _, _ := fixture(t)
	src := m.NoticeSources()
	if len(src) != 1 || src[0].Name != sdk.NotifySourceGoals || src[0].Changed != "goal.changed" || src[0].Waiting == nil {
		t.Fatal(src)
	}
	g := propose(t, m)
	waiting, known, err := src[0].Waiting(ctx)
	if err != nil || !waiting[g.Notice] || !known[g.Notice] {
		t.Fatal(waiting, known, err)
	}
	respond(t, m, g, "確認", "owner(web)")
	waiting, known, err = src[0].Waiting(ctx)
	if err != nil || waiting[g.Notice] || !known[g.Notice] {
		t.Fatal(waiting, known, err)
	}
}
