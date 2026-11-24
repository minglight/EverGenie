import type { Notification, SessionState, ThreadStatus } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import type { Presence } from "../data/agents";
import {
  needsReply,
  noticeNeedsReply,
  noticeTone,
  REPLY_BADGES,
  STATUS_TONES,
  type StatusTone,
  statusTone,
  threadTone,
  toneBadge,
  toneDot,
  tonePill,
  toneText,
} from "./statusTone";

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
const sessionExpect: Record<SessionState, StatusTone> = {
  waiting: "reply",
  busy: "busy",
  idle: "idle",
  stopped: "idle",
  starting: "idle",
  limited: "idle",
};
const presenceExpect: Record<Presence, StatusTone> = {
  waiting: "reply",
  working: "busy",
  idle: "idle",
};
const TONES: StatusTone[] = ["reply", "busy", "trouble", "fresh", "idle"];

describe("顏色一套規則（WEB-70）", () => {
  // Guards: REQ-382
  test("每個狀態對到一個顏色：討論串、session、秘書在場", () => {
    // 聊天頁的 statusView 也用同一套顏色，在 pages/chat/threads.spec.ts 檢查（FEN-27：共用層不 import 頁面）。
    for (const [status, tone] of Object.entries(threadExpect)) {
      expect(threadTone({ status: status as ThreadStatus }), status).toBe(tone);
    }
    for (const [state, tone] of Object.entries(sessionExpect)) {
      expect(statusTone(state), state).toBe(tone);
    }
    for (const [p, tone] of Object.entries(presenceExpect)) {
      expect(statusTone(p), p).toBe(tone);
    }
  });

  // Guards: REQ-382
  test("橘色只給等你處理：對照表裡是橘的只有 waiting_user 與 waiting", () => {
    const orange = Object.entries(STATUS_TONES)
      .filter(([, tone]) => tone === "reply")
      .map(([state]) => state)
      .sort();
    expect(orange).toEqual(["waiting", "waiting_user"]);
    // 每種顏色的字、點、膠囊、徽章，只有 reply 用到橘色（attention）的 token。
    for (const tone of TONES) {
      const classes = [toneText[tone], toneDot[tone], tonePill[tone], toneBadge[tone]].join(" ");
      expect(classes.includes("attention"), tone).toBe(tone === "reply");
    }
    // 五種顏色彼此不同。
    expect(new Set(TONES.map((x) => toneDot[x])).size).toBe(5);
    expect(new Set(TONES.map((x) => toneText[x])).size).toBe(5);
  });

  // Guards: REQ-382
  test("排隊等名額不是等你處理：灰色、沒有橘點；有別的在等你就是橘色", () => {
    // 新版伺服器給 queued＋work_queue（S5 D6）；舊版那台給 waiting_user＋work_queue，兩種都灰。
    const queued = { status: "queued" as const, waiting: ["work_queue"] };
    expect(needsReply(queued)).toBe(false);
    expect(threadTone(queued)).toBe("idle");
    const queue = { status: "waiting_user" as const, waiting: ["work_queue"] };
    expect(needsReply(queue)).toBe(false);
    expect(threadTone(queue)).toBe("idle");
    const asked = { status: "waiting_user" as const, waiting: ["approval", "work_queue"] };
    expect(needsReply(asked)).toBe(true);
    expect(threadTone(asked)).toBe("reply");
    expect(needsReply(undefined)).toBe(false);
  });

  // Guards: REQ-382
  test("沒送到與這一輪沒做完是紅色；不認得的狀態（含舊版的 stuck／lost／late）是灰色（不會意外變橘或紅）", () => {
    for (const s of ["not_delivered", "interrupted"]) expect(statusTone(s), s).toBe("trouble");
    for (const s of "stuck lost late something_new".split(" "))
      expect(statusTone(s), s).toBe("idle");
    expect(threadTone({ status: "stuck" as ThreadStatus })).toBe("idle");
    expect(statusTone(undefined)).toBe("idle");
  });

  // Guards: REQ-382
  test("選單徽章：只有「今天」（等你處理）與「聊天」（等你處理的串數）用橘色", () => {
    expect([...REPLY_BADGES].sort()).toEqual(["core.chat", "core.overview"]);
  });
});

describe("訊息頁的橘點（WEB-70）", () => {
  const n = (over: Partial<Notification>): Notification =>
    ({ id: "M-1", category: "approve", state: "unread", ...over }) as Notification;

  // Guards: REQ-383
  test("要你回答的那幾則是橘點；已讀還沒回也是", () => {
    expect(noticeTone(n({}))).toBe("reply");
    expect(noticeTone(n({ state: "read" }))).toBe("reply");
    expect(noticeNeedsReply(n({ state: "read" }))).toBe(true);
  });

  // Guards: REQ-383
  test("回答了、過期、被自動收掉（via auto）就沒有橘點", () => {
    expect(noticeTone(n({ state: "answered", answer: { text: "好", via: "button" } }))).toBeNull();
    expect(noticeTone(n({ state: "expired" }))).toBeNull();
    const auto = n({ state: "answered", answer: { text: "已經不用回了", via: "auto" } });
    expect(noticeNeedsReply(auto)).toBe(false);
    expect(noticeTone(auto)).toBeNull();
    // 防呆：伺服器還沒改狀態、但已經記了 auto 回答，也不是橘的。
    expect(noticeTone(n({ state: "unread", answer: { text: "已經不用回了", via: "auto" } }))).toBe(
      "fresh",
    );
  });

  // Guards: REQ-383
  test("不用回答的未讀是綠點，看過的沒有點", () => {
    expect(noticeTone(n({ category: "done" }))).toBe("fresh");
    expect(noticeTone(n({ category: "digest", state: "read" }))).toBeNull();
  });
});
