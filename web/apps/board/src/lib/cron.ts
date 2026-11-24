// 排程時間的白話與編輯用的轉換（任務單 → 排程，REQ-18）。只認最常見的兩種（每天、每週幾）；
// 其他的 cron 原樣顯示、在「進階」裡編輯，是否合法交給伺服器（FEN-N3）。
import { t } from "../i18n/zh-TW";

/** 星期的顯示順序：週一到週日（cron 的 0 是週日）。 */
export const WEEK_ORDER = [1, 2, 3, 4, 5, 6, 0] as const;
const DAY_CHAR = ["日", "一", "二", "三", "四", "五", "六"] as const;

export interface SimpleSchedule {
  /** 空陣列＝每天。 */
  days: number[];
  /** 24 小時制 HH:MM，由早到晚。 */
  times: string[];
}

const pad = (n: number) => String(n).padStart(2, "0");
const INT = /^\d{1,2}$/;

function parseDays(field: string): number[] | null {
  if (field === "*") return [];
  const out = new Set<number>();
  for (const part of field.split(",")) {
    const range = /^(\d)-(\d)$/.exec(part);
    if (range) {
      const a = Number(range[1]);
      const b = Number(range[2]);
      if (a > 7 || b > 7 || a > b) return null;
      for (let d = a; d <= b; d++) out.add(d % 7);
    } else if (/^\d$/.test(part) && Number(part) <= 7) {
      out.add(Number(part) % 7);
    } else {
      return null;
    }
  }
  return out.size === 7 ? [] : [...out].sort((x, y) => x - y);
}

/** 單一 cron 是不是「每天」或「每週幾」的簡單寫法；不是就回 null。 */
export function parseSimple(spec: string): SimpleSchedule | null {
  const f = spec.trim().split(/\s+/);
  if (f.length !== 5 || f[2] !== "*" || f[3] !== "*") return null;
  const [minute, hours, , , dow] = f as [string, string, string, string, string];
  if (!INT.test(minute) || Number(minute) > 59) return null;
  const hs = hours.split(",");
  if (hs.some((h) => !INT.test(h) || Number(h) > 23)) return null;
  const days = parseDays(dow);
  if (days === null) return null;
  const times = [...new Set(hs.map((h) => `${pad(Number(h))}:${pad(Number(minute))}`))].sort();
  return { days, times };
}

/** 每天（days 空）或每週幾的 cron；time 是 HH:MM。 */
export function buildCron(days: readonly number[], time: string): string {
  const [h = "0", m = "0"] = time.split(":");
  const dow = days.length === 0 ? "*" : [...new Set(days)].sort((a, b) => a - b).join(",");
  return `${Number(m)} ${Number(h)} * * ${dow}`;
}

/** 週一到週日的順序排好的星期字：[1,3,5] → 「一三五」。 */
export function dayChars(days: readonly number[]): string {
  return WEEK_ORDER.filter((d) => days.includes(d))
    .map((d) => DAY_CHAR[d])
    .join("");
}

export function dayName(day: number): string {
  return t("cron.day", { d: DAY_CHAR[day % 7] ?? "" });
}

/**
 * 每隔一段時間的寫法（WEB-95）：「* * * * *」每分鐘、「*\/N * * * *」每 N 分鐘、「M * * * *」每小時 :MM、
 * 「M *\/N * * *」每 N 小時 :MM；日、月、星期都要是「*」。N 要能整除 60（分）或 24（時），不然跨整點、跨午夜那次間隔會變短
 * （*\/7 是 0、7…56 分，56→0 只隔 4 分），寫「每 N」就不對，原樣顯示。不是這幾種回 null。
 */
function describeEvery(spec: string): string | null {
  const f = spec.trim().split(/\s+/);
  if (f.length !== 5 || f[2] !== "*" || f[3] !== "*" || f[4] !== "*") return null;
  const [minute, hour] = f as [string, string];
  const step = (x: string) => {
    const m = /^\*\/(\d{1,2})$/.exec(x);
    return m ? Number(m[1]) : null;
  };
  if (hour === "*") {
    if (minute === "*") return t("cron.everyMinute");
    const n = step(minute);
    if (n !== null && n >= 1 && n <= 59 && 60 % n === 0) return t("cron.everyMinutes", { n });
    if (INT.test(minute) && Number(minute) <= 59)
      return t("cron.hourly", { m: pad(Number(minute)) });
    return null;
  }
  const h = step(hour);
  if (h !== null && h >= 1 && h <= 23 && 24 % h === 0 && INT.test(minute) && Number(minute) <= 59) {
    return t("cron.everyHours", { n: h, m: pad(Number(minute)) });
  }
  return null;
}

/** 一組 cron 的白話：「每天 08:00」「每週一三五 15:00」「每 30 分鐘」；看不懂的原樣。 */
export function describeCron(spec: string): string {
  const s = parseSimple(spec);
  if (!s) return describeEvery(spec) ?? spec.trim();
  const at = s.times.join("、");
  return s.days.length === 0
    ? t("cron.daily", { at })
    : t("cron.weekly", { days: dayChars(s.days), at });
}

/** 多組 cron 一起寫，用「；」隔開。 */
export function describeCrons(specs: readonly string[]): string {
  return specs
    .map((x) => x.trim())
    .filter(Boolean)
    .map(describeCron)
    .join("；");
}

/** 時段型排程的白話：「時段 10:00–12:00、每天最多 2 次」。 */
export function describeWindows(
  windows: readonly string[],
  perDay: number | undefined,
  weekend?: readonly string[],
): string {
  const range = (w: string) => w.replace("-", "–");
  let text = t("cron.windows", { w: windows.map(range).join("、"), n: perDay || 1 });
  if (weekend && weekend.length > 0) {
    text += t("cron.windows.weekend", { w: weekend.map(range).join("、") });
  }
  return text;
}

export interface JobTimeSource {
  cron?: string[];
  windows?: string[];
  weekend_windows?: string[];
  per_day?: number;
}

/** 一個排程的時間白話（清單與詳情用）。 */
export function describeJobTime(job: JobTimeSource): string {
  if (job.windows && job.windows.length > 0) {
    return describeWindows(job.windows, job.per_day, job.weekend_windows);
  }
  return describeCrons(job.cron ?? []);
}

export type TimeMode = "daily" | "weekly" | "advanced";

/** 目前的 cron 該用哪個編輯分頁開：剛好一組簡單寫法才用「每天／每週幾」，其他進「進階」。 */
export function modeOf(specs: readonly string[]): TimeMode {
  if (specs.length !== 1) return specs.length === 0 ? "daily" : "advanced";
  const s = parseSimple(specs[0] ?? "");
  if (!s || s.times.length !== 1) return "advanced";
  return s.days.length === 0 ? "daily" : "weekly";
}
