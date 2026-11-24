// 日期小工具（本地時區，YYYY-MM-DD／YYYY-MM-DD HH:MM 字串）；沒有 I/O。
const p2 = (n: number) => String(n).padStart(2, "0");

export function dateStr(d: Date): string {
  return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`;
}

export function atStr(d: Date): string {
  return `${dateStr(d)} ${p2(d.getHours())}:${p2(d.getMinutes())}`;
}

export function isDateStr(s: string | null | undefined): s is string {
  if (!s || !/^\d{4}-\d{2}-\d{2}$/.test(s)) return false;
  const [y, m, d] = s.split("-").map(Number) as [number, number, number];
  const t = new Date(y, m - 1, d);
  return t.getFullYear() === y && t.getMonth() === m - 1 && t.getDate() === d;
}

export function addDays(date: string, days: number): string {
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  return dateStr(new Date(y, m - 1, d + days));
}

/** 這個日期那一週的週一。 */
export function mondayOf(date: string): string {
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  const wd = (new Date(y, m - 1, d).getDay() + 6) % 7; // 週一 = 0
  return addDays(date, -wd);
}

/** "YYYY-MM-DD HH:MM"（本地時間）換成 Date；格式不對回 null。 */
export function parseAt(s: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/.exec(s);
  if (!m) return null;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), Number(m[4]), Number(m[5]));
}

const WD = "日一二三四五六";
/** 例：10/25 週日。 */
export function dayLabel(date: string): string {
  if (!isDateStr(date)) return date;
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  return `${m}/${d} 週${WD[new Date(y, m - 1, d).getDay()]}`;
}
