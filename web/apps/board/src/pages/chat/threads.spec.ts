import type { ThreadStatus, ThreadSummary } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import { type StatusTone, threadTone } from "../../lib/statusTone";
import {
  CACHE_SOON_MINUTES,
  CACHE_TICK_MS,
  cacheLeftMinutes,
  cacheView,
  groupWork,
  isWork,
  showTypingDots,
  sortThreads,
  statusView,
  workTitle,
} from "./threads";

const th = (name: string, status: ThreadSummary["status"], at = ""): ThreadSummary => ({
  name,
  status,
  last_assistant_at: at,
  unread: 0,
  queued: 0,
  background_tasks: 0,
});

describe("對話的顯示方式", () => {
  test("預設模式一律寫處理中，不寫工具或正在做什麼；其他狀態不受影響", () => {
    for (const activity of ["跑指令", "讀檔", "用 jira", "思考中", ""]) {
      expect(statusView({ status: "working", activity, queued: 0 }).text).toBe("處理中");
    }
    expect(statusView({ status: "queued", activity: "", queued: 2 }).text).toBe("排隊中（2）");
    // 你的話排了半分鐘以上還沒被收：寫她那邊沒回應；剛排的、排名額的不寫。
    const now = new Date("2026-11-14T08:00:40Z");
    const queuedAt = (since: string, waiting?: string[]) =>
      statusView({ status: "queued", activity: "", queued: 1, status_since: since, waiting }, now)
        .text;
    expect(queuedAt("2026-11-14T08:00:00Z")).toBe("排隊中（1）· 秘書那邊沒回應");
    expect(queuedAt("2026-11-14T08:00:25Z")).toBe("排隊中（1）");
    expect(queuedAt("0001-01-01T00:00:00Z")).toBe("排隊中（1）"); // 不知道從什麼時候開始：不猜
    expect(queuedAt("2026-11-14T08:00:00Z", ["work_queue"])).not.toContain("沒回應");
    expect(statusView({ status: "stopped", activity: "", queued: 0 }).text).toBe("沒在跑");
    expect(
      statusView({ status: "stopped", activity: "", queued: 0, kind: "work", task: "DEV-1" }).text,
    ).toBe("已收起，說話就接回");
    expect(statusView({ status: "waiting_user", activity: "", queued: 0 }).tone).toBe("reply");
  });

  test("預設模式：她忙的整段都有三點，中途說過話也一樣；有卡片等你回答或沒在忙就沒有", () => {
    const base = { working: true, toolDetails: false, lastIsUser: false, awaitingAnswer: false };
    expect(showTypingDots(base)).toBe(true);
    expect(showTypingDots({ ...base, lastIsUser: true })).toBe(true);
    expect(showTypingDots({ ...base, awaitingAnswer: true })).toBe(false);
    expect(showTypingDots({ ...base, working: false })).toBe(false);
  });

  test("工程師模式：三點照舊只在最後一則是你的話時出現", () => {
    const base = { working: true, toolDetails: true, lastIsUser: true, awaitingAnswer: false };
    expect(showTypingDots(base)).toBe(true);
    expect(showTypingDots({ ...base, lastIsUser: false })).toBe(false);
    expect(showTypingDots({ ...base, working: false })).toBe(false);
  });

  test("工程師模式照舊寫正在做什麼", () => {
    const th = { status: "working" as ThreadStatus, activity: "跑指令", queued: 0 };
    expect(statusView(th, undefined, true).text).toBe("跑指令");
  });
});

describe("討論串狀態與排序", () => {
  // Guards: REQ-142
  test("排序：要你回覆 → 處理中／排隊 → 其他；同一組新的在前", () => {
    const sorted = sortThreads([
      th("old-idle", "idle", "2026-10-24T08:00:00Z"),
      th("new-done", "done", "2026-10-24T11:00:00Z"),
      th("work", "working", "2026-10-24T07:00:00Z"),
      th("queue", "queued", "2026-10-24T09:00:00Z"),
      th("ask-old", "waiting_user", "2026-10-24T06:00:00Z"),
      th("ask-new", "waiting_user", "2026-10-24T10:00:00Z"),
    ]);
    expect(sorted.map((x) => x.name)).toEqual([
      "ask-new",
      "ask-old",
      "queue",
      "work",
      "new-done",
      "old-idle",
    ]);
  });

  // Guards: REQ-142
  test("狀態翻成白話：處理中顯示正在做什麼，排隊顯示件數，idle 不顯示", () => {
    expect(statusView({ status: "working", activity: "Read", queued: 0 }, undefined, true)).toEqual(
      {
        text: "Read",
        tone: "busy",
      },
    );
    expect(statusView({ status: "queued", activity: "", queued: 2 }).text).toBe("排隊中（2）");
    expect(statusView({ status: "stopped", activity: "", queued: 0 }).text).toBe("沒在跑");
    expect(
      statusView({ status: "stopped", activity: "", queued: 0, kind: "work", task: "DEV-1" }).text,
    ).toBe("已收起，說話就接回");
    expect(statusView({ status: "waiting_user", activity: "", queued: 0 }).tone).toBe("reply");
    expect(statusView({ status: "done", activity: "", queued: 0 }).text).toBe("完成");
    // 不認得的狀態（含舊版那台送來的 stuck）：不寫字、灰色（S5）
    expect(statusView({ status: "stuck" as ThreadStatus, activity: "", queued: 0 })).toEqual({
      text: "",
      tone: "idle",
    });
    expect(statusView({ status: "idle", activity: "", queued: 0 }).text).toBe("");
  });
});

// ── 工作 session（P7-5、D-33、REQ-204）──────────────────────────────────────────────
const work = (
  name: string,
  agent: string,
  task: string,
  over: Partial<ThreadSummary> = {},
): ThreadSummary => ({
  ...th(name, "idle"),
  agent,
  kind: "work",
  task,
  title: `${task} 整理報表`,
  ...over,
});
const main = (agent: string): ThreadSummary => ({ ...th(agent, "idle"), agent, kind: "resident" });

describe("工作 session：收在她的秘書底下", () => {
  // Guards: REQ-204
  test("工作 session 收進她的秘書的主要討論串底下，依單號排；沒有主要討論串的留在頂層", () => {
    const { top, nested } = groupWork([
      main("code"),
      work("code-sec-10", "code", "SEC-10"),
      work("code-sec-2", "code", "SEC-2"),
      work("life-sec-5", "life", "SEC-5"), // life 的主要討論串不在清單裡
    ]);
    expect(top.map((x) => x.name)).toEqual(["code", "life-sec-5"]);
    expect(nested.get("code")?.map((x) => x.task)).toEqual(["SEC-2", "SEC-10"]);
    expect(nested.has("life")).toBe(false);
    // 不是工作 session（沒有單號）的串不收。
    expect(isWork({ kind: "work", task: "" })).toBe(false);
    expect(isWork({ kind: "room", task: "SEC-1" })).toBe(false);
    expect(isWork(work("a", "code", "SEC-1"))).toBe(true);
  });

  // WEB-121：有空的她底下有張工作 session 在等你回覆，她那一列要排上去，不要埋在清單最後。
  test("底下的工作 session 要你回覆或在處理時，她那一列跟著排到那一組", () => {
    const sorted = sortThreads([
      th("life", "idle", "2026-11-03T03:00:00Z"),
      { ...main("code"), last_assistant_at: "2026-11-03T01:00:00Z" },
      work("code-sec-2", "code", "SEC-2", { status: "waiting_user" }),
      th("art", "working"),
    ]);
    const { top } = groupWork(sorted);
    expect(top.map((x) => x.name)).toEqual(["code", "art", "life"]);
    // 底下沒有急事就照原本的順序。
    const calm = groupWork(sortThreads([th("life", "idle", "2026-11-03T03:00:00Z"), main("code")]));
    expect(calm.top.map((x) => x.name)).toEqual(["life", "code"]);
  });

  // Guards: REQ-204
  test("標題去掉前面的單號", () => {
    expect(workTitle({ title: "SEC-7 整理報表", task: "SEC-7" })).toBe("整理報表");
    expect(workTitle({ title: "整理報表", task: "SEC-7" })).toBe("整理報表");
  });

  // Guards: REQ-204
  test("hot cache 倒數：上一輪結束起 60 分鐘；跑著、沒跑過、過期都不顯示", () => {
    const now = new Date("2026-10-27T10:00:00Z");
    const ended = (minAgo: number) => new Date(now.getTime() - minAgo * 60_000).toISOString();
    expect(cacheLeftMinutes({ status: "idle", last_turn_end_at: ended(22) }, now)).toBe(38);
    expect(cacheLeftMinutes({ status: "stopped", last_turn_end_at: ended(59.5) }, now)).toBe(1);
    expect(cacheLeftMinutes({ status: "idle", last_turn_end_at: ended(60) }, now)).toBeNull();
    expect(cacheLeftMinutes({ status: "idle", last_turn_end_at: ended(90) }, now)).toBeNull();
    expect(cacheLeftMinutes({ status: "working", last_turn_end_at: ended(5) }, now)).toBeNull();
    expect(cacheLeftMinutes({ status: "idle" }, now)).toBeNull();
  });

  // Guards: REQ-204, REQ-221
  test("排隊等名額寫白話，不是「要你回答」", () => {
    // 新版伺服器：queued＋work_queue（S5 D6）；舊版那台：waiting_user＋work_queue。
    expect(
      statusView({ status: "queued", activity: "", queued: 0, waiting: ["work_queue"] }).text,
    ).toBe("排隊等名額");
    expect(
      statusView({ status: "waiting_user", activity: "", queued: 0, waiting: ["work_queue"] }).text,
    ).toBe("排隊等名額");
    expect(
      statusView({
        status: "waiting_user",
        activity: "",
        queued: 0,
        waiting: ["approval", "work_queue"],
      }).text,
    ).toBe("要你同意");
  });

  // DEV-156 U-10：等你處理收成三種，串上寫的是哪一種看在等什麼。
  test("在等權限或通知點頭寫「要你同意」，有問題或待辦要回寫「要你回答」", () => {
    const v = (waiting: string[]) =>
      statusView({ status: "waiting_user", activity: "", queued: 0, waiting }).text;
    expect(v(["permission"])).toBe("要你同意");
    expect(v(["question"])).toBe("要你回答");
    expect(v(["permission", "question"])).toBe("要你回答");
    expect(v(["reminder"])).toBe("要你回答");
    expect(v([])).toBe("要你回答");
  });
});

// ── hot cache 倒數（P7-6、D-34、REQ-228）：上一輪結束起 60 分鐘，只做在前端 ──────────────────
describe("hot cache 倒數", () => {
  const now = new Date("2026-10-28T10:00:00Z");
  const endedAt = (secAgo: number) => new Date(now.getTime() - secAgo * 1000).toISOString();

  // Guards: REQ-228
  test("一路倒數到 1 分鐘，滿 60 分鐘就沒了；剛結束顯示滿的 60，不會超過", () => {
    const left = (secAgo: number) =>
      cacheLeftMinutes({ status: "idle", last_turn_end_at: endedAt(secAgo) }, now);
    expect(left(0)).toBe(60);
    expect(left(1)).toBe(60); // 1 秒前結束：還有 59 分 59 秒，進位成 60
    expect(left(60)).toBe(59);
    expect(left(59 * 60)).toBe(1);
    expect(left(59 * 60 + 59)).toBe(1);
    expect(left(60 * 60)).toBeNull();
    // 時鐘比伺服器快一點（結束時間在「未來」）：不超過 60。
    expect(cacheLeftMinutes({ status: "idle", last_turn_end_at: endedAt(-120) }, now)).toBe(60);
  });

  // Guards: REQ-228
  test("剩 10 分鐘以內換成快過期的說法與顏色（清單列與標題欄共用）", () => {
    expect(CACHE_SOON_MINUTES).toBe(10);
    expect(cacheView(38)).toEqual({ text: "快取剩 38 分", soon: false });
    expect(cacheView(11).soon).toBe(false);
    expect(cacheView(10)).toEqual({ text: "快取快過期了（剩 10 分）", soon: true });
    expect(cacheView(1).soon).toBe(true);
  });

  // Guards: REQ-228
  test("壞掉的時間不顯示倒數", () => {
    expect(cacheLeftMinutes({ status: "idle", last_turn_end_at: "不是時間" }, now)).toBeNull();
    expect(cacheLeftMinutes({ status: "idle", last_turn_end_at: "" }, now)).toBeNull();
  });
});

describe("額度用完（WEB-73）", () => {
  // Guards: REQ-378
  test("灰色「額度用完，13:40 恢復」；不是卡住、不排在要你回覆或處理中前面", () => {
    const now = new Date(2026, 9, 30, 12, 5);
    const until = new Date(2026, 9, 30, 13, 40).toISOString();
    expect(
      statusView({ status: "limited", activity: "", queued: 0, limited_until: until }, now),
    ).toEqual({
      text: "額度用完，13:40 恢復",
      tone: "idle",
    });
    expect(statusView({ status: "limited", activity: "", queued: 0 }, now)).toEqual({
      text: "額度用完",
      tone: "idle",
    });
    const tomorrow = new Date(2026, 9, 31, 9, 0).toISOString();
    expect(
      statusView({ status: "limited", activity: "", queued: 0, limited_until: tomorrow }, now).text,
    ).toBe("額度用完，明天 09:00 恢復");
    const sorted = sortThreads([
      { name: "limited", status: "limited" },
      { name: "working", status: "working" },
    ] as ThreadSummary[]);
    expect(sorted.map((th) => th.name)).toEqual(["working", "limited"]);
  });
});

// 從 lib/statusTone.spec.ts 與 data/live.spec.tsx 搬來（FEN-27：共用層的測試不 import 頁面）。
describe("討論串狀態跟看板同一套顏色（WEB-70）", () => {
  // 每一種討論串狀態都要列在這裡（Record 讓 ThreadStatus 多一種時這裡編不過，逼人決定顏色）。
  const threadExpect: Record<ThreadStatus, StatusTone> = {
    waiting_user: "reply",
    working: "busy",
    done: "fresh",
    idle: "idle",
    stopped: "idle",
    starting: "idle",
    queued: "idle",
    limited: "idle",
  };

  // Guards: REQ-382
  test("statusView 的顏色跟 threadTone 一樣", () => {
    for (const [status, tone] of Object.entries(threadExpect)) {
      expect(threadTone({ status: status as ThreadStatus }), status).toBe(tone);
      expect(statusView({ status: status as ThreadStatus, activity: "", queued: 0 }).tone).toBe(
        tone,
      );
    }
  });

  // Guards: REQ-382
  test("排隊等名額與額度用完是灰色配文字", () => {
    for (const status of ["queued", "waiting_user"] as const) {
      expect(statusView({ status, waiting: ["work_queue"], activity: "", queued: 0 })).toEqual({
        text: "排隊等名額",
        tone: "idle",
      });
    }
    // 額度用完（S4）：灰色；說不出恢復時間只寫「額度用完」。
    expect(statusView({ status: "limited", activity: "", queued: 0 })).toEqual({
      text: "額度用完",
      tone: "idle",
    });
  });

  // 工作對話的快取倒數 3 秒重算一次，不超過看板「不輪詢超過 3 秒」的上限（S8）。
  // Guards: REQ-389
  test("快取倒數的重算間隔不超過 3 秒", () => {
    expect(CACHE_TICK_MS).toBeLessThanOrEqual(3_000);
  });
});
