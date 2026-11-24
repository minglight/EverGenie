// 日期小工具（本地時區，YYYY-MM-DD 字串）；沒有 I/O。
const p2 = (n: number) => String(n).padStart(2, "0");

export function dateStr(d: Date): string {
  return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`;
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

const WD = "日一二三四五六";
/** 例：10/25 週日。 */
export function dayLabel(date: string): string {
  if (!isDateStr(date)) return date;
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  return `${m}/${d} 週${WD[new Date(y, m - 1, d).getDay()]}`;
}
