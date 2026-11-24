import { describe, expect, it } from "vitest";
import {
  BIG_BACKUP_BYTES,
  dayTimeText,
  hasTime,
  kindKey,
  lostLinksShown,
  restorable,
  restoreFinished,
  skippedText,
  totalOf,
  usageIsBig,
} from "./backupView";

describe("整個家的備份：畫面的純函式", () => {
  it("時間零值算還沒有", () => {
    expect(hasTime("0001-01-01T00:00:00Z")).toBe(false);
    expect(hasTime(undefined)).toBe(false);
    expect(hasTime("2026-11-16T02:30:00Z")).toBe(true);
    expect(dayTimeText("0001-01-01T00:00:00Z")).toBe("");
    expect(dayTimeText("2026-11-16T02:30:00Z")).toContain("2026");
  });

  it("種類的白話：不認得的當手動", () => {
    expect(kindKey("daily")).toBe("settings.backup.kind.daily");
    expect(kindKey("before-restore")).toBe("settings.backup.kind.beforeRestore");
    expect(kindKey("manual")).toBe("settings.backup.kind.manual");
    expect(kindKey(undefined)).toBe("settings.backup.kind.manual");
  });

  it("還原清單新的在前，沒名字或沒時間的不列", () => {
    const list = restorable([
      { name: "a.egbackup", size: 1, created_at: "2026-11-10T00:00:00Z" },
      { name: "b.egbackup", size: 1, created_at: "2026-11-15T00:00:00Z" },
      { size: 1, created_at: "2026-11-16T00:00:00Z" },
      { name: "c.egbackup", size: 1, created_at: "0001-01-01T00:00:00Z" },
    ]);
    expect(list.map((f) => f.name)).toEqual(["b.egbackup", "a.egbackup"]);
    expect(restorable(undefined)).toEqual([]);
  });

  it("等還原做完：服務還沒回來、還在等重開、結果還是上一筆，都繼續等", () => {
    const base = {
      last_at: "",
      auto_error_at: "",
      keep_count: 7,
      keep_days: 30,
      daily_keep: 7,
      free_bytes: 0,
      local: true,
    };
    const old = { ok: true, at: "2026-11-01T00:00:00Z", backup_created_at: "" };
    const fresh = { ok: false, at: "2026-11-16T03:00:00Z", backup_created_at: "" };
    expect(restoreFinished("", undefined)).toBeNull();
    expect(restoreFinished("", { ...base, restoring: true, last_restore: fresh })).toBeNull();
    expect(restoreFinished(old.at, { ...base, restoring: false, last_restore: old })).toBeNull();
    expect(restoreFinished("", { ...base, restoring: false })).toBeNull();
    expect(restoreFinished(old.at, { ...base, restoring: false, last_restore: fresh })).toBe(fresh);
    expect(restoreFinished("", { ...base, restoring: false, last_restore: old })).toBe(old);
  });

  it("備份一共幾份、多大；沒收的最多列三個", () => {
    expect(totalOf(undefined)).toEqual({ count: 0, bytes: 0 });
    expect(
      totalOf([
        { name: "a", size: 10, created_at: "" },
        { name: "b", size: 5, created_at: "" },
      ]),
    ).toEqual({ count: 2, bytes: 15 });
    expect(skippedText(["a", "b"])).toBe("a、b");
    expect(skippedText(["a", "b", "c", "d"])).toBe("a、b、c 等");
  });

  it("備份佔用大才提醒：超過 10 GB，或已經比硬碟剩下的還多；不知道剩多少只看總量", () => {
    expect(usageIsBig(0, 0)).toBe(false);
    expect(usageIsBig(5 * 2 ** 30, 0)).toBe(false);
    expect(usageIsBig(5 * 2 ** 30, undefined)).toBe(false);
    expect(usageIsBig(5 * 2 ** 30, 100 * 2 ** 30)).toBe(false);
    expect(usageIsBig(BIG_BACKUP_BYTES, 500 * 2 ** 30)).toBe(true);
    expect(usageIsBig(5 * 2 ** 30, 4 * 2 ** 30)).toBe(true);
  });

  it("還原後會不見的捷徑：最多列 8 個，其餘只說還有幾個", () => {
    expect(lostLinksShown(undefined)).toEqual({ shown: [], more: 0 });
    expect(lostLinksShown(["vault/a", "media/b"])).toEqual({
      shown: ["vault/a", "media/b"],
      more: 0,
    });
    const many = Array.from({ length: 11 }, (_, i) => `vault/l${i}`);
    expect(lostLinksShown(many)).toEqual({ shown: many.slice(0, 8), more: 3 });
  });
});
