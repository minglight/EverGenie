package requests

import (
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-374
// policy 登記自己的通知來源；申請待審時審核通知算在等，決定之後就不等了，notify 據此收掉。
func TestNoticeSourceWaitingFollowsPending(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	src := m.NoticeSources()
	if len(src) != 1 || src[0].Name != sdk.NotifySourcePolicy || src[0].Changed != "policy.changed" || src[0].Waiting == nil {
		t.Fatal(src)
	}
	q, err := m.Request(ctx, ruleReq("~/Workspace/amy/evergenie"), "code")
	if err != nil {
		t.Fatal(err)
	}
	waiting, known, err := src[0].Waiting(ctx)
	if err != nil || !waiting[q.Notice] || !known[q.Notice] {
		t.Fatal(waiting, known, err)
	}
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	waiting, known, err = src[0].Waiting(ctx)
	if err != nil || waiting[q.Notice] || !known[q.Notice] {
		t.Fatal(waiting, known, err)
	}
}
