import { describe, expect, test } from "vitest";
import { reviewCardio as reviewRun } from "./cardio";
import {
  advice,
  type EquipLike,
  feedback,
  inferMuscles,
  MUSCLES,
  review,
  type SetRec,
  type WorkoutLike,
  weekMuscles,
} from "./coach";

const set = (equipment: string, weight: number, reps: number, feel: string): SetRec => ({
  equipment,
  weight,
  reps,
  feel,
});

function workout(over: Partial<WorkoutLike>): WorkoutLike {
  return {
    id: "W-1",
    type: "重訓",
    date: "2026-10-21",
    started: "2026-10-21 18:00",
    feeling: "",
    sets: [],
    distance_km: 0,
    duration_min: 0,
    pace: "",
    ...over,
  };
}

const eq = (name: string, primary: string[], secondary: string[] = []): EquipLike => ({
  name,
  primary,
  secondary,
});
const eqMap = (...list: EquipLike[]) => new Map(list.map((e) => [e.name, e]));

describe("教練規則", () => {
  // Guards: REQ-127
  test("TestAdviceRules", () => {
    const ref = set("腿推舉機", 40, 10, "剛好");
    // 第一次
    expect(advice(set("腿推舉機", 40, 10, "剛好"), null, "")).toBe(
      "這台第一次記錄，之後拿這組當比較基準。",
    );
    // 吃力：有加重 / 沒加重
    expect(advice(set("腿推舉機", 45, 10, "吃力"), ref, "上一組")).toBe(
      "比上一組加了 5 kg 就吃力，下一組維持 45 kg，把動作做穩。",
    );
    expect(advice(set("腿推舉機", 40, 10, "吃力"), ref, "上一組")).toBe(
      "吃力，下一組維持 40 kg（或減 5 kg），先把次數做漂亮。",
    );
    // 輕鬆：做到 12 下 / 沒到
    expect(advice(set("腿推舉機", 40, 12, "輕鬆"), ref, "上次")).toBe(
      "輕鬆又做到 12 下，下一組加到 45 kg。",
    );
    expect(advice(set("腿推舉機", 40, 10, "輕鬆"), ref, "上次")).toBe(
      "輕鬆，下一組可以加到 45 kg，或先把次數推到 12 下。",
    );
    // 剛好：加重 / 次數變多 / 做到 12 / 其他
    expect(advice(set("腿推舉機", 42.5, 10, "剛好"), ref, "上次")).toBe(
      "比上次加重 2.5 kg 還剛好，維持 42.5 kg 再站穩一組。",
    );
    expect(advice(set("腿推舉機", 40, 11, "剛好"), ref, "上一組")).toBe(
      "次數比上一組多 1 下，有進步，維持 40 kg。",
    );
    expect(advice(set("腿推舉機", 40, 12, "剛好"), set("腿推舉機", 40, 12, "剛好"), "上次")).toBe(
      "剛好，而且做到 12 下，下次訓練可以試 45 kg。",
    );
    expect(advice(set("腿推舉機", 40, 10, "剛好"), ref, "上次")).toBe("強度剛好，維持 40 kg。");
    // 徒手（重量 0）：看次數與感覺
    const bw = set("棒式", 0, 30, "剛好");
    expect(advice(set("棒式", 0, 30, "吃力"), bw, "上一組")).toBe(
      "吃力，下一組維持這個量，把動作做穩。",
    );
    expect(advice(set("棒式", 0, 30, "輕鬆"), bw, "上一組")).toBe(
      "輕鬆，下一組可以多做一點（次數或秒數），或放慢速度。",
    );
    expect(advice(set("棒式", 0, 40, "剛好"), bw, "上一組")).toBe("比上一組多 10，有進步。");
    expect(advice(set("棒式", 0, 30, "剛好"), bw, "上一組")).toBe("強度剛好，維持。");
  });

  // Guards: REQ-127
  test("TestAdviceFeedbackAddsWeekMuscles", () => {
    const eqm = eqMap(eq("啞鈴彎舉", ["二頭肌"]));
    const ws = [
      workout({
        id: "W-2",
        date: "2026-10-25",
        started: "2026-10-25 10:00",
        sets: [set("啞鈴彎舉", 10, 10, "剛好"), set("啞鈴彎舉", 10, 11, "剛好")],
      }),
      workout({
        id: "W-1",
        date: "2026-10-21",
        started: "2026-10-21 10:00",
        sets: [set("啞鈴彎舉", 10, 10, "剛好")],
      }),
    ];
    // 2026-10-25 是週日；本週從 10/19 起算，兩次訓練都算，共 3 組
    expect(feedback(ws, "W-2", eqm, "2026-10-25")).toEqual([
      "次數比上一組多 1 下，有進步，維持 10 kg。",
      "本週二頭肌第 2 次（共 3 組）。",
    ]);
    // 新的一週：只算 10/26 起
    expect(weekMuscles(ws, eqm, "2026-10-26")).toEqual([]);
    // 沒有肌群資料的器材不補「本週」那句
    expect(feedback(ws, "W-2", eqMap(), "2026-10-25")).toHaveLength(1);
  });

  // Guards: REQ-127
  test("TestReviewSentences", () => {
    const eqm = eqMap(
      eq("腿推舉機", ["股四頭"], ["臀大肌"]),
      eq("啞鈴彎舉", ["二頭肌"]),
      eq("坐姿划船機", ["菱形肌", "背闊肌"]),
    );
    const old = workout({
      id: "W-1",
      date: "2026-10-01",
      started: "2026-10-01 18:00",
      sets: [set("腿推舉機", 40, 10, "剛好"), set("坐姿划船機", 30, 10, "剛好")],
    });
    const cur = workout({
      id: "W-2",
      date: "2026-10-25",
      started: "2026-10-25 18:00",
      feeling: "吃力",
      sets: [
        set("腿推舉機", 45, 10, "吃力"),
        set("腿推舉機", 45, 8, "吃力"),
        set("啞鈴彎舉", 10, 10, "剛好"),
      ],
    });
    const out = review(cur, [cur, old], eqm, "2026-10-25").split("\n");
    expect(out).toEqual([
      "練了：股四頭 2 組、二頭肌 1 組。",
      "進步：腿推舉機 最大重量 40→45 kg。",
      // 菱形肌與背闊肌最近一次是 10/01（24 天前）；股四頭、二頭肌今天練了
      "偏弱：吃力偏多：腿推舉機；14 天內沒練到：菱形肌、背闊肌。",
      "下次：先安排 菱形肌、背闊肌；整體吃力，下次重量先不加；腿推舉機 維持或減 5 kg。",
    ]);

    // 沒有進步、沒有落後、整體輕鬆
    const easy = workout({
      id: "W-3",
      date: "2026-10-25",
      started: "2026-10-25 18:00",
      feeling: "輕鬆",
      sets: [set("腿推舉機", 40, 10, "剛好")],
    });
    const out2 = review(easy, [easy, old], eqMap(eq("腿推舉機", ["股四頭"])), "2026-10-25").split(
      "\n",
    );
    expect(out2).toEqual([
      "練了：股四頭 1 組。",
      "進步：這次沒有新的最大重量，穩住也算數。",
      "偏弱：沒有明顯落後的地方。",
      "下次：整體偏輕鬆，每台可加 5 kg。",
    ]);

    // 沒有任何肌群資料時不亂講落後；沒有感覺也沒有偏弱 → 預設一句
    const bare = workout({ id: "W-4", sets: [set("神秘機", 10, 10, "剛好")] });
    expect(review(bare, [bare], eqMap(), "2026-10-25").split("\n")).toEqual([
      "練了：神秘機 1 組。",
      "進步：這次沒有新的最大重量，穩住也算數。",
      "偏弱：沒有明顯落後的地方。",
      "下次：照這次的重量，順的話再往上加。",
    ]);
  });

  // Guards: REQ-127
  test("TestReviewRun", () => {
    const run = (over: Partial<WorkoutLike>) =>
      workout({ type: "跑步", distance_km: 5, duration_min: 30, pace: "6:00", ...over });
    const first = run({ id: "W-5", feeling: "剛好" });
    expect(reviewRun(first, [first])).toBe(
      "這次：5 km、30 分，配速 6:00/km。\n這是第一筆跑步，之後拿它當比較基準。\n下次：照這次的強度，穩定了再加距離。",
    );
    // 跟上一次比：距離多 0.5、配速快 10 秒；輕鬆
    const next = run({
      id: "W-6",
      distance_km: 5.5,
      duration_min: 32.5,
      pace: "5:50",
      feeling: "輕鬆",
    });
    expect(reviewRun(next, [next, first])).toBe(
      "這次：5.5 km、32.5 分，配速 5:50/km。\n跟上次比：距離多 0.5 km、配速快 10 秒/km。\n下次：可以多跑 0.5 km，或把配速加快一點點（二選一，不要同時）。",
    );
    // 距離少 0.45 km、配速慢 12 秒；吃力
    const same = run({ id: "W-7", distance_km: 5.05, pace: "6:02", feeling: "吃力" });
    expect(reviewRun(same, [same, next, first]).split("\n")).toEqual([
      "這次：5.05 km、30 分，配速 6:02/km。",
      "跟上次比：距離少 0.45 km、配速慢 12 秒/km。",
      "下次：配速或距離先不要加，睡好再跑。",
    ]);
    const near = run({
      id: "W-8",
      distance_km: 5.55,
      duration_min: 32.5,
      pace: "5:51",
      feeling: "剛好",
    });
    expect(reviewRun(near, [near, next]).split("\n")[1]).toBe("跟上次比：差不多。");
  });

  // Guards: REQ-127
  test("TestInferMuscles", () => {
    const cases: Record<string, [string[], string[]]> = {
      腹部捲曲訓練機: [["腹直肌"], ["髂腰肌"]],
      坐姿提踵: [["比目魚肌"], ["腓腸肌"]],
      站姿提踵機: [["腓腸肌"], ["比目魚肌"]],
      羅馬椅背伸: [["豎脊肌"], ["臀大肌", "大腿後側"]],
      背部伸展: [["豎脊肌"], ["臀大肌", "大腿後側"]],
      聳肩: [["上斜方"], ["提肩胛肌", "中斜方"]],
      錘式彎舉: [["肱肌", "肱橈肌"], ["二頭肌"]],
      髖內收機: [["髖內收肌群"], []],
      旋轉軀幹: [["腹外斜肌", "腹內斜肌"], ["腹直肌"]],
      腿推舉機: [["股四頭"], ["臀大肌", "大腿後側"]],
      高位下拉: [["背闊肌"], ["二頭肌"]],
      啞鈴彎舉: [["二頭肌"], []],
      棒式: [["核心"], []],
    };
    for (const [name, want] of Object.entries(cases)) {
      expect(inferMuscles(name), name).toEqual(want);
    }
    expect(inferMuscles("不認得的器材")).toBeNull();
    // 規則只用名單裡的肌群，名單沒有重複
    expect(new Set(MUSCLES).size).toBe(MUSCLES.length);
    for (const name of Object.keys(cases)) {
      const got = inferMuscles(name);
      for (const m of [...(got?.[0] ?? []), ...(got?.[1] ?? [])]) expect(MUSCLES).toContain(m);
    }
  });
});
