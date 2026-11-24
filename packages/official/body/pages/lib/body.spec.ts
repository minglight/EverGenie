import { describe, expect, test } from "vitest";
import {
  type BodyEntry,
  checkMetric,
  describeBody,
  latestDateOf,
  measuredLabel,
  summarize,
  toBody,
  trendLine,
} from "./body";

function b(date: string, weight: number | null, fat: number | null = null): BodyEntry {
  return { date, weight, fat, bmi: null, bmr: null, visceral: null, score: null, note: "" };
}

describe("身體數據", () => {
  // Guards: REQ-129
  test("TestBodyWeeklyAverage", () => {
    const today = "2026-10-25";
    const items = [
      // 近 7 天：10/19–10/25
      b("2026-10-25", 72, 20),
      b("2026-10-23", 73),
      b("2026-10-19", 74, 22),
      // 前 7 天：10/12–10/18
      b("2026-10-18", 75, 24),
      b("2026-10-12", 76),
      // 更早的不算
      b("2026-10-11", 90),
    ];
    const s = summarize(items, today);
    expect(s.latest?.date).toBe("2026-10-25");
    expect(s.weight).toEqual({ avg7: 73, prev7: 75.5, delta: -2.5, latest: 72 });
    expect(s.fat).toEqual({ avg7: 21, prev7: 24, delta: -3, latest: 20 });
    // 前 7 天沒有資料：沒有差
    expect(summarize([b("2026-10-25", 72)], today).weight).toEqual({
      avg7: 72,
      prev7: null,
      delta: null,
      latest: 72,
    });
    // 近 7 天沒有資料：只有最新值
    expect(summarize([b("2026-10-10", 72)], today).weight).toEqual({
      avg7: null,
      prev7: null,
      delta: null,
      latest: 72,
    });
    expect(summarize([], today).latest).toBeNull();
  });

  // 今天頁「今天的身體」卡片：最近一筆不是今天量的要標日期，不然三週前的體重看起來像今天的（BOD-5）。
  test("TestTodayCardSaysWhenWeighed", () => {
    const today = "2026-11-03";
    expect(measuredLabel("2026-11-03", today)).toBe("");
    expect(measuredLabel("2026-11-02", today)).toBe("昨天");
    expect(measuredLabel("2026-10-13", today)).toBe("10/13 週二");
    // 跨月、跨年的「昨天」照樣認得。
    expect(measuredLabel("2025-12-31", "2026-01-01")).toBe("昨天");
    // 最新一筆有體重的日期：只量了體脂的那天不算。
    const items = [b("2026-11-01", 72), b("2026-11-03", null, 18)];
    expect(latestDateOf(items, "weight")).toBe("2026-11-01");
    expect(latestDateOf(items, "fat")).toBe("2026-11-03");
    expect(latestDateOf([], "weight")).toBeNull();
  });

  // Guards: REQ-129
  test("TestBodyTrendAndInput", () => {
    const items = [
      b("2026-10-25", 72),
      b("2026-10-20", 74),
      b("2026-10-24", null, 20),
      b("2026-10-10", 80),
    ];
    // 只畫有體重的日子，舊→新；7 天平均含當天往前 6 天
    expect(trendLine(items, "weight")).toEqual([
      { date: "2026-10-10", v: 80, avg: 80 },
      { date: "2026-10-20", v: 74, avg: 74 },
      { date: "2026-10-25", v: 72, avg: 73 },
    ]);
    expect(trendLine(items, "fat")).toEqual([{ date: "2026-10-24", v: 20, avg: 20 }]);
    // 輸入檢查
    expect(checkMetric("weight", "72.46")).toEqual({ value: 72.5, error: "" });
    expect(checkMetric("weight", " ")).toEqual({ value: null, error: "" });
    expect(checkMetric("weight", "abc").error).toBe("體重要是數字");
    expect(checkMetric("weight", "5").error).toBe("體重要在 20–300");
    expect(checkMetric("fat", "70").error).toBe("體脂要在 3–60");
    expect(
      toBody({ id: "2026-10-25", fields: { weight: 72.4, fat: null, note: "早上" } }),
    ).toMatchObject({
      date: "2026-10-25",
      weight: 72.4,
      fat: null,
      note: "早上",
    });
    expect(describeBody({ ...b("2026-10-25", 72.4, 18.5), bmr: 1454 })).toBe(
      "體重 72.4 kg　體脂 18.5%　基礎代謝 1454 大卡",
    );
  });
});
