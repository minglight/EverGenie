// 運動教練（REQ-127）：用規則算的即時反饋（每組之後）和收操檢討，不呼叫 AI。有氧的檢討在 cardio.ts（reviewCardio）。
// 判斷與句子照舊家的 coach.py（行為參考，程式用 TypeScript 重寫）；沒有 I/O，方便單元測試。
// 要調整教練的說法或門檻，只改這支的 RULES 和 advice／review 的句子。
// workouts 一律是「新的在前」（見 workout.ts 的 sortWorkouts）；日期是 YYYY-MM-DD 字串（比字串就是比日期）。

import type { CardioMetrics } from "./cardio";
import { addDays, mondayOf } from "./dates";

export const FEELS = ["輕鬆", "剛好", "吃力"] as const;

export interface SetRec {
  equipment: string;
  /** 公斤；徒手動作是 0。 */
  weight: number;
  reps: number;
  feel: string;
}

export interface WorkoutLike {
  id: string;
  type: string;
  date: string;
  started: string;
  feeling: string;
  sets: SetRec[];
  distance_km: number;
  duration_min: number;
  pace: string;
  /** 有氧的指數（心率、步頻…）；重訓沒有。 */
  cardio?: CardioMetrics;
}

export interface EquipLike {
  name: string;
  primary: string[];
  secondary: string[];
}

export type EqMap = ReadonlyMap<string, EquipLike>;

// ---- 可以調的門檻（集中在這裡）----
export const RULES = {
  /** 建議加重／減重的幅度（公斤）。 */
  step_kg: 5,
  /** 次數做到這個數又輕鬆＝該加重了。 */
  rep_top: 12,
  /** 超過幾天沒練到的肌群，檢討時提醒。 */
  neglect_days: 14,
  /** 一個器材超過這個比例的組數覺得吃力＝偏吃力。 */
  hard_ratio: 0.5,
  /** 檢討最多列幾個進步項目。 */
  max_progress_lines: 3,
} as const;

export const MUSCLES: readonly string[] = [
  // 臀、髖
  "臀中肌",
  "臀小肌",
  "髖外旋肌群",
  "臀大肌",
  "髂腰肌",
  "髖內收肌群",
  "闊筋膜張肌",
  "梨狀肌",
  // 大腿、小腿（股四頭／大腿後側／小腿 是粗名字，分不細時用）
  "股四頭",
  "股直肌",
  "股外側肌",
  "股內側肌",
  "股中間肌",
  "縫匠肌",
  "大腿後側",
  "股二頭肌",
  "半腱肌",
  "半膜肌",
  "股薄肌",
  "小腿",
  "腓腸肌",
  "比目魚肌",
  "脛前肌",
  "腓骨肌",
  // 背
  "菱形肌",
  "背闊肌",
  "下斜方",
  "中斜方",
  "上斜方",
  "大圓肌",
  "豎脊肌",
  "多裂肌",
  "腰方肌",
  // 肩（旋轉肌群＝棘上＋棘下＋小圓＋肩胛下）
  "後三角",
  "前三角",
  "中三角",
  "旋轉肌群",
  "棘上肌",
  "棘下肌",
  "小圓肌",
  "肩胛下肌",
  "前鋸肌",
  // 手臂
  "二頭肌",
  "肱肌",
  "肱橈肌",
  "三頭",
  "前臂屈肌",
  "前臂伸肌",
  // 頸
  "深頸屈肌",
  "胸鎖乳突肌",
  "提肩胛肌",
  // 胸
  "胸大肌",
  "胸小肌",
  // 核心
  "腹直肌",
  "腹外斜肌",
  "腹內斜肌",
  "腹橫肌",
  // 粗名字：分不細時用；後面幾個目前計畫不練，留給之後自己加的器材
  "胸",
  "肩",
  "核心",
  "平衡",
];

// 器材名稱 → [關鍵字, 主練, 輔助]。由上往下第一個命中的關鍵字生效，所以「更具體的」要排前面。
const MUSCLE_RULES: readonly (readonly [
  readonly string[],
  readonly string[],
  readonly string[],
])[] = [
  [["坐姿提踵", "坐姿小腿"], ["比目魚肌"], ["腓腸肌"]],
  [["站姿提踵", "站姿小腿"], ["腓腸肌"], ["比目魚肌"]],
  [["頸屈", "頸"], ["深頸屈肌"], ["胸鎖乳突肌"]],
  [["聳肩"], ["上斜方"], ["提肩胛肌", "中斜方"]],
  [["錘式", "肱肌"], ["肱肌", "肱橈肌"], ["二頭肌"]],
  [["背伸", "背部伸展", "羅馬椅"], ["豎脊肌"], ["臀大肌", "大腿後側"]],
  [["內收"], ["髖內收肌群"], []],
  [["扭轉", "旋轉軀幹"], ["腹外斜肌", "腹內斜肌"], ["腹直肌"]],
  [["腹部捲曲", "捲腹", "仰臥起坐"], ["腹直肌"], ["髂腰肌"]],
  [["反向飛鳥", "反向蝴蝶", "後三角", "臉拉"], ["後三角", "菱形肌", "下斜方"], ["旋轉肌群"]],
  [
    ["外旋", "旋轉肌"],
    ["棘下肌", "小圓肌"],
    ["後三角", "下斜方"],
  ],
  [["前鋸"], ["前鋸肌"], []],
  [["腿彎舉", "腿後"], ["大腿後側"], ["小腿"]],
  [["腿伸展", "腿伸直", "腿屈伸", "坐站"], ["股四頭"], []],
  [["腿推", "深蹲", "哈克", "倒蹬"], ["股四頭"], ["臀大肌", "大腿後側"]],
  [["髖外展", "外展", "單腳站"], ["臀中肌"], ["臀小肌", "髖外旋肌群"]],
  [["後踢", "臀推", "臀橋", "髖後伸"], ["臀大肌"], ["大腿後側"]],
  [["提踵", "小腿"], ["小腿"], []],
  [["硬舉", "羅馬尼亞"], ["大腿後側"], ["臀大肌"]],
  [["夾胸", "飛鳥", "蝴蝶"], ["胸"], ["肩"]],
  [["胸推", "臥推", "推胸", "胸肌", "上斜", "下斜"], ["胸"], ["三頭", "肩"]],
  [["肩推", "肩上", "側平舉", "前平舉", "肩"], ["肩"], ["三頭"]],
  [["划船"], ["菱形肌", "背闊肌"], ["下斜方", "後三角", "二頭肌"]],
  [["下拉", "引體", "高位", "背"], ["背闊肌"], ["二頭肌"]],
  [["三頭", "下壓", "臂屈伸", "撐體"], ["三頭"], ["胸", "肩"]],
  [["二頭", "彎舉"], ["二頭肌"], []],
  [["腹", "捲腹", "核心", "棒式", "仰臥起坐"], ["核心"], []],
];

/** 用關鍵字推測器材練哪些肌群；回 [主練, 輔助]，推不出來回 null（交給教練補）。 */
export function inferMuscles(name: string): [string[], string[]] | null {
  for (const [keys, prim, sec] of MUSCLE_RULES) {
    if (keys.some((k) => name.includes(k))) return [[...prim], [...sec]];
  }
  return null;
}

/** 公斤的寫法：整數不帶小數，其他最多兩位。 */
export function kg(w: number): string {
  return Number.isInteger(w) ? String(w) : String(Math.round(w * 100) / 100);
}

function primaryOf(eq: EqMap, name: string): string[] {
  return [...(eq.get(name)?.primary ?? [])];
}

export interface WeekMuscle {
  muscle: string;
  sessions: number;
  sets: number;
}

/** 本週（週一起算）每個主練肌群：練了幾次（幾天）、幾組。沒練到的不列。 */
export function weekMuscles(
  workouts: readonly WorkoutLike[],
  eq: EqMap,
  today: string,
): WeekMuscle[] {
  const mon = mondayOf(today);
  const days = new Map<string, Set<string>>();
  const sets = new Map<string, number>();
  for (const w of workouts) {
    if (w.type !== "重訓" || w.date < mon) continue;
    for (const s of w.sets) {
      for (const m of primaryOf(eq, s.equipment)) {
        if (!days.has(m)) days.set(m, new Set());
        days.get(m)?.add(w.id);
        sets.set(m, (sets.get(m) ?? 0) + 1);
      }
    }
  }
  const order = (m: string) => {
    const i = MUSCLES.indexOf(m);
    return i < 0 ? 99 : i;
  };
  return [...days.keys()]
    .sort((a, b) => order(a) - order(b))
    .map((m) => ({ muscle: m, sessions: days.get(m)?.size ?? 0, sets: sets.get(m) ?? 0 }));
}

/** 新那一組之前，這個器材最近的一組：先看這次訓練裡的上一組，沒有再找之前訓練的最後一組。 */
export function reference(
  workouts: readonly WorkoutLike[],
  wid: string,
  name: string,
): { set: SetRec | null; label: string } {
  const cur = workouts.find((w) => w.id === wid);
  if (cur) {
    const prev = cur.sets.slice(0, -1).filter((s) => s.equipment === name);
    const last = prev[prev.length - 1];
    if (last) return { set: last, label: "上一組" };
  }
  for (const w of workouts) {
    if (w.id === wid || w.type !== "重訓") continue;
    const ss = w.sets.filter((s) => s.equipment === name);
    const last = ss[ss.length - 1];
    if (last) return { set: last, label: "上次" };
  }
  return { set: null, label: "" };
}

/** 下一組怎麼做：比較這組跟上一組／上次、再看感覺。 */
export function advice(s: SetRec, ref: SetRec | null, label: string): string {
  const step = RULES.step_kg;
  const top = RULES.rep_top;
  const w = s.weight;
  const reps = s.reps;
  const feel = s.feel;
  const nxt = kg(w + step);
  if (ref === null) return "這台第一次記錄，之後拿這組當比較基準。";
  const dw = w - ref.weight;
  const dr = reps - ref.reps;
  if (w === 0) {
    // 徒手動作：沒有重量可加，看次數（或秒數）和感覺
    if (feel === "吃力") return "吃力，下一組維持這個量，把動作做穩。";
    if (feel === "輕鬆") return "輕鬆，下一組可以多做一點（次數或秒數），或放慢速度。";
    return dr > 0 ? `比${label}多 ${dr}，有進步。` : "強度剛好，維持。";
  }
  if (feel === "吃力") {
    if (dw > 0) return `比${label}加了 ${kg(dw)} kg 就吃力，下一組維持 ${kg(w)} kg，把動作做穩。`;
    return `吃力，下一組維持 ${kg(w)} kg（或減 ${kg(step)} kg），先把次數做漂亮。`;
  }
  if (feel === "輕鬆") {
    if (reps >= top) return `輕鬆又做到 ${reps} 下，下一組加到 ${nxt} kg。`;
    return `輕鬆，下一組可以加到 ${nxt} kg，或先把次數推到 ${top} 下。`;
  }
  if (dw > 0) return `比${label}加重 ${kg(dw)} kg 還剛好，維持 ${kg(w)} kg 再站穩一組。`;
  if (dr > 0) return `次數比${label}多 ${dr} 下，有進步，維持 ${kg(w)} kg。`;
  if (reps >= top) return `剛好，而且做到 ${reps} 下，下次訓練可以試 ${nxt} kg。`;
  return `強度剛好，維持 ${kg(w)} kg。`;
}

/** 完成一組後的即時反饋（workouts 已包含剛加的那一組，是 wid 那次訓練的最後一組）。 */
export function feedback(
  workouts: readonly WorkoutLike[],
  wid: string,
  eq: EqMap,
  today: string,
): string[] {
  const cur = workouts.find((w) => w.id === wid);
  const s = cur?.sets[cur.sets.length - 1];
  if (!cur || !s) return [];
  const { set: ref, label } = reference(workouts, wid, s.equipment);
  const out = [advice(s, ref, label)];
  const prim = primaryOf(eq, s.equipment);
  if (prim.length > 0) {
    const wk = new Map(weekMuscles(workouts, eq, today).map((x) => [x.muscle, x]));
    const parts = prim
      .map((m) => wk.get(m))
      .filter((x): x is WeekMuscle => x !== undefined)
      .map((x) => `${x.muscle}第 ${x.sessions} 次（共 ${x.sets} 組）`);
    out.push(`本週${parts.join("、")}。`);
  }
  return out;
}

function uniq<T>(xs: readonly T[]): T[] {
  return [...new Set(xs)];
}

/** 收操後的檢討：練了哪些肌群、進步、偏弱、下次建議。w 是剛收操的那次，workouts 含它。 */
export function review(
  w: WorkoutLike,
  workouts: readonly WorkoutLike[],
  eq: EqMap,
  today: string,
): string {
  const lines: string[] = [];
  const by = new Map<string, number>();
  for (const s of w.sets) {
    const prim = primaryOf(eq, s.equipment);
    for (const m of prim.length > 0 ? prim : [s.equipment]) by.set(m, (by.get(m) ?? 0) + 1);
  }
  lines.push(`練了：${[...by].map(([m, n]) => `${m} ${n} 組`).join("、")}。`);

  // 進步：這次每個器材的最大重量 vs 之前的最大重量
  const names = uniq(w.sets.map((s) => s.equipment));
  const prog: string[] = [];
  for (const name of names) {
    const nowMax = Math.max(...w.sets.filter((s) => s.equipment === name).map((s) => s.weight));
    const before = workouts
      .filter((x) => x.id !== w.id && x.started < (w.started || "~"))
      .flatMap((x) => x.sets.filter((s) => s.equipment === name).map((s) => s.weight));
    if (before.length > 0 && nowMax > Math.max(...before)) {
      prog.push(`${name} 最大重量 ${kg(Math.max(...before))}→${kg(nowMax)} kg`);
    }
  }
  lines.push(
    prog.length > 0
      ? `進步：${prog.slice(0, RULES.max_progress_lines).join("；")}。`
      : "進步：這次沒有新的最大重量，穩住也算數。",
  );

  // 偏弱：吃力比例高的器材、兩週沒練到的肌群
  const hard: string[] = [];
  for (const name of names) {
    const ss = w.sets.filter((s) => s.equipment === name);
    if (
      ss.length >= 2 &&
      ss.filter((s) => s.feel === "吃力").length / ss.length >= RULES.hard_ratio
    ) {
      hard.push(name);
    }
  }
  const lastTrained = new Map<string, string>();
  for (const x of workouts) {
    if (x.type !== "重訓" || !/^\d{4}-\d{2}-\d{2}$/.test(x.date)) continue;
    for (const s of x.sets) {
      for (const m of primaryOf(eq, s.equipment)) {
        const prev = lastTrained.get(m);
        if (prev === undefined || x.date > prev) lastTrained.set(m, x.date);
      }
    }
  }
  const cutoff = addDays(today, -RULES.neglect_days);
  const planned = new Set([...eq.values()].flatMap((e) => e.primary)); // 只提醒清單裡的器材會練到的肌群
  const neglected = MUSCLES.filter((m) => planned.has(m) && (lastTrained.get(m) ?? "") < cutoff);
  const weak: string[] = [];
  if (hard.length > 0) weak.push(`吃力偏多：${hard.join("、")}`);
  if (neglected.length > 0 && lastTrained.size > 0) {
    // 完全沒有肌群資料時不亂講
    weak.push(`${RULES.neglect_days} 天內沒練到：${neglected.slice(0, 4).join("、")}`);
  }
  lines.push(weak.length > 0 ? `偏弱：${weak.join("；")}。` : "偏弱：沒有明顯落後的地方。");

  // 下次建議
  const tips: string[] = [];
  if (neglected.length > 0 && lastTrained.size > 0)
    tips.push(`先安排 ${neglected.slice(0, 2).join("、")}`);
  if (w.feeling === "吃力") tips.push("整體吃力，下次重量先不加");
  else if (w.feeling === "輕鬆") tips.push(`整體偏輕鬆，每台可加 ${kg(RULES.step_kg)} kg`);
  if (hard.length > 0) tips.push(`${hard[0]} 維持或減 ${kg(RULES.step_kg)} kg`);
  lines.push(
    tips.length > 0 ? `下次：${tips.join("；")}。` : "下次：照這次的重量，順的話再往上加。",
  );
  return lines.join("\n");
}

/** 配速字串 m:ss 換成秒。 */
export function paceSec(p: string): number {
  const [m, sec] = String(p).split(":");
  return Number.parseInt(m ?? "0", 10) * 60 + Number.parseInt(sec || "0", 10);
}
