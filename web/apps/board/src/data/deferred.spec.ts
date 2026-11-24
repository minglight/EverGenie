import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

const toasts: { message: string; undo?: () => void }[] = [];
vi.mock("@evergenie/ui", () => ({
  showToast: (message: string, options?: { undo?: { onClick: () => void } }) => {
    toasts.push({ message, undo: options?.undo?.onClick });
    return toasts.length;
  },
  dismissToast: () => {},
}));

import { DEFER_MS, deferAction, flushDeferred, hasDeferred } from "./deferred";

beforeEach(() => {
  vi.useFakeTimers();
  toasts.length = 0;
});
afterEach(() => {
  flushDeferred();
  vi.useRealTimers();
});

function defer(key: string) {
  const run = vi.fn().mockResolvedValue(undefined);
  const settled = vi.fn();
  deferAction({ key, message: `做完了：${key}`, run, settled });
  return { run, settled };
}

describe("deferAction（FEN-21）", () => {
  // Guards: REQ-59
  test("畫面先生效，5 秒內不呼叫 API；5 秒到了才送", async () => {
    const { run, settled } = defer("R-1");
    expect(hasDeferred("R-1")).toBe(true);
    expect(toasts[0]?.message).toBe("做完了：R-1");
    await vi.advanceTimersByTimeAsync(DEFER_MS - 1);
    expect(run).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(run).toHaveBeenCalledTimes(1);
    expect(settled).toHaveBeenCalledTimes(1);
    expect(hasDeferred("R-1")).toBe(false);
  });

  // Guards: REQ-59
  test("按復原就不送", async () => {
    const { run } = defer("R-2");
    toasts[0]?.undo?.();
    await vi.advanceTimersByTimeAsync(DEFER_MS * 2);
    expect(run).not.toHaveBeenCalled();
    expect(hasDeferred("R-2")).toBe(false);
  });

  // Guards: REQ-59
  test("離開畫面（flush）時等著的動作立刻全部送出", async () => {
    const a = defer("R-3");
    const b = defer("R-4");
    flushDeferred();
    await vi.advanceTimersByTimeAsync(0);
    expect(a.run).toHaveBeenCalledTimes(1);
    expect(b.run).toHaveBeenCalledTimes(1);
    // 之後 5 秒到了不會再送第二次。
    await vi.advanceTimersByTimeAsync(DEFER_MS);
    expect(a.run).toHaveBeenCalledTimes(1);
  });

  // Guards: REQ-59
  test("同一個編號等著的時候，再按一次不會排第二個", async () => {
    const first = defer("R-5");
    const second = defer("R-5");
    await vi.advanceTimersByTimeAsync(DEFER_MS);
    expect(first.run).toHaveBeenCalledTimes(1);
    expect(second.run).not.toHaveBeenCalled();
  });

  // Guards: REQ-59
  test("送出失敗：提示原因，不重抓資料，項目會回到清單", async () => {
    const run = vi.fn().mockRejectedValue(new Error("目前狀態不允許這項操作"));
    const settled = vi.fn();
    deferAction({ key: "R-6", message: "x", run, settled });
    await vi.advanceTimersByTimeAsync(DEFER_MS);
    expect(settled).not.toHaveBeenCalled();
    expect(toasts.at(-1)?.message).toContain("沒送出去");
  });
});
