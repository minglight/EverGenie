import { afterEach, describe, expect, test, vi } from "vitest";
import { ALL_KINDS, emitVaultChanged, onLiveChange, onVaultChanged } from "./vaultEvents";

// 套件頁面靠看板轉來的「記錄變了」自己重讀（PKG-49）。
describe("記錄變了的通知", () => {
  afterEach(() => vi.useRealTimers());

  test("TestPackagePageHearsOnlyItsOwnKinds", () => {
    const got: string[] = [];
    const off = onVaultChanged("nutrition.", (c) => got.push(`${c.kind}/${c.id}`));
    emitVaultChanged({ kind: "nutrition.meal", id: "M-3" });
    emitVaultChanged({ kind: "fitness.workout", id: "W-1" });
    emitVaultChanged({ kind: "task", id: "SEC-1" });
    // 種類名稱只是開頭一樣不算（nutritionx 不是 nutrition 套件的）。
    emitVaultChanged({ kind: "nutritionx.meal", id: "X-1" });
    emitVaultChanged({ kind: "" });
    off();
    emitVaultChanged({ kind: "nutrition.goal", id: "main" });
    expect(got).toEqual(["nutrition.meal/M-3"]);
  });

  // 看板串流斷線重連（手機鎖屏回來）：不知道漏了什麼，每個套件頁面都重讀。
  test("TestReconnectReloadsEveryPackagePage", () => {
    const got: string[] = [];
    const offA = onVaultChanged("nutrition.", (c) => got.push(`n:${c.kind}`));
    const offB = onVaultChanged("body.", (c) => got.push(`b:${c.kind}`));
    emitVaultChanged({ kind: ALL_KINDS });
    offA();
    offB();
    expect(got).toEqual(["n:*", "b:*"]);
  });

  test("TestBurstOfChangesReloadsOnce", () => {
    vi.useFakeTimers();
    const fn = vi.fn();
    const off = onLiveChange("body.", fn, 300);
    emitVaultChanged({ kind: "body.entry", id: "2026-11-03" });
    vi.advanceTimersByTime(200);
    emitVaultChanged({ kind: "body.entry", id: "2026-11-02" });
    vi.advanceTimersByTime(200);
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(150);
    expect(fn).toHaveBeenCalledTimes(1);
    // 取消訂閱之後，排好但還沒到的那次也不叫。
    emitVaultChanged({ kind: "body.entry", id: "2026-11-01" });
    off();
    vi.advanceTimersByTime(1000);
    expect(fn).toHaveBeenCalledTimes(1);
  });
});
