import { describe, expect, test } from "vitest";
import {
  canGoNext,
  dateFromSearch,
  dayTotals,
  emptyDayHint,
  guessMeal,
  type Meal,
  mealFieldsFromFav,
  mealsOfDay,
  noGoalText,
  progress,
  recentAverage,
  shiftDate,
  toMeal,
  trendDays,
} from "./nutrition";

function meal(over: Partial<Meal>): Meal {
  return {
    id: "M-1",
    date: "2026-10-25",
    at: "2026-10-25 12:00",
    meal: "午餐",
    kcal: 0,
    protein: 0,
    carb: 0,
    fat: 0,
    items: [],
    photos: [],
    conf: "",
    note: "",
    fav: "",
    ...over,
  };
}

describe("飲食的純函式", () => {
  // Guards: REQ-128
  test("TestDayTotals", () => {
    const meals = [
      meal({ id: "M-1", kcal: 500, protein: 30, carb: 60, fat: 15 }),
      meal({ id: "M-2", kcal: 250.5, protein: 10, carb: 20, fat: 5 }),
      meal({ id: "M-3", date: "2026-10-24", kcal: 999, protein: 99 }),
    ];
    expect(dayTotals(meals, "2026-10-25")).toEqual({
      kcal: 750.5,
      protein: 40,
      carb: 80,
      fat: 20,
      count: 2,
    });
    expect(dayTotals(meals, "2026-10-20")).toEqual({
      kcal: 0,
      protein: 0,
      carb: 0,
      fat: 0,
      count: 0,
    });
    // 目標進度：沒設目標（0）回 null，超過目標標 over，不是錯誤。
    expect(progress(750, 0)).toBeNull();
    expect(progress(1000, 2000)).toEqual({ ratio: 0.5, over: false, left: 1000 });
    expect(progress(2100, 2000)?.over).toBe(true);
    // 餐點依 at 排，同時間依編號數字排（M-2 在 M-10 前）。
    const sorted = mealsOfDay(
      [
        meal({ id: "M-10", at: "2026-10-25 08:00" }),
        meal({ id: "M-2", at: "2026-10-25 08:00" }),
        meal({ id: "M-3", at: "2026-10-25 07:00" }),
      ],
      "2026-10-25",
    );
    expect(sorted.map((m) => m.id)).toEqual(["M-3", "M-2", "M-10"]);
    // 從 vault 記錄轉成餐點：缺的欄位補 0／空字串，items 收得下 list of map。
    const m = toMeal({
      id: "M-7",
      fields: {
        date: "2026-10-25",
        kcal: 400,
        items: [{ name: "飯", qty: "1 碗", kcal: 200 }],
        photos: ["/share/a.jpg"],
      },
    });
    expect(m.protein).toBe(0);
    expect(m.items[0]).toMatchObject({ name: "飯", qty: "1 碗", kcal: 200, fat: 0 });
    expect(m.photos).toEqual(["/share/a.jpg"]);
  });

  // Guards: REQ-128
  test("TestGuessMeal", () => {
    const at = (h: number, min = 0) => new Date(2026, 9, 25, h, min);
    expect(guessMeal(at(4, 59))).toBe("宵夜");
    expect(guessMeal(at(5))).toBe("早餐");
    expect(guessMeal(at(9, 59))).toBe("早餐");
    expect(guessMeal(at(10))).toBe("午餐");
    expect(guessMeal(at(13, 59))).toBe("午餐");
    expect(guessMeal(at(14))).toBe("點心");
    expect(guessMeal(at(16, 59))).toBe("點心");
    expect(guessMeal(at(17))).toBe("晚餐");
    expect(guessMeal(at(20, 59))).toBe("晚餐");
    expect(guessMeal(at(21))).toBe("宵夜");
    expect(guessMeal(at(0))).toBe("宵夜");
    // 常吃餐點「記一筆」：日期、時間、餐別依現在時間，品項與營養照抄並記下來源。
    const fields = mealFieldsFromFav(
      { id: "F-1", name: "雞腿便當", kcal: 680, protein: 42, carb: 70, fat: 22, items: [] },
      new Date(2026, 9, 25, 12, 5),
    );
    expect(fields).toMatchObject({
      date: "2026-10-25",
      at: "2026-10-25 12:05",
      meal: "午餐",
      kcal: 680,
      fav: "F-1",
    });
  });

  // Guards: REQ-128
  test("TestTrendSkipsEmptyDays", () => {
    const meals = [
      meal({ id: "M-1", date: "2026-10-25", kcal: 1800, protein: 90 }),
      meal({ id: "M-2", date: "2026-10-25", kcal: 200, protein: 10 }),
      meal({ id: "M-3", date: "2026-10-23", kcal: 1500, protein: 70 }),
      meal({ id: "M-4", date: "2026-10-01", kcal: 5000, protein: 5 }), // 14 天之外
    ];
    const days = trendDays(meals, "2026-10-25", 14);
    expect(days).toHaveLength(14);
    expect(days[0]?.date).toBe("2026-10-12");
    expect(days[13]?.date).toBe("2026-10-25");
    // 沒記錄的日子是 null，不是 0。
    expect(days[12]).toEqual({ date: "2026-10-24", kcal: null, protein: null, count: 0 });
    expect(days[13]).toEqual({ date: "2026-10-25", kcal: 2000, protein: 100, count: 2 });
    expect(days.filter((d) => d.kcal !== null)).toHaveLength(2);
    // 近 7 天平均：只除以有記錄的 2 天（不是 7），14 天之外的不算。
    expect(recentAverage(days, 7)).toEqual({ kcal: 1750, protein: 85, days: 2 });
    expect(recentAverage(trendDays([], "2026-10-25", 14), 7)).toBeNull();
    // 跨月、跨年的日期運算。
    expect(shiftDate("2026-03-01", -1)).toBe("2026-02-28");
    expect(shiftDate("2026-12-31", 1)).toBe("2027-01-01");
    expect(dateFromSearch("?date=2026-10-20", new Date(2026, 9, 25))).toBe("2026-10-20");
    expect(dateFromSearch("?date=2026-13-40", new Date(2026, 9, 25))).toBe("2026-10-25");
    expect(dateFromSearch("", new Date(2026, 9, 25))).toBe("2026-10-25");
  });

  // 「後一天」只走到今天為止，不讓人按到明天的空白頁；跨月、跨年也對（NUT-8）。
  test("TestNextDayStopsAtToday", () => {
    expect(canGoNext("2026-11-02", "2026-11-03")).toBe(true);
    expect(canGoNext("2026-11-03", "2026-11-03")).toBe(false);
    expect(canGoNext("2026-11-04", "2026-11-03")).toBe(false);
    expect(canGoNext("2025-12-31", "2026-01-01")).toBe(true);
    expect(canGoNext("2026-01-31", "2026-02-01")).toBe(true);
  });

  // 看別天時不說「今天累計」；沒有常吃餐點時不叫人「點上面的常吃餐點」（NUT-9）。
  test("TestDayTextsFollowTheShownDay", () => {
    const t = { kcal: 512.25, protein: 30, carb: 0, fat: 0 };
    expect(noGoalText("2026-11-03", "2026-11-03", t)).toBe(
      "還沒設目標。今天累計 512.3 大卡、蛋白質 30 公克。",
    );
    expect(noGoalText("2026-11-01", "2026-11-03", t)).toBe(
      "還沒設目標。這天累計 512.3 大卡、蛋白質 30 公克。",
    );
    expect(emptyDayHint("", false)).toBe("請秘書幫你記");
    expect(emptyDayHint("", true)).toBe("請秘書幫你記，或點上面的常吃餐點");
    expect(emptyDayHint("小愛", false)).toBe("把餐點照片貼給 小愛，就會幫你估算並記下來");
  });
});
