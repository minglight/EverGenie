import type {
  Notification,
  Reminder,
  Task,
  ThreadSummary,
  WebGetOverviewResponse,
} from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import type { Site } from "../../data/sites";
import { waitingCounts, workFor } from "../../data/waiting";
import {
  failedSites,
  mergedCounts,
  mergedUnread,
  mergedWaitingTotal,
  mergeWork,
  type SiteOverview,
  unreadThreads,
} from "./merged";

const site = (key: string, base = key ? `https://${key}.example.ts.net` : ""): Site => ({
  key,
  base,
  primary: key === "",
  label: key || "amy-mini",
  machine: key ? "office" : "mini",
  env: "prod",
  version: "",
  boardApi: 1,
  publicUrl: "",
  color: "",
  state: "ok",
  reason: "",
});
const A = site("");
const B = site("amy-office");

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

const overview = (o: Partial<WebGetOverviewResponse>): WebGetOverviewResponse => ({
  waiting_total: 0,
  unread: 0,
  ...o,
});

// 兩台都有 M-1、都有 code：合併後 key 不撞、別台的帶台名。
const oa = overview({
  waiting_total: 2,
  unread: 3,
  approvals: [approval("M-1", "code")],
  waiting_tasks: [{ task: reviewTask("SEC-1", "code"), waiting_days: 1 }],
  waiting_threads: [{ name: "code", agent: "code", unread: 0, queued: 0, background_tasks: 0 }],
  today_reminders: [todo("R-1", "code")],
});
const ob = overview({
  waiting_total: 1,
  unread: 2,
  approvals: [approval("M-1", "code")],
  waiting_threads: [{ name: "code", agent: "code", unread: 0, queued: 0, background_tasks: 0 }],
  today_reminders: [todo("R-1", "work")],
});
const two: SiteOverview[] = [
  { site: A, data: oa, failed: false },
  { site: B, data: ob, failed: false },
];

describe("今天頁合併兩台", () => {
  test("TestMergeWorkKeysCarrySiteAndApprovalsComeFirst：兩台同編號不撞，要你同意的排在任務單前面", () => {
    const { items, threads, reminders } = mergeWork(two, []);
    expect(items.map((i) => i.key)).toEqual(["M-1", "amy-office/M-1", "SEC-1"]);
    expect(items.map((i) => i.site.key)).toEqual(["", "amy-office", ""]);
    // 動作與連結用的還是那台自己的編號。
    expect(items[1]?.id).toBe("M-1");
    expect(threads.map((t) => t.key)).toEqual(["code", "amy-office/code"]);
    expect(reminders.map((r) => r.key)).toEqual(["R-1", "amy-office/R-1"]);
  });

  test("TestMergeWorkFiltersBySiteAgentKey：篩選 amy-office/code 只留那台的 code", () => {
    const { items, threads, reminders } = mergeWork(two, ["amy-office/code"]);
    expect(items.map((i) => i.key)).toEqual(["amy-office/M-1"]);
    expect(threads.map((t) => t.key)).toEqual(["amy-office/code"]);
    expect(reminders).toEqual([]);
    expect(mergedWaitingTotal(two, ["amy-office/code"])).toBe(1);
    expect(mergedWaitingTotal(two, ["code"])).toBe(2);
  });

  test("TestMergedNumbersAddUpEachSitesServerNumbers：件數、未讀都是各台伺服器的數字相加", () => {
    expect(mergedWaitingTotal(two, [])).toBe(3);
    expect(mergedUnread(two)).toBe(5);
    const { counts, total } = mergedCounts(two);
    expect(total).toBe(3);
    expect(counts).toEqual({ code: 2, "amy-office/code": 1 });
  });

  test("TestMergedWithOneSiteEqualsThatSite：只有一台時跟直接用那一份 overview 一樣", () => {
    const one: SiteOverview[] = [{ site: A, data: oa, failed: false }];
    const merged = mergeWork(one, []);
    const plain = workFor(oa, []);
    expect(merged.items.map((i) => i.key)).toEqual(plain.items.map((i) => i.id));
    expect(merged.threads.map((t) => t.thread)).toEqual(plain.threads);
    expect(merged.reminders.map((r) => r.reminder)).toEqual(plain.reminders);
    expect(mergedCounts(one)).toEqual(waitingCounts(oa));
    expect(mergedWaitingTotal(one, [])).toBe(oa.waiting_total);
  });

  test("TestMergedTotalWaitsForPrimary：主伺服器還沒讀到不給數字（不先顯示 0）", () => {
    expect(
      mergedWaitingTotal(
        [
          { site: A, data: undefined, failed: false },
          { site: B, data: ob, failed: false },
        ],
        [],
      ),
    ).toBeUndefined();
  });

  test("TestFailedSitesOnlyListsOtherSites：只列讀失敗的別台", () => {
    expect(
      failedSites([
        { site: A, data: undefined, failed: true },
        { site: B, data: undefined, failed: true },
      ]),
    ).toEqual(["amy-office"]);
  });
});

// 今天頁的「新訊息」（DEV-153）：秘書說了、主人還沒看的對話（例：新家第一位秘書上班後的招呼）。
describe("unreadThreads", () => {
  const th = (name: string, agent: string, unread: number): ThreadSummary => ({
    name,
    agent,
    unread,
    queued: 0,
    background_tasks: 0,
  });

  test("TestUnreadThreadsListsOnlyUnseen：只列有未讀的，照台、照清單順序，key 跨台不撞", () => {
    const got = unreadThreads(
      [
        { site: A, threads: [th("life", "life", 1), th("code", "code", 0)] },
        { site: B, threads: [th("life", "life", 2)] },
      ],
      [],
      [],
    );
    expect(got.map((g) => [g.site.key, g.thread.name, g.thread.unread])).toEqual([
      ["", "life", 1],
      ["amy-office", "life", 2],
    ]);
    expect(new Set(got.map((g) => g.key)).size).toBe(2);
  });

  test("TestUnreadThreadsSkipsWaitingAndFilters：要你回答的不重複，秘書篩選照各台的 key", () => {
    const lists = [{ site: A, threads: [th("life", "life", 1), th("work", "work", 3)] }];
    // 要你回答的那段用真的 mergeWork 算出來（兩邊的 key 一樣才去得掉重複）。
    const life = { ...th("life", "life", 1), status: "waiting_user" } as ThreadSummary;
    const waiting = mergeWork(
      [{ site: A, data: { waiting_threads: [life] } as WebGetOverviewResponse, failed: false }],
      [],
    ).threads;
    expect(waiting.map((w) => w.thread.name)).toEqual(["life"]);
    expect(unreadThreads(lists, waiting, []).map((g) => g.thread.name)).toEqual(["work"]);
    expect(unreadThreads(lists, [], ["work"]).map((g) => g.thread.name)).toEqual(["work"]);
    expect(unreadThreads([{ site: A, threads: undefined }], [], [])).toEqual([]);
  });
});
