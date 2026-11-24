package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/sdk"
)

// finishQueued 把這個 session 還在處理的訊息都當成回完了：closeWhenSettled 不剪掉處理中的訊息。
func (w *workFixture) finishQueued(t *testing.T, name string) {
	t.Helper()
	live, err := w.b.List(context.Background(), name, sdk.LifecycleQueued, sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked)
	must(t, err)
	for _, e := range live {
		must(t, w.b.Transition(context.Background(), e.ID, sdk.LifecycleFailed, bus.TransitionOpt{}))
	}
}

// 家的 [tasks] keep_review_open：true＝等你驗收時工作對話開著不關、被退回直接在同一個對話送退回意見、驗收才關；
// false（預設）＝照舊，送驗收、閒了就關。核心只看這個設定，不看單號字首。
func TestKeepReviewOpenDecidesWhetherReviewCloses(t *testing.T) {
	t.Run("off closes on review", func(t *testing.T) {
		w := workSetup(t, 3, 1)
		w.openAs(t, "SEC-1", "code")
		w.setStatus("SEC-1", deliveredStatus)
		w.settle()
		if w.state("code-sec-1") != sdk.SessionStateStopped || w.stops("code-sec-1") != 1 {
			t.Fatalf("沒設 keep_review_open，送驗收要關：%s", w.state("code-sec-1"))
		}
	})

	t.Run("on keeps open, tells on reject, closes on done", func(t *testing.T) {
		w := workSetup(t, 3, 1)
		w.m.config.Tasks.KeepReviewOpen = true
		w.openAs(t, "SEC-1", "code")
		w.setStatus("SEC-1", deliveredStatus)
		w.after(WorkReconcileEvery)
		w.after(WorkSettle)
		w.after(time.Hour)
		if w.state("code-sec-1") == sdk.SessionStateStopped || w.stops("code-sec-1") != 0 {
			t.Fatalf("keep_review_open：等你驗收時不能關：%s", w.state("code-sec-1"))
		}
		// 退回：回 doing、帶退回意見 → 同一個對話收到退回意見，不重開。
		w.tasks.set("SEC-1", func(task *sdk.Task) { task.ReviewNote = "按鈕太小" })
		w.setStatus("SEC-1", sdk.TaskStatusDoing)
		w.after(WorkReconcileEvery)
		if len(w.starts("code-sec-1")) != 1 {
			t.Fatalf("對話還開著，退回不重開：%+v", w.starts("code-sec-1"))
		}
		rows, err := w.b.List(context.Background(), "code-sec-1")
		must(t, err)
		found := false
		for _, e := range rows {
			if e.From == "system" && strings.Contains(e.Body, "SEC-1") && strings.Contains(e.Body, "按鈕太小") {
				found = true
			}
		}
		if !found {
			t.Fatalf("退回時要把退回意見送進對話：%+v", rows)
		}
		w.finishQueued(t, "code-sec-1")
		w.setStatus("SEC-1", sdk.TaskStatusDone)
		w.settle()
		w.after(time.Hour)
		if w.state("code-sec-1") != sdk.SessionStateStopped || len(w.starts("code-sec-1")) != 1 {
			t.Fatalf("完成要關、不再開：%s", w.state("code-sec-1"))
		}
	})
}

// keep_review_open 的家：等你驗收時被關掉的對話（重開機等）自動接回，而且不占進行中的名額；主人手動關的不接回。
func TestKeepReviewOpenResumesClosedReviewWithoutUsingSlot(t *testing.T) {
	w := workSetup(t, 1, 3)
	w.m.config.Tasks.KeepReviewOpen = true
	for _, id := range []string{"SEC-1", "SEC-2"} {
		w.openAs(t, id, "code")
		w.setStatus(id, deliveredStatus)
		w.after(WorkReconcileEvery)
	}
	w.openAs(t, "SEC-3", "code") // doing：占掉唯一的名額
	if w.state("code-sec-3") == sdk.SessionStateStopped {
		t.Fatalf("驗收中的不算進行中，SEC-3 要開得起來：%s", w.state("code-sec-3"))
	}
	w.m.operations.Lock()
	must(t, w.m.close(context.Background(), "code-sec-1")) // 被關掉（不是主人手動關）
	w.m.operations.Unlock()
	must(t, w.m.Close(context.Background(), "code-sec-2")) // 主人手動關
	w.after(WorkReconcileEvery)
	w.after(WorkReconcileEvery)
	if w.state("code-sec-1") == sdk.SessionStateStopped || len(w.starts("code-sec-1")) != 2 {
		t.Fatalf("驗收中被關掉的要接回（名額滿了也接）：%s %d", w.state("code-sec-1"), len(w.starts("code-sec-1")))
	}
	if w.state("code-sec-2") != sdk.SessionStateStopped {
		t.Fatalf("主人手動關的不接回：%s", w.state("code-sec-2"))
	}
}
