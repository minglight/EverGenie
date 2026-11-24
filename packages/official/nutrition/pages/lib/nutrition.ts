// 飲食套件的純函式（REQ-128）：加總、猜餐別、趨勢、日期。沒有任何 I/O，方便單元測試。
// 日期一律用瀏覽器的本地時區；資料裡的 date 是 YYYY-MM-DD 字串。

export const MEAL_KINDS = ["早餐", "午餐", "晚餐", "點心", "宵夜"] as const;
export type MealKind = (typeof MEAL_KINDS)[number];
export const GOAL_MODES = ["減脂", "增肌", "維持", "自訂"] as const;
export type GoalMode = (typeof GOAL_MODES)[number];

export interface Item {
  name: string;
  qty: string;
  kcal: number;
  protein: number;
  carb: number;
  fat: number;
}

export interface Macros {
  kcal: number;
  protein: number;
  carb: number;
  fat: number;
}

export interface Meal extends Macros {
  id: string;
  date: string;
  at: string;
  meal: string;
  items: Item[];
  photos: string[];
  conf: string;
  note: string;
  fav: string;
}

export interface Goal extends Macros {
  mode: string;
}

export interface Fav extends Macros {
  id: string;
  name: string;
  items: Item[];
}

/** vault 記錄的最小形狀（不 import sdk 型別，讓這個檔能被單元測試直接載入）。 */
export interface RecordLike {
  id?: string;
  fields?: Record<string, unknown>;
}

export function num(v: unknown): number {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    if (Number.isFinite(n)) return n;
  }
  return 0;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : v == null ? "" : String(v);
}

function toItems(v: unknown): Item[] {
  if (!Array.isArray(v)) return [];
  return v
    .filter((x): x is Record<string, unknown> => typeof x === "object" && x !== null)
    .map((x) => ({
      name: str(x.name),
      qty: str(x.qty),
      kcal: num(x.kcal),
      protein: num(x.protein),
      carb: num(x.carb),
      fat: num(x.fat),
    }));
}

export function toMeal(r: RecordLike): Meal {
  const f = r.fields ?? {};
  return {
    id: r.id ?? "",
    date: str(f.date),
    at: str(f.at),
    meal: str(f.meal),
    kcal: num(f.kcal),
    protein: num(f.protein),
    carb: num(f.carb),
    fat: num(f.fat),
    items: toItems(f.items),
    photos: Array.isArray(f.photos) ? f.photos.map(str).filter(Boolean) : [],
    conf: str(f.conf),
    note: str(f.note),
    fav: str(f.fav),
  };
}

export function toGoal(r: RecordLike | undefined): Goal | null {
  if (!r) return null;
  const f = r.fields ?? {};
  return {
    mode: str(f.mode),
    kcal: num(f.kcal),
    protein: num(f.protein),
    carb: num(f.carb),
    fat: num(f.fat),
  };
}

export function toFav(r: RecordLike): Fav {
  const f = r.fields ?? {};
  return {
    id: r.id ?? "",
    name: str(f.name),
    kcal: num(f.kcal),
    protein: num(f.protein),
    carb: num(f.carb),
    fat: num(f.fat),
    items: toItems(f.items),
  };
}

// ---- 日期 ----

const two = (n: number) => String(n).padStart(2, "0");

/** 本地日期 YYYY-MM-DD。 */
export function dateStr(d: Date): string {
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`;
}

/** 本地時間 YYYY-MM-DD HH:MM（meal.at 的格式）。 */
export function atStr(d: Date): string {
  return `${dateStr(d)} ${two(d.getHours())}:${two(d.getMinutes())}`;
}

export function isDateStr(s: string | null | undefined): s is string {
  if (!s || !/^\d{4}-\d{2}-\d{2}$/.test(s)) return false;
  const [y, m, d] = s.split("-").map(Number) as [number, number, number];
  const t = new Date(y, m - 1, d);
  return t.getFullYear() === y && t.getMonth() === m - 1 && t.getDate() === d;
}

/** 把日期往前／往後移幾天（本地日曆，不受夏令時間影響）。 */
export function shiftDate(date: string, days: number): string {
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  return dateStr(new Date(y, m - 1, d + days));
}

/** 「後一天」按得下去嗎：只能走到今天為止（NUT-8，跟日期選擇器的 max 一致）。 */
export function canGoNext(date: string, today: string): boolean {
  return shiftDate(date, 1) <= today;
}

/** 網址 ?date=YYYY-MM-DD 的值；沒有或不合法就是今天。 */
export function dateFromSearch(search: string, now: Date): string {
  const v = new URLSearchParams(search).get("date");
  return isDateStr(v) ? v : dateStr(now);
}

// ---- 餐別、加總 ----

/** 依時間猜餐別：5–10 早餐、10–14 午餐、14–17 點心、17–21 晚餐、其他宵夜。 */
export function guessMeal(now: Date): MealKind {
  const h = now.getHours();
  if (h >= 5 && h < 10) return "早餐";
  if (h >= 10 && h < 14) return "午餐";
  if (h >= 14 && h < 17) return "點心";
  if (h >= 17 && h < 21) return "晚餐";
  return "宵夜";
}

export interface DayTotals extends Macros {
  count: number;
}

/** 某一天所有餐點的合計。 */
export function dayTotals(meals: readonly Meal[], date: string): DayTotals {
  const t: DayTotals = { kcal: 0, protein: 0, carb: 0, fat: 0, count: 0 };
  for (const m of meals) {
    if (m.date !== date) continue;
    t.kcal += m.kcal;
    t.protein += m.protein;
    t.carb += m.carb;
    t.fat += m.fat;
    t.count += 1;
  }
  return t;
}

/** 某一天的餐點，依 at 由早到晚（at 一樣就依編號）。 */
export function mealsOfDay(meals: readonly Meal[], date: string): Meal[] {
  return meals
    .filter((m) => m.date === date)
    .sort((a, b) => a.at.localeCompare(b.at) || a.id.localeCompare(b.id, "en", { numeric: true }));
}

export interface Progress {
  /** 0 以上；超過 1 代表超過目標。 */
  ratio: number;
  over: boolean;
  /** 目標與目前的差（目標 - 目前；負數＝超過）。 */
  left: number;
}

/** 目標 <= 0 代表沒設目標，回 null。 */
export function progress(value: number, target: number): Progress | null {
  if (!(target > 0)) return null;
  return { ratio: value / target, over: value > target, left: target - value };
}

// ---- 趨勢 ----

export interface TrendDay {
  date: string;
  /** 沒有任何記錄的日子是 null（畫面留空，不畫 0）。 */
  kcal: number | null;
  protein: number | null;
  count: number;
}

/** 到 end（含）為止的 n 天，由舊到新；沒有記錄的日子 kcal、protein 是 null。 */
export function trendDays(meals: readonly Meal[], end: string, n: number): TrendDay[] {
  const byDay = new Map<string, DayTotals>();
  for (const m of meals) {
    const t = byDay.get(m.date) ?? { kcal: 0, protein: 0, carb: 0, fat: 0, count: 0 };
    t.kcal += m.kcal;
    t.protein += m.protein;
    t.count += 1;
    byDay.set(m.date, t);
  }
  const out: TrendDay[] = [];
  for (let i = n - 1; i >= 0; i--) {
    const date = shiftDate(end, -i);
    const t = byDay.get(date);
    out.push(
      t
        ? { date, kcal: t.kcal, protein: t.protein, count: t.count }
        : { date, kcal: null, protein: null, count: 0 },
    );
  }
  return out;
}

export interface Average {
  kcal: number;
  protein: number;
  /** 有記錄的天數（分母）。 */
  days: number;
}

/** 最近 n 天（取 days 尾端）的平均：只算有記錄的日子；一天都沒有回 null。 */
export function recentAverage(days: readonly TrendDay[], n: number): Average | null {
  const rows = days.slice(-n).filter((d) => d.kcal !== null);
  if (rows.length === 0) return null;
  const sum = rows.reduce(
    (a, d) => ({ kcal: a.kcal + (d.kcal ?? 0), protein: a.protein + (d.protein ?? 0) }),
    { kcal: 0, protein: 0 },
  );
  return { kcal: sum.kcal / rows.length, protein: sum.protein / rows.length, days: rows.length };
}

// ---- 常吃餐點 ----

/** 把常吃餐點變成新餐點要寫的欄位（date、at、meal 依現在時間）。 */
export function mealFieldsFromFav(fav: Fav, now: Date): Record<string, unknown> {
  return {
    date: dateStr(now),
    at: atStr(now),
    meal: guessMeal(now),
    kcal: fav.kcal,
    protein: fav.protein,
    carb: fav.carb,
    fat: fav.fat,
    items: fav.items,
    fav: fav.id,
  };
}

/** 把一餐存成常吃餐點要寫的欄位。 */
export function favFieldsFromMeal(m: Meal, name: string): Record<string, unknown> {
  return {
    name,
    kcal: m.kcal,
    protein: m.protein,
    carb: m.carb,
    fat: m.fat,
    items: m.items,
  };
}

/** 沒設目標時進度卡上的那句（NUT-9）：看的是別天就說「這天」，不說「今天」。 */
export function noGoalText(date: string, today: string, t: Macros): string {
  const day = date === today ? "今天" : "這天";
  return `還沒設目標。${day}累計 ${fmt(t.kcal)} 大卡、蛋白質 ${fmt(t.protein)} 公克。`;
}

/** 這天沒有記錄時的提示（NUT-9）：沒有常吃餐點就不叫人「點上面的常吃餐點」。 */
export function emptyDayHint(agentName: string, hasFavs: boolean): string {
  if (agentName) return `把餐點照片貼給 ${agentName}，就會幫你估算並記下來`;
  return hasFavs ? "請秘書幫你記，或點上面的常吃餐點" : "請秘書幫你記";
}

/** 顯示用的數字：整數不帶小數，其他最多一位。 */
export function fmt(n: number): string {
  return Number.isInteger(n) ? String(n) : (Math.round(n * 10) / 10).toString();
}
