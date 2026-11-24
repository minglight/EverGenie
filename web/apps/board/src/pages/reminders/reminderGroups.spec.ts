import { describe, expect, test } from "vitest";
import { groupKeyOf, pageViewOf, recentlyDone } from "./reminderGroups";

// 2026-11-04 是週三。
const now = new Date(2026, 10, 4, 10, 0);
const at = (m: number, d: number, h = 9) => new Date(2026, m - 1, d, h, 0);

describe("待辦依時間分組", () => {
  test("今天以前逾期；今天稍早的還算今天", () => {
    expect(groupKeyOf(at(11, 3, 23), now)).toBe("late");
    expect(groupKeyOf(at(11, 4, 8), now)).toBe("today");
    expect(groupKeyOf(at(11, 4, 23), now)).toBe("today");
  });
  test("明天到週日是這週，下週一起是之後，沒有時間單獨一組", () => {
    expect(groupKeyOf(at(11, 5), now)).toBe("week");
    expect(groupKeyOf(at(11, 8, 23), now)).toBe("week");
    expect(groupKeyOf(at(11, 9, 0), now)).toBe("later");
    expect(groupKeyOf(null, now)).toBe("none");
  });
  test("今天是週日時，這週只剩今天，明天就是之後", () => {
    const sunday = new Date(2026, 10, 8, 10, 0);
    expect(groupKeyOf(at(11, 9), sunday)).toBe("later");
  });
});

describe("待辦網址的舊分段", () => {
  test("今天、逾期、這週、之後、沒時間都進「未完成」，昨天做完進「已完成」", () => {
    for (const v of ["today", "overdue", "this_week", "upcoming", "no_time", undefined, "open"]) {
      expect(pageViewOf(v)).toBe("open");
    }
    expect(pageViewOf("done_yesterday")).toBe("done");
    expect(pageViewOf("waiting")).toBe("waiting");
  });
});

describe("已完成只列最近 7 天", () => {
  const day = 86_400_000;
  const iso = (ms: number) => new Date(ms).toISOString();
  test("剛好 7 天內算，超過不算", () => {
    expect(recentlyDone({ closed_at: iso(now.getTime() - 6.9 * day) }, now)).toBe(true);
    expect(recentlyDone({ closed_at: iso(now.getTime() - 7 * day) }, now)).toBe(true);
    expect(recentlyDone({ closed_at: iso(now.getTime() - 7.1 * day) }, now)).toBe(false);
  });
  test("closed_at 缺值或零值時退到 updated_at；都沒有就算最近，不讓做完的待辦無聲消失", () => {
    const recent = iso(now.getTime() - day);
    const old = iso(now.getTime() - 30 * day);
    expect(recentlyDone({ updated_at: recent }, now)).toBe(true);
    expect(recentlyDone({ closed_at: "0001-01-01T00:00:00Z", updated_at: recent }, now)).toBe(true);
    expect(recentlyDone({ closed_at: "0001-01-01T00:00:00Z", updated_at: old }, now)).toBe(false);
    expect(recentlyDone({}, now)).toBe(true);
  });
});
