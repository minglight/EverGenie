import { describe, expect, test } from "vitest";
import {
  isEmptyPatch,
  reminderDraftErrors,
  reminderDraftOf,
  reminderPatch,
  taskDraftErrors,
  taskDraftOf,
  taskPatch,
} from "./editPatch";

describe("待辦整張表單只送改過的欄位", () => {
  const reminder = {
    id: "R-1",
    content: "打給牙醫",
    at: "2026-11-08T01:00:00Z",
    owner: "life",
    follow: "remind" as const,
    cycle: "once" as const,
    difficulty: "normal" as const,
    body: "帶健保卡",
  };

  test("沒改就是空的，不呼叫 API", () => {
    const d = reminderDraftOf(reminder);
    expect(isEmptyPatch(reminderPatch(d, { ...d }))).toBe(true);
  });

  test("改了內容、負責秘書、難度：只送這三個（內容去頭尾空白）", () => {
    const d = reminderDraftOf(reminder);
    const patch = reminderPatch(d, {
      ...d,
      content: " 打給牙醫改期 ",
      owner: "health",
      difficulty: "hard",
    });
    expect(patch).toEqual({ content: "打給牙醫改期", owner: "health", difficulty: "hard" });
  });

  test("時間改了送 ISO；同一刻不送；清掉（null）不送，API 沒有拿掉時間", () => {
    const d = reminderDraftOf(reminder);
    const next = new Date("2026-11-09T02:30:00Z");
    expect(reminderPatch(d, { ...d, at: next })).toEqual({ at: next.toISOString() });
    expect(reminderPatch(d, { ...d, at: new Date("2026-11-08T01:00:00Z") })).toEqual({});
    expect(reminderPatch(d, { ...d, at: null })).toEqual({});
  });

  test("要確認什麼只在跟進方式是確認時送", () => {
    const d = reminderDraftOf(reminder);
    expect(reminderPatch(d, { ...d, check: "繳了沒" })).toEqual({});
    expect(reminderPatch(d, { ...d, follow: "check", check: "繳了沒" })).toEqual({
      follow: "check",
      check: "繳了沒",
    });
  });

  test("描述清空送空字串（清掉）", () => {
    const d = reminderDraftOf(reminder);
    expect(reminderPatch(d, { ...d, body: "  " })).toEqual({ body: "" });
  });

  test("內容空白、確認方式沒寫要擋", () => {
    const d = reminderDraftOf(reminder);
    expect(reminderDraftErrors(d)).toEqual({});
    expect(reminderDraftErrors({ ...d, content: " " })).toEqual({
      content: "reminder.edit.required",
    });
    expect(reminderDraftErrors({ ...d, follow: "check", check: "" })).toEqual({
      check: "quick.check.required",
    });
  });
});

describe("任務單整張表單只送改過的欄位", () => {
  const task = {
    id: "SEC-1",
    title: "整理書櫃",
    agent: "life",
    type: "task" as const,
    body: "客廳那個",
    criteria: "拍照給我看",
  };

  test("沒改就是空的", () => {
    const d = taskDraftOf(task);
    expect(isEmptyPatch(taskPatch(d, { ...d }))).toBe(true);
  });

  test("改標題、負責秘書、類型：只送這三個", () => {
    const d = taskDraftOf(task);
    expect(taskPatch(d, { ...d, title: " 整理書櫃和書桌 ", agent: "code", type: "ask" })).toEqual({
      title: "整理書櫃和書桌",
      agent: "code",
      type: "ask",
    });
  });

  test("驗收條件改了才送（主人改的算主人的，由伺服器記）；問答類不送驗收條件", () => {
    const d = taskDraftOf(task);
    expect(taskPatch(d, { ...d, criteria: "拍照" })).toEqual({ criteria: "拍照" });
    expect(taskPatch(d, { ...d, criteria: " 拍照給我看 " })).toEqual({});
    expect(taskPatch(d, { ...d, type: "ask", criteria: "別的" })).toEqual({ type: "ask" });
  });

  test("標題不能空", () => {
    const d = taskDraftOf(task);
    expect(taskDraftErrors(d)).toEqual({});
    expect(taskDraftErrors({ ...d, title: "  " })).toEqual({ title: "task.title.required" });
  });
});
