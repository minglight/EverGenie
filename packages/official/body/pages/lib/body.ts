// 身體數據的純函式（REQ-129）：型別、輸入檢查、近 7 天平均 vs 前 7 天、趨勢線。沒有 I/O。
// 趨勢看 7 天平均，不看單日（水分、鹽分都會讓單日跳 1–2 公斤）。
import { addDays, dayLabel } from "./dates";

export interface RecordLike {
  id?: string;
  fields?: Record<string, unknown>;
}

function num(v: unknown): number {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    if (Number.isFinite(n)) return n;
  }
  return 0;
}

export type BodyKey = "weight" | "fat" | "bmi" | "bmr" | "visceral" | "score";

export interface BodyEntry {
  /** 編號就是日期。 */
  date: string;
  weight: number | null;
  fat: number | null;
  bmi: number | null;
  bmr: number | null;
  visceral: number | null;
  score: number | null;
  note: string;
}

/** 主指標（看 7 天平均）。 */
export const MAIN_KEYS = ["weight", "fat"] as const;
/** 只看最新值的欄位。 */
export const EXTRA_KEYS = ["bmi", "bmr", "visceral", "score"] as const;

/** 欄位的白話名稱、單位與合理範圍（量錯、打錯才擋得到）。 */
export const METRICS: Record<BodyKey, { label: string; unit: string; min: number; max: number }> = {
  weight: { label: "體重", unit: "kg", min: 20, max: 300 },
  fat: { label: "體脂", unit: "%", min: 3, max: 60 },
  bmi: { label: "BMI", unit: "", min: 10, max: 60 },
  bmr: { label: "基礎代謝", unit: "大卡", min: 500, max: 4000 },
  visceral: { label: "內臟脂肪", unit: "級", min: 0, max: 60 },
  score: { label: "身體得分", unit: "分", min: 0, max: 100 },
};

function opt(v: unknown): number | null {
  if (v === null || v === undefined || v === "") return null;
  return typeof v === "number" || typeof v === "string" ? num(v) : null;
}

export function toBody(r: RecordLike): BodyEntry {
  const f = r.fields ?? {};
  return {
    date: typeof f.date === "string" && f.date ? f.date : (r.id ?? ""),
    weight: opt(f.weight),
    fat: opt(f.fat),
    bmi: opt(f.bmi),
    bmr: opt(f.bmr),
    visceral: opt(f.visceral),
    score: opt(f.score),
    note: typeof f.note === "string" ? f.note : "",
  };
}

/** 日期新→舊。 */
export function sortBody(items: readonly BodyEntry[]): BodyEntry[] {
  return [...items].sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : 0));
}

const round1 = (n: number) => Math.round(n * 10) / 10;

/** 輸入檢查：空白＝沒填；有填要在範圍內，四捨五入到小數 1 位。 */
export function checkMetric(key: BodyKey, raw: string): { value: number | null; error: string } {
  const t = raw.trim();
  const m = METRICS[key];
  if (t === "") return { value: null, error: "" };
  const n = Number(t);
  if (!Number.isFinite(n)) return { value: null, error: `${m.label}要是數字` };
  if (n < m.min || n > m.max) return { value: null, error: `${m.label}要在 ${m.min}–${m.max}` };
  return { value: round1(n), error: "" };
}

function avg(rows: readonly BodyEntry[], key: BodyKey): number | null {
  const v = rows.map((x) => x[key]).filter((x): x is number => x !== null);
  return v.length > 0 ? round1(v.reduce((a, b) => a + b, 0) / v.length) : null;
}

export interface MainSummary {
  avg7: number | null;
  prev7: number | null;
  delta: number | null;
  latest: number | null;
}

export interface BodySummary {
  latest: BodyEntry | null;
  weight: MainSummary;
  fat: MainSummary;
}

/** 最新一筆、近 7 天平均、跟再前 7 天平均的差（沒有就是 null）。 */
export function summarize(items: readonly BodyEntry[], today: string): BodySummary {
  const sorted = sortBody(items);
  const win = (a: number, c: number) =>
    sorted.filter((x) => addDays(today, -c) < x.date && x.date <= addDays(today, -a));
  const cur = win(0, 7);
  const prev = win(7, 14);
  const main = (k: "weight" | "fat"): MainSummary => {
    const a = avg(cur, k);
    const p = avg(prev, k);
    return {
      avg7: a,
      prev7: p,
      delta: a !== null && p !== null ? round1(a - p) : null,
      latest: sorted.find((x) => x[k] !== null)?.[k] ?? null,
    };
  };
  return { latest: sorted[0] ?? null, weight: main("weight"), fat: main("fat") };
}

/** 最新一筆有這個欄位的日期（沒有就是 null）。 */
export function latestDateOf(items: readonly BodyEntry[], key: BodyKey): string | null {
  return sortBody(items).find((x) => x[key] !== null)?.date ?? null;
}

/**
 * 今天頁卡片上「這個數字是哪天量的」（BOD-5）：今天量的不寫；昨天寫「昨天」；更早寫「11/1 週日」。
 * 卡片標題是「今天的身體」，三週前的體重不標日期會被當成今天的。
 */
export function measuredLabel(date: string, today: string): string {
  if (date === today) return "";
  if (date === addDays(today, -1)) return "昨天";
  return dayLabel(date);
}

export interface TrendPoint {
  date: string;
  v: number;
  /** 那天往前 days 天（含當天）的平均。 */
  avg: number;
}

/** 每個有記錄的日子：當天數字和 7 天平均（畫趨勢圖用）。舊→新；沒記錄的日子不補。 */
export function trendLine(
  items: readonly BodyEntry[],
  key: "weight" | "fat",
  days = 7,
): TrendPoint[] {
  const rows = [...items].filter((x) => x[key] !== null).sort((a, b) => (a.date < b.date ? -1 : 1));
  return rows.map((x) => {
    const near = rows.filter((y) => addDays(x.date, -days) < y.date && y.date <= x.date);
    const sum = near.reduce((s, y) => s + (y[key] ?? 0), 0);
    return { date: x.date, v: x[key] ?? 0, avg: Math.round((sum / near.length) * 100) / 100 };
  });
}

/** 一筆的白話摘要，例：體重 72.4 kg　體脂 18.5%。 */
export function describeBody(x: BodyEntry): string {
  const keys: BodyKey[] = ["weight", "fat", "bmi", "bmr", "visceral", "score"];
  return keys
    .filter((k) => x[k] !== null)
    .map((k) => {
      const m = METRICS[k];
      const u = m.unit === "%" ? "%" : m.unit ? ` ${m.unit}` : "";
      return `${m.label} ${x[k]}${u}`;
    })
    .join("　");
}
