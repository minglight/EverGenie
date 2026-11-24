import { describe, expect, test } from "vitest";
import { emptyMetrics } from "./cardio";
import { MUSCLES } from "./coach";
import {
  activeWorkout,
  checkSet,
  type Equip,
  frequentEquipment,
  frequentExcept,
  groupByPart,
  groupSets,
  initialPick,
  isStale,
  lastSetOf,
  matchEquipment,
  parseCues,
  parseMuscles,
  parseSetInput,
  partOfMuscle,
  partsOf,
  planSet,
  recentInWorkout,
  staleCloseFields,
  strengthSeries,
  summaryOf,
  toEquip,
  toWorkout,
  type Workout,
  weekCardioCount,
  weeklyTotals,
} from "./workout";

function w(over: Partial<Workout>): Workout {
  return {
    id: "W-1",
    type: "重訓",
    date: "2026-10-25",
    started: "2026-10-25 09:00",
    finished: "",
    status: "進行中",
    feeling: "",
    note: "",
    sets: [],
    distance_km: 0,
    duration_min: 0,
    pace: "",
    source: "",
    review: "",
    review_by: "",
    updatedAt: "",
    cardio: emptyMetrics(),
    ...over,
  };
}
const set = (equipment: string, weight = 10, reps = 10) => ({
  equipment,
  weight,
  reps,
  feel: "剛好",
});
const eqp = (name: string, primary: string[] = [], type = ""): Equip => ({
  id: `E-${name}`,
  name,
  primary,
  secondary: [],
  type,
  seat: "",
  cues: [],
  unit: "",
});

describe("進行中的訓練", () => {
  // 今天頁的卡片用同一把尺：超過 8 小時沒動就寫「上次的訓練還沒收操」，不再一直寫「進行中」（FIT-15）。
  test("TestForgottenWorkoutIsStaleOnTodayCard", () => {
    const now = new Date(2026, 9, 25, 20, 0);
    expect(
      isStale(
        w({ sets: [set("啞鈴彎舉")], updatedAt: new Date(2026, 9, 25, 13, 0).toISOString() }),
        now,
      ),
    ).toBe(false);
    expect(
      isStale(
        w({ sets: [set("啞鈴彎舉")], updatedAt: new Date(2026, 9, 24, 19, 0).toISOString() }),
        now,
      ),
    ).toBe(true);
    expect(isStale(w({ started: "2026-10-25 11:00" }), now)).toBe(true);
  });

  // Guards: REQ-127
  test("TestStaleWorkoutStartsNew", () => {
    const now = new Date(2026, 9, 25, 20, 0);
    // 沒有進行中的重訓：開新的，沒有舊的要收
    expect(planSet([w({ status: "完成" })], now)).toEqual({ stale: null, active: null });
    // 進行中、最後一組 7 小時前：接在後面
    const fresh = w({
      sets: [set("啞鈴彎舉")],
      updatedAt: new Date(2026, 9, 25, 13, 0).toISOString(),
    });
    expect(planSet([fresh], now)).toEqual({ stale: null, active: fresh });
    // 最後一組超過 8 小時：舊的標完成、開新的
    const stale = w({
      sets: [set("啞鈴彎舉")],
      updatedAt: new Date(2026, 9, 25, 11, 59).toISOString(),
    });
    expect(planSet([stale], now)).toEqual({ stale, active: null });
    // 沒有組就看開始時間
    expect(planSet([w({ started: "2026-10-25 11:00" })], now).stale).not.toBeNull();
    expect(planSet([w({ started: "2026-10-25 13:00" })], now).stale).toBeNull();
    // 跑步不算「進行中的重訓」
    expect(activeWorkout([w({ type: "跑步" })])).toBeNull();

    // 舊的那筆被標完成：備註加「忘了收操」、檢討用規則算
    const eqm = new Map([["啞鈴彎舉", eqp("啞鈴彎舉", ["二頭肌"])]]);
    const fields = staleCloseFields(stale, [stale], eqm, "2026-10-25");
    expect(fields).toMatchObject({ status: "完成", note: "忘了收操", review_by: "規則" });
    expect(String(fields.review)).toContain("練了：二頭肌 1 組。");
    expect(staleCloseFields({ ...stale, note: "腿痠" }, [stale], eqm, "2026-10-25").note).toBe(
      "腿痠 忘了收操",
    );
  });

  // Guards: REQ-127
  test("TestEquipmentPickerRules", () => {
    const list = [
      eqp("A", ["胸"]),
      eqp("B", ["背闊肌"]),
      eqp("C"),
      eqp("D"),
      eqp("E"),
      eqp("F"),
      eqp("G"),
      eqp("H", [], "徒手"),
    ];
    const ws = [
      w({
        id: "W-2",
        status: "完成",
        started: "2026-10-25 09:00",
        sets: [set("G"), set("G"), set("B")],
      }),
      w({ id: "W-1", status: "完成", started: "2026-10-24 09:00", sets: [set("B")] }),
    ];
    // 組數多的在前（G 2、B 2 → 依清單順序 B 在 G 前），不足依清單補
    expect(frequentEquipment(ws, list)).toEqual(["B", "G", "A", "C", "D", "E"]);
    // 只看最近 12 次重訓
    const many = Array.from({ length: 13 }, (_, i) =>
      w({
        id: `W-${i + 1}`,
        status: "完成",
        started: `2026-10-${String(i + 1).padStart(2, "0")} 09:00`,
        sets: [set(i === 0 ? "H" : "A")],
      }),
    );
    expect(frequentEquipment(many, list)).toEqual(["A", "B", "C", "D", "E", "F"]);
    // 搜尋名稱或肌群
    expect(matchEquipment(list, "背闊").map((e) => e.name)).toEqual(["B"]);
    expect(matchEquipment(list, "C").map((e) => e.name)).toEqual(["C"]);
    expect(matchEquipment(list, "")).toHaveLength(list.length);
    // 預設帶最近一組
    expect(lastSetOf(ws, "G")?.set.reps).toBe(10);
    expect(lastSetOf(ws, "Z")).toBeNull();
  });

  // Guards: REQ-127
  test("TestInputChecks", () => {
    expect(checkSet(40, 10).ok).toBe(true);
    expect(checkSet(0, 30).ok).toBe(true);
    expect(checkSet(-1, 10).ok).toBe(false);
    expect(checkSet(40, 0).ok).toBe(false);
    expect(checkSet(40, 10.5).ok).toBe(false);
    expect(checkSet(Number.NaN, 10).error).toBe("重量、次數要是數字");
    expect(parseMuscles("二頭肌、胸 肩,不存在")).toEqual({
      ok: ["二頭肌", "胸", "肩"],
      bad: ["不存在"],
    });
    // vault 記錄換成型別：數字字串、缺欄位都不會炸
    const rec = toWorkout({
      id: "W-9",
      fields: { type: "重訓", sets: [{ equipment: "A", weight: "12.5", reps: 8, feel: "剛好" }] },
      updated_at: "x",
    });
    expect(rec.sets[0]).toEqual({ equipment: "A", weight: 12.5, reps: 8, feel: "剛好" });
    expect(rec.status).toBe("");
  });

  test("TestStrengthTimeline", () => {
    // 時間軸：舊→新，每次訓練一個點；最大重量、估算 1RM、總量。
    const ws = [
      w({
        id: "W-1",
        date: "2026-10-01",
        status: "完成",
        sets: [set("腿推", 100, 10), set("腿推", 110, 8)],
      }),
      w({
        id: "W-2",
        date: "2026-10-08",
        started: "2026-10-08 09:00",
        status: "完成",
        sets: [set("腿推", 120, 5), set("棒式", 0, 60)],
      }),
      w({
        id: "W-3",
        type: "跑步",
        date: "2026-10-09",
        started: "2026-10-09 09:00",
        status: "完成",
        distance_km: 5,
        duration_min: 30,
      }),
    ];
    expect(strengthSeries(ws, "腿推", "max")).toEqual([
      { id: "W-1", date: "2026-10-01", value: 110 },
      { id: "W-2", date: "2026-10-08", value: 120 },
    ]);
    // Epley：100×(1+10/30)=133.3→133.5；110×(1+8/30)=139.3→139.5；120×(1+5/30)=140
    expect(strengthSeries(ws, "腿推", "e1rm").map((p) => p.value)).toEqual([139.5, 140]);
    expect(strengthSeries(ws, "腿推", "volume").map((p) => p.value)).toEqual([1880, 600]);
    // 徒手改看次數
    expect(strengthSeries(ws, "棒式", "max").map((p) => p.value)).toEqual([60]);
    // 有負重的器材，只記了徒手組的那次不畫（單位一致）；只做一下時 1RM 就是那個重量
    const mixed = [
      ...ws,
      w({
        id: "W-6",
        date: "2026-10-09",
        started: "2026-10-09 18:00",
        status: "完成",
        sets: [set("腿推", 0, 20)],
      }),
      w({
        id: "W-7",
        date: "2026-10-10",
        started: "2026-10-10 18:00",
        status: "完成",
        sets: [set("腿推", 130, 1)],
      }),
    ];
    expect(strengthSeries(mixed, "腿推", "max").map((p) => p.id)).toEqual(["W-1", "W-2", "W-7"]);
    expect(strengthSeries(mixed, "腿推", "e1rm").at(-1)?.value).toBe(130);
    // 每週總量：重訓算 kg、有氧算分鐘；沒練的週是 0
    const wk = weeklyTotals(ws, "2026-10-10", "重訓", 3);
    expect(wk).toEqual([
      { week: "2026-09-21", value: 0 },
      { week: "2026-09-28", value: 1880 },
      { week: "2026-10-05", value: 600 },
    ]);
    expect(weeklyTotals(ws, "2026-10-10", "有氧", 1)).toEqual([{ week: "2026-10-05", value: 30 }]);
    expect(weekCardioCount(ws, "2026-10-05")).toBe(1);
  });

  test("TestCardioSummaryInHistory", () => {
    const run = w({
      type: "跑步",
      status: "完成",
      distance_km: 5.5,
      duration_min: 32.5,
      pace: "5:55",
    });
    expect(summaryOf(run)).toBe("5.5 km · 32.5 分 · 5:55/km");
    const swim = toWorkout({
      id: "W-4",
      fields: {
        type: "游泳",
        distance_km: 1,
        duration_min: 25,
        pace: "2:30",
        avg_hr: 140,
        pool_m: 25,
        laps: 40,
      },
    });
    expect(summaryOf(swim)).toBe("1000 m · 25 分 · 2:30/100m · ♥ 140");
    expect(swim.cardio.laps).toBe(40);
    const ride = toWorkout({
      id: "W-5",
      fields: { type: "騎車", distance_km: 20, duration_min: 48 },
    });
    expect(summaryOf(ride)).toBe("20 km · 48 分 · 25 km/h");
  });

  // 重量空白不能被當成 0 kg 記成「徒手」；次數空白直接說要填（FIT-13）。
  test("TestEmptyWeightIsNotZeroKg", () => {
    expect(parseSetInput("", "10", false)).toMatchObject({ ok: false, error: "重量要填" });
    expect(parseSetInput("  ", "10", false).error).toBe("重量要填");
    expect(parseSetInput("40", "", false)).toMatchObject({ ok: false, error: "次數要填" });
    expect(parseSetInput("", "", true, "秒數").error).toBe("秒數要填");
    expect(parseSetInput("", "30", true, "秒數")).toEqual({
      ok: true,
      error: "",
      weight: 0,
      reps: 30,
    });
    expect(parseSetInput("22.5", "8", false)).toEqual({
      ok: true,
      error: "",
      weight: 22.5,
      reps: 8,
    });
    // 填了 0 是刻意的（例如只用槓），照樣記。
    expect(parseSetInput("0", "8", false).ok).toBe(true);
    expect(parseSetInput("abc", "8", false).error).toBe("重量、次數要是數字");
  });
});

// Guards: FIT-17
describe("TestEquipmentPickerParts", () => {
  const list = [
    eqp("划船", ["菱形肌", "背闊肌"]),
    eqp("腿伸", ["股四頭"]),
    eqp("飛鳥", ["後三角", "菱形肌"]),
    eqp("神祕", []),
    eqp("單腳站", ["臀中肌"], "徒手"),
  ];
  test("肌群歸到部位，器材照主練肌群可以在兩個部位，沒有肌群是其他", () => {
    expect(partOfMuscle("股四頭")).toBe("腿臀");
    expect(partOfMuscle("前鋸肌")).toBe("肩");
    expect(partOfMuscle("平衡")).toBe("其他");
    expect(partsOf(list[2] as Equip)).toEqual(["背", "肩"]);
    expect(partsOf(list[3] as Equip)).toEqual(["其他"]);
    expect(groupByPart(list).map((g) => [g.part, g.items.map((e) => e.name)])).toEqual([
      ["腿臀", ["腿伸", "單腳站"]],
      ["背", ["划船", "飛鳥"]],
      ["肩", ["飛鳥"]],
      ["其他", ["神祕"]],
    ]);
  });
  test("名單上的每個肌群都有部位，只有頸部與平衡刻意歸到其他", () => {
    const other = MUSCLES.filter((m) => partOfMuscle(m) === "其他");
    expect(other).toEqual(["深頸屈肌", "胸鎖乳突肌", "提肩胛肌", "平衡"]);
  });
  test("剛才在用：這次用過的，最近的在前；常用扣掉它們", () => {
    const active = w({ sets: [set("划船"), set("腿伸"), set("划船"), set("已刪的")] });
    expect(recentInWorkout(active, list)).toEqual(["划船", "腿伸"]);
    expect(recentInWorkout(null, list)).toEqual([]);
    const ws = [active];
    expect(frequentExcept(ws, list, ["划船", "腿伸"], 2)).toEqual(["飛鳥", "神祕"]);
  });
  test("進行中的組：連續同一台併成一段", () => {
    const g = groupSets([set("A"), set("A"), set("B"), set("A")]);
    expect(g.map((x) => [x.equipment, x.sets.length])).toEqual([
      ["A", 2],
      ["B", 1],
      ["A", 1],
    ]);
  });
});

// Guards: FIT-18
describe("TestEquipmentRemembered", () => {
  const list = [eqp("划船", ["背闊肌"]), eqp("腿伸", ["股四頭"])];
  const now = new Date("2026-11-09T10:00:00");
  const hoursAgo = (h: number) => now.getTime() - h * 3600 * 1000;
  test("記住的器材 8 小時內、還在清單裡就預選", () => {
    expect(initialPick({ name: "腿伸", at: hoursAgo(1) }, [], list, now)).toBe("腿伸");
    expect(initialPick({ name: "腿伸", at: hoursAgo(9) }, [], list, now)).toBe("");
    expect(initialPick({ name: "已刪的", at: hoursAgo(1) }, [], list, now)).toBe("");
  });
  test("沒記住：進行中的訓練就用最後一組的器材；忘了收操的不算", () => {
    const active = w({
      started: "2026-11-09 09:30",
      updatedAt: "2026-11-09T09:50:00",
      sets: [set("腿伸"), set("划船")],
    });
    expect(initialPick(null, [active], list, now)).toBe("划船");
    const old = w({
      started: "2026-11-08 09:30",
      updatedAt: "2026-11-08T09:50:00",
      sets: [set("划船")],
    });
    expect(initialPick(null, [old], list, now)).toBe("");
  });
});

describe("TestEquipmentSeatAndCues", () => {
  test("器材讀得到座位調整與注意事項；沒寫的是空的", () => {
    const e = toEquip({
      id: "E-11",
      fields: { name: "腹部捲曲訓練機", seat: " 座位 3 ", cues: ["不要用脖子帶動", " ", 3] },
    });
    expect(e.seat).toBe("座位 3");
    expect(e.cues).toEqual(["不要用脖子帶動", "3"]);
    const bare = toEquip({ id: "E-1", fields: { name: "髖外展機" } });
    expect(bare.seat).toBe("");
    expect(bare.cues).toEqual([]);
  });
  test("注意事項一行一條，空行不算", () => {
    expect(parseCues("第一條\n\n  第二條 \r\n")).toEqual(["第一條", "第二條"]);
    expect(parseCues("")).toEqual([]);
    expect(parseCues("同一條\n同一條")).toEqual(["同一條"]);
  });
});
