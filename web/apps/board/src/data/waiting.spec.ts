import type { Notification, Reminder, Task, WebGetOverviewResponse } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import { bodyDetail, filteredWaitingTotal, waitingCounts, workFor } from "./waiting";

const approval = (id: string, agent: string): Notification => ({
  id,
  agent,
  title: id,
  urgent: false,
  badge: false,
  delivers_answer: true,
});
const reviewTask = (id: string, agent: string): Task =>
  ({ id, agent, title: id, status: "waiting_user", proposed: "done", nudges: 0 }) as Task;
const todo = (id: string, owner: string): Reminder =>
  ({ id, owner, content: id, asks: 0, archived: false }) as Reminder;

describe("今天頁的數字", () => {
  // Guards: REQ-155
  test("TestTodayCountsMatchSubtitle：徽章、頁首大字、waiting_total 是同一個定義", () => {
    const overview: WebGetOverviewResponse = {
      waiting_total: 4,
      unread: 0,
      approvals: [approval("n1", "builder"), approval("n2", "builder"), approval("n3", "helper")],
      waiting_tasks: [{ task: reviewTask("t1", "builder"), waiting_days: 1 }],
      waiting_threads: [
        { name: "builder-1", agent: "builder", unread: 0, queued: 0, background_tasks: 0 },
      ],
      today_reminders: [todo("r1", "builder"), todo("r2", "builder")],
    };
    const { counts, total } = waitingCounts(overview);
    expect(counts).toEqual({ builder: 3, helper: 1 });
    expect(total).toBe(overview.waiting_total);
    // 選一位時，徽章等於頁首大字用的函式。
    expect(filteredWaitingTotal(overview, ["builder"])).toBe(counts.builder);
    expect(filteredWaitingTotal(overview, ["helper"])).toBe(counts.helper);
  });
});

describe("要你回覆的串", () => {
  test("舊版那台把只有「排隊等名額」的串放進 waiting_threads：不列（不是在等你，S5 D6）", () => {
    const overview = {
      waiting_total: 1,
      unread: 0,
      waiting_threads: [
        {
          name: "code-sec-9",
          agent: "code",
          status: "waiting_user",
          waiting: ["work_queue"],
          unread: 0,
          queued: 0,
          background_tasks: 0,
        },
        {
          name: "code",
          agent: "code",
          status: "waiting_user",
          waiting: ["approval"],
          unread: 0,
          queued: 0,
          background_tasks: 0,
        },
      ],
    } as WebGetOverviewResponse;
    expect(workFor(overview, []).threads.map((th) => th.name)).toEqual(["code"]);
  });
});

// 通知沒有背景、請你時退回 body：不顯示 notify 組的 `[code｜] 類型｜標題` 那行，也不留空的「背景：」「請你：」（NTF-63）。
test("TestWaitingDetailDropsNoticeHeader", () => {
  expect(bodyDetail("[code｜] 事情做完｜測試做完了\n背景：\n請你：")).toBe("");
  expect(bodyDetail("[code｜main] 要你同意｜確認\n背景：舊系統搬來\n請你：")).toBe(
    "背景：舊系統搬來",
  );
  // 不是 notify 組的（舊系統搬來的純文字）照原樣
  expect(bodyDetail("Chrome 看門狗：AI 瀏覽器掛掉")).toBe("Chrome 看門狗：AI 瀏覽器掛掉");
  expect(bodyDetail(undefined)).toBe("");
  // 秘書自己寫的「[重要] …」不是 notify 的開頭，不能被刪
  expect(bodyDetail("[重要] Chrome 掛了")).toBe("[重要] Chrome 掛了");
});
