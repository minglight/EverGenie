import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

// 假的串流連線狀態：測試自己切 down，通知訂閱的人。
const link = vi.hoisted(() => {
  const listeners = new Set<() => void>();
  return {
    down: false,
    listeners,
    set(next: boolean) {
      this.down = next;
      for (const fn of listeners) fn();
    },
  };
});
vi.mock("@evergenie/sdk-web", () => ({
  streamsDown: () => link.down,
  onStreamsChange: (fn: () => void) => {
    link.listeners.add(fn);
    return () => link.listeners.delete(fn);
  },
}));

import { OFFLINE_AFTER_MS, useOffline } from "./connection";

beforeEach(() => {
  vi.useFakeTimers();
  link.down = false;
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("useOffline（DEV-156 U-21 頁首「現在連不上」）", () => {
  test("TestOfflineOnlyAfterDelay：斷一下就接回來不算，持續斷才說；連回來馬上收掉", () => {
    const { result } = renderHook(() => useOffline());
    expect(result.current).toBe(false);
    // 斷一下（畫面從背景回來、伺服器關掉串流）：1 秒內接回來，不出現。
    act(() => link.set(true));
    act(() => vi.advanceTimersByTime(1_000));
    act(() => link.set(false));
    act(() => vi.advanceTimersByTime(OFFLINE_AFTER_MS));
    expect(result.current).toBe(false);
    // 真的連不上：過了 OFFLINE_AFTER_MS 才出現。
    act(() => link.set(true));
    act(() => vi.advanceTimersByTime(OFFLINE_AFTER_MS - 1));
    expect(result.current).toBe(false);
    act(() => vi.advanceTimersByTime(1));
    expect(result.current).toBe(true);
    // 連回來：馬上收掉，不等。
    act(() => link.set(false));
    expect(result.current).toBe(false);
  });
});
