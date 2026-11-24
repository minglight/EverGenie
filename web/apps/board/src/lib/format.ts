import { t } from "../i18n/zh-TW";

const relative = new Intl.RelativeTimeFormat("zh-TW", { numeric: "always" });

/** 伺服器的「沒有時間」是 Go 的零值（0001-01-01…）；它跟空字串、壞掉的字串一樣都當作沒有。 */
export function parseIso(iso: string | undefined): Date | null {
  if (!iso) return null;
  const at = new Date(iso);
  if (Number.isNaN(at.getTime()) || at.getFullYear() < 1971) return null;
  return at;
}

function seconds(iso: string | undefined, now: Date): number | null {
  const at = parseIso(iso);
  if (!at) return null;
  return Math.max(0, (now.getTime() - at.getTime()) / 1000);
}

/** 「3 分鐘前」「2 小時前」；不到一分鐘「剛剛」。時間一律用瀏覽器的時區（定案 15）。 */
export function relativeAgo(iso: string | undefined, now: Date): string {
  const sec = seconds(iso, now);
  if (sec === null) return "";
  if (sec < 60) return t("time.justNow");
  const min = Math.floor(sec / 60);
  if (min < 60) return relative.format(-min, "minute");
  const hr = Math.floor(min / 60);
  if (hr < 24) return relative.format(-hr, "hour");
  return relative.format(-Math.floor(hr / 24), "day");
}

/** 「2 小時」「3 天」這種時間長度。 */
export function durationText(sec: number): string {
  const min = Math.floor(sec / 60);
  if (min < 60) return t("time.unit.minute", { n: Math.max(1, min) });
  const hr = Math.floor(min / 60);
  if (hr < 24) return t("time.unit.hour", { n: hr });
  return t("time.unit.day", { n: Math.floor(hr / 24) });
}

/** 「等了 2 小時」；沒有開始時間就用 fallbackDays（伺服器算的等待天數）。 */
export function waitedFor(iso: string | undefined, now: Date, fallbackDays = 0): string {
  const sec = seconds(iso, now);
  if (sec === null) {
    return fallbackDays > 0 ? t("time.waited", { d: durationText(fallbackDays * 86400) }) : "";
  }
  return t("time.waited", { d: durationText(sec) });
}

export type GreetingPart = "morning" | "afternoon" | "evening";

/** 05–11 早安、11–18 午安，其餘晚安。 */
export function greetingPart(now: Date): GreetingPart {
  const h = now.getHours();
  if (h >= 5 && h < 11) return "morning";
  if (h >= 11 && h < 18) return "afternoon";
  return "evening";
}

/** 「10 月 24 日（六）」。 */
export function dateLabel(now: Date): string {
  const weekday = new Intl.DateTimeFormat("zh-TW", { weekday: "narrow" }).format(now);
  return `${now.getMonth() + 1} 月 ${now.getDate()} 日（${weekday}）`;
}

const clock = new Intl.DateTimeFormat("zh-TW", {
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

/** 「10:00」。 */
export function clockText(iso: string | undefined): string {
  const at = parseIso(iso);
  return at ? clock.format(at) : "";
}

const narrowWeekday = new Intl.DateTimeFormat("zh-TW", { weekday: "narrow" });
const pad2 = (n: number) => String(n).padStart(2, "0");

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

function dayDiff(at: Date, now: Date): number {
  const a = new Date(at.getFullYear(), at.getMonth(), at.getDate()).getTime();
  const b = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  return Math.round((a - b) / 86_400_000);
}

/** 清單右邊等寬字的時間：今天「10:00」、明天「明天 10:00」、一週內「週三 19:00」、其他「10/28 14:00」。 */
export function timeLabel(iso: string | undefined, now: Date): string {
  const at = parseIso(iso);
  if (!at) return "";
  const hm = clock.format(at);
  const diff = dayDiff(at, now);
  if (diff === 0) return hm;
  if (diff === 1) return `${t("time.tomorrow")} ${hm}`;
  if (diff === -1) return `${t("time.yesterday")} ${hm}`;
  if (diff > 1 && diff < 7) return `${t("time.weekday", { d: narrowWeekday.format(at) })} ${hm}`;
  return `${at.getMonth() + 1}/${at.getDate()} ${hm}`;
}

/** 清單分組標題：今天、明天、昨天，其他「10/28（三）」。 */
export function dayGroupLabel(at: Date, now: Date): string {
  const diff = dayDiff(at, now);
  if (diff === 0) return t("time.today");
  if (diff === 1) return t("time.tomorrow");
  if (diff === -1) return t("time.yesterday");
  return `${at.getMonth() + 1}/${at.getDate()}（${narrowWeekday.format(at)}）`;
}

/** WhenField 的解析結果：「→ 10/25（六）09:00」。 */
export function describeWhen(at: Date, now: Date = new Date()): string {
  const year = at.getFullYear() === now.getFullYear() ? "" : `${at.getFullYear()}/`;
  return `→ ${year}${at.getMonth() + 1}/${at.getDate()}（${narrowWeekday.format(at)}）${clock.format(at)}`;
}

/** 寫回輸入框的字，when.ts 讀得回來（含年份，過去的日期也不會被當成明年）。 */
export function whenInputText(at: Date): string {
  return `${at.getFullYear()}/${at.getMonth() + 1}/${at.getDate()} ${pad2(at.getHours())}:${pad2(at.getMinutes())}`;
}

/** 完整的日期時間（詳細頁用）：「10/25（六）09:00」。 */
export function fullTime(iso: string | undefined, now: Date = new Date()): string {
  const at = parseIso(iso);
  return at ? describeWhen(at, now).slice(2) : "";
}

export { sameDay };

/**
 * 討論串裡的時間標記（REQ-157）：同一天「09:05」、昨天「昨天 09:05」、7 天內「週三 09:05」、
 * 同一年「10/2 09:05」、更早「2025/10/2 09:05」。24 小時制、補 0、瀏覽器時區。
 */
export function timeMark(iso: string | undefined, now: Date): string {
  const at = parseIso(iso);
  if (!at) return "";
  const hm = `${pad2(at.getHours())}:${pad2(at.getMinutes())}`;
  const diff = dayDiff(at, now);
  if (diff === 0) return hm;
  if (diff === -1) return `${t("time.yesterday")} ${hm}`;
  if (diff < -1 && diff > -7) return `${t("time.weekday", { d: narrowWeekday.format(at) })} ${hm}`;
  const md = `${at.getMonth() + 1}/${at.getDate()} ${hm}`;
  return at.getFullYear() === now.getFullYear() ? md : `${at.getFullYear()}/${md}`;
}

const MD_LINE_START = /^(?:[ \t]*(?:#{1,6}[ \t]+|>[ \t]*|[-*][ \t]+|\d+\.[ \t]+))+/gm;

/**
 * 去掉 Markdown 符號，只留給人讀的字（清單預覽用）。
 * 順序：圖片與連結、行內程式與 code fence、成對的強調符號、行首的符號；單獨一個 `*` 保留（`3 * 4`），
 * 被截斷而落單的 `**`、`__`、`~~` 直接去掉。
 */
export function stripMarkdown(text: string | undefined): string {
  return (text ?? "")
    .replace(/!\[[^\]\n]*\]\([^)\n]*\)/g, t("preview.image"))
    .replace(/\[([^\]\n]*)\]\([^)\n]*\)/g, "$1")
    .replace(/^[ \t]*```[\w+-]*[ \t]*$/gm, "")
    .replace(/`+([^`\n]*)`+/g, "$1")
    .replace(/```/g, "")
    .replace(/\*\*(.+?)\*\*/g, "$1")
    .replace(/__(.+?)__/g, "$1")
    .replace(/~~(.+?)~~/g, "$1")
    .replace(/\*(\S(?:[^*\n]*?\S)?)\*/g, "$1")
    .replace(/(^|[^\w])_(\S(?:[^_\n]*?\S)?)_(?=$|[^\w])/gm, "$1$2")
    .replace(MD_LINE_START, "")
    .replace(/\*\*|__|~~/g, "");
}

/** 前 n 個字（先去掉 Markdown 符號、合併空白），超過加「…」。 */
export function preview(text: string | undefined, n = 60): string {
  const chars = Array.from(stripMarkdown(text).replace(/\s+/g, " ").trim());
  return chars.length > n ? `${chars.slice(0, n).join("")}…` : chars.join("");
}

/** 檔案大小：「512 B」「3.4 MB」。 */
export function bytesText(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 1024) return `${Math.max(0, Math.round(bytes || 0))} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let n = bytes / 1024;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n >= 10 ? Math.round(n) : n.toFixed(1)} ${units[i]}`;
}
