import { describe, expect, test } from "vitest";
import {
  type CardioInput,
  cardioDetails,
  cardioSeries,
  checkCardio,
  emptyMetrics,
  metricsFor,
  paceFor,
  reviewCardio,
} from "./cardio";
import type { WorkoutLike } from "./coach";

const input = (dist: string, minutes: string, values: CardioInput["values"] = {}): CardioInput => ({
  dist,
  minutes,
  values,
});

function cw(
  over: Partial<WorkoutLike>,
  metrics: Partial<ReturnType<typeof emptyMetrics>> = {},
): WorkoutLike {
  return {
    id: "W-1",
    type: "跑步",
    date: "2026-10-21",
    started: "2026-10-21 07:00",
    feeling: "剛好",
    sets: [],
    distance_km: 0,
    duration_min: 0,
    pace: "",
    ...over,
    cardio: { ...emptyMetrics(), ...metrics },
  };
}

describe("有氧", () => {
  test("TestCardioPaceUnits", () => {
    // 跑步每公里、划船每 500 m、游泳每 100 m；騎車沒有配速
    expect(paceFor("跑步", 5, 30)).toBe("6:00");
    expect(paceFor("划船", 2, 8)).toBe("2:00");
    expect(paceFor("游泳", 1, 25)).toBe("2:30");
    expect(paceFor("騎車", 20, 48)).toBe("");
  });

  test("TestCardioInputChecks", () => {
    // 跑步：距離公里；心率、步頻有填才存；有步頻沒步幅就推步幅
    const run = checkCardio(
      "跑步",
      input("5", "30", { avg_hr: "150", max_hr: "172", cadence_spm: "170" }),
    );
    expect(run).toEqual({
      ok: true,
      error: "",
      fields: {
        distance_km: 5,
        duration_min: 30,
        pace: "6:00",
        avg_hr: 150,
        max_hr: 172,
        cadence_spm: 170,
        stride_m: 0.98,
      },
    });
    // 推出來的步幅不合理（距離或步頻填錯）就不存
    expect(
      checkCardio("跑步", input("50", "30", { cadence_spm: "100" })).fields.stride_m,
    ).toBeUndefined();
    expect(checkCardio("跑步", input("0.05", "30")).error).toBe("距離 0.1–100 km、時間 1–720 分");
    expect(checkCardio("跑步", input("5", "30", { avg_hr: "250" })).error).toBe(
      "平均心率 30–230 bpm（整數）",
    );
    expect(checkCardio("跑步", input("5", "30", { avg_hr: "160", max_hr: "150" })).error).toBe(
      "最大心率不能比平均心率低",
    );
    expect(checkCardio("跑步", input("", "30")).error).toBe("距離、時間要是數字");
    // 划船：距離公尺，存公里；配速每 500 m
    const row = checkCardio(
      "划船",
      input("2000", "8", { stroke_rate_spm: "26", avg_power_w: "180" }),
    );
    expect(row.fields).toEqual({
      distance_km: 2,
      duration_min: 8,
      pace: "2:00",
      stroke_rate_spm: 26,
      avg_power_w: 180,
    });
    expect(checkCardio("划船", input("50", "8")).error).toBe("距離 100–50000 m、時間 1–720 分");
    // 游泳：沒填距離時用池長×趟數；泳姿照存
    const swim = checkCardio("游泳", {
      ...input("", "25", { pool_m: "25", laps: "40", swolf: "38" }),
      stroke_style: "自由式",
    });
    expect(swim.fields).toEqual({
      distance_km: 1,
      duration_min: 25,
      pace: "2:30",
      pool_m: 25,
      laps: 40,
      swolf: 38,
      stroke_style: "自由式",
    });
    expect(checkCardio("游泳", input("", "25")).error).toBe("距離（或池長＋趟數）、時間要是數字");
    // 騎車：存均速、室內
    const ride = checkCardio("騎車", { ...input("20", "48", { cadence_rpm: "85" }), indoor: true });
    expect(ride.fields).toEqual({
      distance_km: 20,
      duration_min: 48,
      avg_speed_kmh: 25,
      cadence_rpm: 85,
      indoor: true,
    });
  });

  test("TestReviewCardio", () => {
    // 划船第一次：公尺、每 500 m 配速、心率
    const r1 = cw(
      { id: "W-2", type: "划船", distance_km: 2, duration_min: 8, pace: "2:00" },
      { avg_hr: 150, max_hr: 170 },
    );
    expect(reviewCardio(r1, [r1]).split("\n")).toEqual([
      "這次：2000 m、8 分，配速 2:00/500m。",
      "心率：平均 150、最大 170 bpm。",
      "這是第一筆划船，之後拿它當比較基準。",
      "下次：照這次的強度，穩定了再加距離。",
    ]);
    // 第二次：更快、心率更低 → 心肺進步；輕鬆 → 多划 500 m
    const r2 = cw(
      { id: "W-3", type: "划船", distance_km: 2, duration_min: 7.8, pace: "1:57", feeling: "輕鬆" },
      { avg_hr: 145 },
    );
    expect(reviewCardio(r2, [r2, r1]).split("\n")).toEqual([
      "這次：2000 m、7.8 分，配速 1:57/500m。",
      "心率：平均 145 bpm。",
      "跟上次比：配速快 3 秒/500m、平均心率低 5。",
      "一樣快（或更快）心率卻更低，心肺有進步。",
      "下次：可以多划 500 m，或把配速加快一點點（二選一，不要同時）。",
    ]);
    // 只跟同一個項目比：中間夾一筆跑步不影響
    const run = cw(
      { id: "W-4", type: "跑步", distance_km: 5, duration_min: 30, pace: "6:00" },
      { cadence_spm: 150 },
    );
    expect(reviewCardio(run, [run, r2, r1])).toContain("這是第一筆跑步");
    expect(reviewCardio(run, [run, r2, r1])).toContain("步頻 150 偏低");
    // 騎車看均速；吃力
    const b1 = cw({ id: "W-5", type: "騎車", distance_km: 20, duration_min: 48 });
    const b2 = cw(
      { id: "W-6", type: "騎車", distance_km: 25, duration_min: 50, feeling: "吃力" },
      { avg_speed_kmh: 30 },
    );
    expect(reviewCardio(b2, [b2, b1]).split("\n")).toEqual([
      "這次：25 km、50 分，均速 30 km/h。",
      "跟上次比：距離多 5 km、均速快 5 km/h。",
      "下次：速度或距離先不要加，睡好再騎。",
    ]);
  });

  test("TestCardioTrendsAndDetails", () => {
    const a = cw(
      {
        id: "W-1",
        type: "游泳",
        date: "2026-10-01",
        distance_km: 1,
        duration_min: 25,
        pace: "2:30",
      },
      { swolf: 40 },
    );
    const b = cw({
      id: "W-2",
      type: "游泳",
      date: "2026-10-08",
      distance_km: 1.2,
      duration_min: 28,
      pace: "2:20",
    });
    const metrics = metricsFor("游泳");
    expect(metrics.map((m) => m.label)).toEqual(["距離", "時間", "配速", "平均心率", "SWOLF"]);
    const pace = metrics.find((m) => m.key === "pace");
    if (!pace) throw new Error("no pace");
    // 舊→新；配速越低越好
    expect(cardioSeries([b, a], "游泳", pace)).toEqual([
      { id: "W-1", date: "2026-10-01", value: 150 },
      { id: "W-2", date: "2026-10-08", value: 140 },
    ]);
    expect(pace.lower).toBe(true);
    expect(pace.fmt(140)).toBe("2:20/100m");
    // 沒填的那次跳過
    const swolf = metrics.find((m) => m.key === "swolf");
    if (!swolf) throw new Error("no swolf");
    expect(cardioSeries([b, a], "游泳", swolf).map((p) => p.id)).toEqual(["W-1"]);
    expect(metricsFor("騎車").map((m) => m.key)).toContain("speed");
    // 歷史展開：只列有填的
    const d = cw(
      { type: "游泳" },
      { avg_hr: 140, swolf: 38, pool_m: 25, laps: 40, stroke_style: "蛙式" },
    );
    expect(cardioDetails(d)).toEqual(["平均心率 140 bpm", "SWOLF 38", "池長 25 m × 40 趟", "蛙式"]);
  });
});
