// 運動套件的資料型別與純函式（REQ-127）：vault 記錄換成型別、排序、進行中／忘了收操的判斷、常用器材、搜尋。
// 沒有任何 I/O，方便單元測試。
import { type CardioMetrics, cardioSummary, isCardio, type Point, toMetrics } from "./cardio";
import {
  review as coachReview,
  type EquipLike,
  inferMuscles,
  MUSCLES,
  type SetRec,
  type WorkoutLike,
} from "./coach";
import { addDays, atStr, mondayOf, parseAt } from "./dates";

/** 進行中的訓練超過這麼久沒動，下一組開新的一次（舊的當作忘了收操）。 */
export const STALE_HOURS = 8;
/** 「常用」顯示幾個器材。 */
export const FREQ_N = 6;

/** vault 記錄的最小形狀（不 import sdk 型別，讓這個檔能被單元測試直接載入）。 */
export interface RecordLike {
  id?: string;
  fields?: Record<string, unknown>;
  body?: string;
  updated_at?: string;
}

export interface Workout extends WorkoutLike {
  status: string;
  finished: string;
  note: string;
  review: string;
  review_by: string;
  source: string;
  /** vault 記錄最後修改的時間（ISO）；進行中的重訓拿它當「最後一組的時間」。 */
  updatedAt: string;
  cardio: CardioMetrics;
}

export interface Equip extends EquipLike {
  id: string;
  type: string;
  unit: string;
  /** 座位調整（例如「座位 3」）；空字串＝沒記。 */
  seat: string;
  /** 注意事項，一條一句，第一條最重要。 */
  cues: string[];
}

export interface Note {
  id: string;
  title: string;
  date: string;
  pinned: boolean;
  body: string;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : v == null ? "" : String(v);
}

export function num(v: unknown): number {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    if (Number.isFinite(n)) return n;
  }
  return 0;
}

function strList(v: unknown): string[] {
  return Array.isArray(v) ? v.map(str).filter((x) => x !== "") : [];
}

function toSets(v: unknown): SetRec[] {
  if (!Array.isArray(v)) return [];
  return v
    .filter((x): x is Record<string, unknown> => typeof x === "object" && x !== null)
    .map((x) => ({
      equipment: str(x.equipment),
      weight: num(x.weight),
      reps: num(x.reps),
      feel: str(x.feel),
    }));
}

export function toWorkout(r: RecordLike): Workout {
  const f = r.fields ?? {};
  return {
    id: r.id ?? "",
    type: str(f.type),
    date: str(f.date),
    started: str(f.started),
    finished: str(f.finished),
    status: str(f.status),
    feeling: str(f.feeling),
    note: str(f.note),
    sets: toSets(f.sets),
    distance_km: num(f.distance_km),
    duration_min: num(f.duration_min),
    pace: str(f.pace),
    source: str(f.source),
    review: str(f.review),
    review_by: str(f.review_by),
    updatedAt: str(r.updated_at),
    cardio: toMetrics(f),
  };
}

export function toEquip(r: RecordLike): Equip {
  const f = r.fields ?? {};
  return {
    id: r.id ?? "",
    name: str(f.name),
    primary: strList(f.primary),
    secondary: strList(f.secondary),
    type: str(f.type),
    unit: str(f.unit),
    seat: str(f.seat).trim(),
    cues: [
      ...new Set(
        strList(f.cues)
          .map((x) => x.trim())
          .filter(Boolean),
      ),
    ],
  };
}

/** 把使用者輸入的注意事項（一行一條）拆開；空行不算。 */
export function parseCues(input: string): string[] {
  return [
    ...new Set(
      input
        .split(/\r?\n/)
        .map((x) => x.trim())
        .filter(Boolean),
    ),
  ];
}

export function toNote(r: RecordLike): Note {
  const f = r.fields ?? {};
  return {
    id: r.id ?? "",
    title: str(f.title),
    date: str(f.date),
    pinned: f.pinned === true,
    body: r.body ?? "",
  };
}

function numOf(id: string): number {
  return Number.parseInt(id.split("-")[1] ?? "0", 10) || 0;
}

/** 新的在前：先比開始時間，同一分鐘記兩筆時比單號。 */
export function sortWorkouts<T extends WorkoutLike>(ws: readonly T[]): T[] {
  return [...ws].sort((a, b) =>
    a.started === b.started ? numOf(b.id) - numOf(a.id) : a.started < b.started ? 1 : -1,
  );
}

export function eqMapOf(list: readonly Equip[]): Map<string, Equip> {
  return new Map(list.map((e) => [e.name, e]));
}

/** 同時只會有一筆進行中的重訓；有多筆（手改資料）時取最新的。 */
export function activeWorkout<T extends Workout>(ws: readonly T[]): T | null {
  return sortWorkouts(ws).find((w) => w.type === "重訓" && w.status === "進行中") ?? null;
}

/** 進行中那筆最後一次動的時間：有組就用記錄的修改時間，沒有組就是開始時間。 */
export function lastActivity(w: Workout): Date | null {
  if (w.sets.length > 0 && w.updatedAt) {
    const t = new Date(w.updatedAt);
    if (!Number.isNaN(t.getTime())) return t;
  }
  return parseAt(w.started);
}

export interface SetPlan {
  /** 要先標成完成的舊訓練（忘了收操）；沒有就是 null。 */
  stale: Workout | null;
  /** 這一組要接在哪一筆後面；null＝開新的一筆。 */
  active: Workout | null;
}

/** 記一組之前的判斷（REQ-127）：沒有進行中的重訓、或進行中那筆超過 8 小時沒動 → 舊的標完成、開新的。 */
export function planSet(ws: readonly Workout[], now: Date): SetPlan {
  const active = activeWorkout(ws);
  if (!active) return { stale: null, active: null };
  if (isStale(active, now)) return { stale: active, active: null };
  return { stale: null, active };
}

/** 進行中那筆是不是忘了收操：最後一次動（見 lastActivity）超過 STALE_HOURS 小時（FIT-6、FIT-15）。 */
export function isStale(w: Workout, now: Date): boolean {
  const last = lastActivity(w);
  return !!last && now.getTime() - last.getTime() > STALE_HOURS * 3600 * 1000;
}

/** 忘了收操的那一筆要寫回去的欄位：標完成、備註加一句、檢討用規則算。 */
export function staleCloseFields(
  w: Workout,
  ws: readonly Workout[],
  eq: ReadonlyMap<string, Equip>,
  today: string,
): Record<string, unknown> {
  const last = lastActivity(w);
  const note = w.note ? `${w.note} 忘了收操` : "忘了收操";
  const closed: Workout = { ...w, status: "完成", note };
  const all = ws.map((x) => (x.id === w.id ? closed : x));
  return {
    status: "完成",
    finished: last ? atStr(last) : w.started,
    note,
    review: coachReview(closed, sortWorkouts(all), eq, today),
    review_by: "規則",
  };
}

/** 常用＝最近 12 次重訓裡組數最多的器材（取前 FREQ_N 個），還沒練過的依清單順序補滿。 */
export function frequentEquipment(
  ws: readonly Workout[],
  equipment: readonly Equip[],
  n: number = FREQ_N,
): string[] {
  const names = equipment.map((e) => e.name);
  const cnt = new Map<string, number>();
  for (const w of sortWorkouts(ws)
    .filter((x) => x.type === "重訓")
    .slice(0, 12)) {
    for (const s of w.sets) cnt.set(s.equipment, (cnt.get(s.equipment) ?? 0) + 1);
  }
  const used = names
    .filter((x) => cnt.has(x))
    .sort((a, b) => (cnt.get(b) ?? 0) - (cnt.get(a) ?? 0));
  return [...used, ...names.filter((x) => !cnt.has(x))].slice(0, n);
}

/** 搜尋：器材名稱或肌群（主練、輔助）有包含關鍵字。 */
export function matchEquipment(equipment: readonly Equip[], q: string): Equip[] {
  const k = q.trim();
  if (!k) return [...equipment];
  return equipment.filter(
    (e) =>
      e.name.includes(k) ||
      e.primary.some((m) => m.includes(k)) ||
      e.secondary.some((m) => m.includes(k)),
  );
}

/** 這個器材最近的一組（先看進行中那筆，再往前找）；用來預設重量與次數。 */
export function lastSetOf(
  ws: readonly Workout[],
  name: string,
): { set: SetRec; date: string; current: boolean } | null {
  const active = activeWorkout(ws);
  for (const w of sortWorkouts(ws)) {
    if (w.type !== "重訓") continue;
    const ss = w.sets.filter((s) => s.equipment === name);
    const last = ss[ss.length - 1];
    if (last) return { set: last, date: w.date, current: w.id === active?.id };
  }
  return null;
}

export const isBodyweight = (e: Equip | undefined): boolean => e?.type === "徒手";

/** 新器材的肌群：用關鍵字推；推不出來回空（頁面提示「請教練補」）。 */
export function newEquipmentFields(name: string): Record<string, unknown> {
  const got = inferMuscles(name);
  return { name, primary: got ? got[0] : [], secondary: got ? got[1] : [] };
}

/** 把使用者輸入的肌群（逗號、頓號、空白分隔）拆開；回傳合格的與不認得的。 */
export function parseMuscles(input: string): { ok: string[]; bad: string[] } {
  const parts = input
    .split(/[,，、\s]+/)
    .map((x) => x.trim())
    .filter(Boolean);
  const ok: string[] = [];
  const bad: string[] = [];
  for (const p of new Set(parts)) (MUSCLES.includes(p) ? ok : bad).push(p);
  return { ok, bad };
}

export interface SetCheck {
  ok: boolean;
  error: string;
}

/** 一組的輸入檢查（重量 0–500 kg、次數 1–200）。 */
export function checkSet(weight: number, reps: number): SetCheck {
  if (!Number.isFinite(weight) || !Number.isFinite(reps)) {
    return { ok: false, error: "重量、次數要是數字" };
  }
  if (weight < 0 || weight > 500 || !Number.isInteger(reps) || reps < 1 || reps > 200) {
    return { ok: false, error: "重量 0–500 kg、次數 1–200（整數）" };
  }
  return { ok: true, error: "" };
}

/** 重訓趨勢的指標：每次最大重量、估算 1RM（Epley）、總量（重量×次數加總）。徒手動作改看次數。 */
export type StrengthMetric = "max" | "e1rm" | "volume";

/** Epley 估算 1RM：重量 ×（1 + 次數/30），取到 0.5 kg；只做一下時就是那個重量。 */
export function e1rm(weight: number, reps: number): number {
  if (!(weight > 0) || !(reps > 0)) return 0;
  if (reps === 1) return weight; // 做一下就是 1RM 本身
  return Math.round(weight * (1 + reps / 30) * 2) / 2;
}

export interface SetInput extends SetCheck {
  weight: number;
  reps: number;
}

/**
 * 表單上的字變成一組（FIT-13）：要填重量的器材，重量空白要擋下來說「重量要填」，
 * 不能當成 0 kg 記進去（Number("") 是 0，以前會靜靜記成「徒手」）；次數空白也直接說要填。
 * repsLabel 是畫面上次數欄的名字（「次數」或徒手器材的「秒數」等）。
 */
export function parseSetInput(
  weightRaw: string,
  repsRaw: string,
  bodyweight: boolean,
  repsLabel = "次數",
): SetInput {
  const w = weightRaw.trim();
  const r = repsRaw.trim();
  if (!bodyweight && w === "") return { ok: false, error: "重量要填", weight: 0, reps: 0 };
  if (r === "") return { ok: false, error: `${repsLabel}要填`, weight: 0, reps: 0 };
  const weight = bodyweight ? 0 : Number(w);
  const reps = Number(r);
  return { ...checkSet(weight, reps), weight, reps };
}

/** 某器材每次訓練的指標（舊→新），畫時間軸用。全部是徒手（重量 0）時三種都改用次數（最多次數／一次總次數）。 */
export function strengthSeries(
  ws: readonly Workout[],
  name: string,
  metric: StrengthMetric,
): Point[] {
  // 徒手與否看這個器材的全部歷史（跟畫面的單位一致）；有負重的器材，只記了徒手組的那次跳過，免得 kg 和次數畫在同一條線。
  const bw = isBodyweightHistory(ws, name);
  return sortWorkouts(ws)
    .reverse()
    .filter((w) => w.type === "重訓")
    .map((w) => ({ w, sets: w.sets.filter((s) => s.equipment === name && (bw || s.weight > 0)) }))
    .filter((x) => x.sets.length > 0)
    .map(({ w, sets }) => {
      let value: number;
      if (bw)
        value =
          metric === "volume"
            ? sets.reduce((a, s) => a + s.reps, 0)
            : Math.max(...sets.map((s) => s.reps));
      else if (metric === "max") value = Math.max(...sets.map((s) => s.weight));
      else if (metric === "e1rm") value = Math.max(...sets.map((s) => e1rm(s.weight, s.reps)));
      else value = sets.reduce((a, s) => a + s.weight * s.reps, 0);
      return { id: w.id, date: w.date, value };
    })
    .sort((a, b) => (a.date < b.date ? -1 : a.date > b.date ? 1 : 0));
}

/** 這個器材是不是只記過徒手（重量 0）的組。 */
export function isBodyweightHistory(ws: readonly Workout[], name: string): boolean {
  const sets = ws
    .filter((w) => w.type === "重訓")
    .flatMap((w) => w.sets.filter((s) => s.equipment === name));
  return sets.length > 0 && sets.every((s) => s.weight === 0);
}

export interface WeekBar {
  /** 那週的週一（YYYY-MM-DD）。 */
  week: string;
  value: number;
}

/** 最近 n 週（含本週，舊→新）每週的量：重訓＝總量（kg，含進行中那次，邊練邊看得到本週在長）、有氧＝總分鐘（有氧一次記完，都是完成）；沒練的週是 0。 */
export function weeklyTotals(
  ws: readonly Workout[],
  today: string,
  what: "重訓" | "有氧",
  n = 12,
): WeekBar[] {
  const thisMon = mondayOf(today);
  const bars: WeekBar[] = [];
  for (let i = n - 1; i >= 0; i--) bars.push({ week: addDays(thisMon, -7 * i), value: 0 });
  const idx = new Map(bars.map((b, i) => [b.week, i]));
  for (const w of ws) {
    if (isCardio(w.type) && w.status !== "完成") continue;
    const i = idx.get(mondayOf(w.date));
    if (i === undefined) continue;
    const bar = bars[i];
    if (!bar) continue;
    if (what === "重訓" && w.type === "重訓") {
      bar.value += w.sets.reduce((a, s) => a + s.weight * s.reps, 0);
    } else if (what === "有氧" && isCardio(w.type)) {
      bar.value += w.duration_min;
    }
  }
  for (const b of bars) b.value = Math.round(b.value * 10) / 10;
  return bars;
}

/** 本週（週一起算）完成的重訓次數。 */
export function weekStrengthCount(ws: readonly Workout[], monday: string): number {
  return ws.filter((w) => w.type === "重訓" && w.status === "完成" && w.date >= monday).length;
}

/** 本週（週一起算）完成的有氧次數（跑步、划船、游泳、騎車）。 */
export function weekCardioCount(ws: readonly Workout[], monday: string): number {
  return ws.filter((w) => isCardio(w.type) && w.status === "完成" && w.date >= monday).length;
}

/** 訓練在歷史清單裡的摘要：重訓＝幾組，有氧＝距離、時間、配速（騎車均速）、平均心率。 */
export function summaryOf(w: Workout): string {
  if (isCardio(w.type)) return cardioSummary(w);
  return `${w.sets.length} 組`;
}

// ---- 挑器材：分類（部位）、剛才在用、記住選的器材（FIT-17、FIT-18，DEV-146）----

/** 挑器材的分類＝部位（照器材的主練肌群推，不用另外填）；之後器材多了先挑部位再挑器材。 */
export const BODY_PARTS = ["腿臀", "背", "胸", "肩", "手臂", "核心", "其他"] as const;
export type BodyPart = (typeof BODY_PARTS)[number];

const PART_OF: Readonly<Record<string, BodyPart>> = (() => {
  const m: Record<string, BodyPart> = {};
  const put = (part: BodyPart, names: string[]) => {
    for (const n of names) m[n] = part;
  };
  put("腿臀", [
    "臀中肌",
    "臀小肌",
    "髖外旋肌群",
    "臀大肌",
    "髂腰肌",
    "髖內收肌群",
    "闊筋膜張肌",
    "梨狀肌",
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
  ]);
  put("背", [
    "菱形肌",
    "背闊肌",
    "下斜方",
    "中斜方",
    "上斜方",
    "大圓肌",
    "豎脊肌",
    "多裂肌",
    "腰方肌",
  ]);
  put("肩", [
    "後三角",
    "前三角",
    "中三角",
    "旋轉肌群",
    "棘上肌",
    "棘下肌",
    "小圓肌",
    "肩胛下肌",
    "前鋸肌",
    "肩",
  ]);
  put("手臂", ["二頭肌", "肱肌", "肱橈肌", "三頭", "前臂屈肌", "前臂伸肌"]);
  put("胸", ["胸大肌", "胸小肌", "胸"]);
  put("核心", ["腹直肌", "腹外斜肌", "腹內斜肌", "腹橫肌", "核心"]);
  return m;
})();

/** 一個肌群屬於哪個部位；名單外（頸、平衡、自己打的）是「其他」。 */
export function partOfMuscle(m: string): BodyPart {
  return PART_OF[m] ?? "其他";
}

/** 器材屬於哪些部位：看主練肌群（一台可以在兩個部位，例如反向飛鳥＝肩＋背）；沒有主練肌群就是「其他」。 */
export function partsOf(e: Equip): BodyPart[] {
  const got = new Set(e.primary.map(partOfMuscle));
  const out = BODY_PARTS.filter((p) => got.has(p));
  return out.length > 0 ? out : ["其他"];
}

/** 依部位分組（照 BODY_PARTS 順序，空的部位不出現）；組內照器材清單順序。 */
export function groupByPart(equipment: readonly Equip[]): { part: BodyPart; items: Equip[] }[] {
  return BODY_PARTS.map((part) => ({
    part,
    items: equipment.filter((e) => partsOf(e).includes(part)),
  })).filter((g) => g.items.length > 0);
}

/** 這次訓練用過的器材，最近用的在前（「剛才在用」）；只回清單裡還有的器材。 */
export function recentInWorkout(active: Workout | null, equipment: readonly Equip[]): string[] {
  if (!active) return [];
  const have = new Set(equipment.map((e) => e.name));
  const out: string[] = [];
  for (let i = active.sets.length - 1; i >= 0; i--) {
    const n = active.sets[i]?.equipment ?? "";
    if (n && have.has(n) && !out.includes(n)) out.push(n);
  }
  return out;
}

/** 「常用」那一排：常用器材扣掉已經在「剛才在用」的，最多 n 個。 */
export function frequentExcept(
  ws: readonly Workout[],
  equipment: readonly Equip[],
  recent: readonly string[],
  n: number = FREQ_N,
): string[] {
  return frequentEquipment(ws, equipment, n + recent.length)
    .filter((x) => !recent.includes(x))
    .slice(0, n);
}

/** 進行中那張卡：連續同一台的組併成一段（器材名稱只寫一次）。 */
export function groupSets(sets: readonly SetRec[]): { equipment: string; sets: SetRec[] }[] {
  const out: { equipment: string; sets: SetRec[] }[] = [];
  for (const s of sets) {
    const g = out[out.length - 1];
    if (g && g.equipment === s.equipment) g.sets.push(s);
    else out.push({ equipment: s.equipment, sets: [s] });
  }
  return out;
}

/** 記住的選擇（存在瀏覽器）：選了哪台、什麼時候選的。 */
export interface Remembered {
  name: string;
  at: number;
}

/**
 * 回到頁面時要預選哪台器材（FIT-18）：記住的那台還在清單裡、而且沒超過 STALE_HOURS 就用它；
 * 不然有進行中（沒忘了收操）的訓練就用最後一組的器材；都沒有就不選。
 */
export function initialPick(
  saved: Remembered | null,
  ws: readonly Workout[],
  equipment: readonly Equip[],
  now: Date,
): string {
  const have = (n: string) => equipment.some((e) => e.name === n);
  if (saved && have(saved.name) && now.getTime() - saved.at <= STALE_HOURS * 3600 * 1000) {
    return saved.name;
  }
  const active = activeWorkout(ws);
  if (active && !isStale(active, now)) return recentInWorkout(active, equipment)[0] ?? "";
  return "";
}
