// 從一句話裡抓時間（FEN-15、補充契約 S5 第 2、6 點）。用瀏覽器的時區；解析不到就回 null、不動句子。
// 認得：今天／今晚／明天／後天／昨天／前天／下週一…日／週三、早上／下午／晚上、「9」「9 點」「9:30」「9 點半」、
// 「3h」「30m」「3 小時後」「30 分鐘後」「2 天後」、「10/28」「10/28 14:00」「2026/10/28」。

export interface WhenParse {
  /** 解析出來的時間。 */
  date: Date;
  /** 句子拿掉時間那幾段之後剩下的字（前後的空白與標點已整理）。 */
  rest: string;
  /** 被拿掉的原文（顯示用）。 */
  matched: string;
}

interface Span {
  start: number;
  end: number;
}

const WEEKDAYS: Record<string, number> = {
  日: 0,
  天: 0,
  一: 1,
  二: 2,
  三: 3,
  四: 4,
  五: 5,
  六: 6,
};

// 這些字可以接在「今天／週三」後面當一個沒有標點的數字（「明天 9」）；若後面接的是這些，數字就不是時間。
const NOT_HOUR_TAIL = "[\\d:：/月日號号點点時时分年天週周人位個个份次%]";
const NOT_HOUR_AFTER = `(?!\\s*${NOT_HOUR_TAIL})`;

const NAMED_DAYS: { re: string; offset: number; period?: Period }[] = [
  { re: "大[後后]天", offset: 3 },
  { re: "[後后]天", offset: 2 },
  { re: "明[天日]", offset: 1 },
  { re: "今[天日]", offset: 0 },
  { re: "今晚", offset: 0, period: "evening" },
  { re: "明晚", offset: 1, period: "evening" },
  { re: "今早", offset: 0, period: "morning" },
  { re: "明早", offset: 1, period: "morning" },
  // 說的是過去（「昨天 3 點」）：照說的那天，不當成今天的 03:00（RMD-20）；記下來是已經過了的待辦。
  // 「前天」常在詞裡（「目前天氣」）：前面是漢字就不算。
  { re: "(?<![\\u4e00-\\u9fff])大前天", offset: -3 },
  { re: "(?<![\\u4e00-\\u9fff])前天", offset: -2 },
  { re: "昨[天日]", offset: -1 },
  { re: "昨晚", offset: -1, period: "evening" },
  { re: "昨早", offset: -1, period: "morning" },
];

type Period = "dawn" | "morning" | "noon" | "afternoon" | "dusk" | "evening";

const PERIODS: { re: string; period: Period }[] = [
  { re: "凌晨", period: "dawn" },
  { re: "清晨|早上|早晨|上午", period: "morning" },
  { re: "中午", period: "noon" },
  { re: "下午", period: "afternoon" },
  { re: "傍晚", period: "dusk" },
  { re: "晚上|晚間|晚间|夜裡|夜里", period: "evening" },
];

const PERIOD_DEFAULT_HOUR: Record<Period, number> = {
  dawn: 6,
  morning: 9,
  noon: 12,
  afternoon: 15,
  dusk: 18,
  evening: 20,
};

function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function addDays(d: Date, n: number): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + n, d.getHours(), d.getMinutes());
}

function validDate(y: number, m: number, d: number): boolean {
  const probe = new Date(y, m - 1, d);
  return probe.getFullYear() === y && probe.getMonth() === m - 1 && probe.getDate() === d;
}

// 整理剩下的字：被拿掉的地方補一個空白、多餘的空白合併、前後的空白與連接標點拿掉。
function tidy(text: string, spans: Span[]): string {
  const sorted = [...spans].sort((a, b) => b.start - a.start);
  let out = text;
  for (const s of sorted) {
    out = `${out.slice(0, s.start)} ${out.slice(s.end)}`;
  }
  return out
    .replace(/\s+/g, " ")
    .replace(/^[\s,，、:：;；\-—]+|[\s,，、:：;；\-—]+$/g, "")
    .trim();
}

function overlaps(a: Span, spans: Span[]): boolean {
  return spans.some((s) => a.start < s.end && s.start < a.end);
}

interface Found<T> extends Span {
  value: T;
}

function firstMatch<T>(
  text: string,
  re: RegExp,
  pick: (m: RegExpMatchArray) => T | null,
  taken: Span[] = [],
): Found<T> | null {
  for (const m of text.matchAll(re)) {
    const start = m.index ?? 0;
    const span = { start, end: start + m[0].length };
    if (overlaps(span, taken)) continue;
    const value = pick(m);
    if (value !== null) return { ...span, value };
  }
  return null;
}

function parseRelative(text: string, now: Date): WhenParse | null {
  type Rel = { minutes: number };
  const candidates: Found<Rel>[] = [];
  const add = (f: Found<Rel> | null) => {
    if (f) candidates.push(f);
  };
  add(firstMatch<Rel>(text, /半\s*(?:個|个)?\s*小[時时]\s*[後后]?/g, () => ({ minutes: 30 })));
  add(
    firstMatch<Rel>(text, /(\d+(?:\.\d+)?)\s*(?:個|个)?\s*小[時时]\s*[後后]?/g, (m) => ({
      minutes: Math.round(Number(m[1]) * 60),
    })),
  );
  add(
    firstMatch<Rel>(
      text,
      /(?<![A-Za-z0-9.])(\d+(?:\.\d+)?)\s*(?:hours?|hrs?|h)(?![A-Za-z0-9])/gi,
      (m) => ({ minutes: Math.round(Number(m[1]) * 60) }),
    ),
  );
  add(
    firstMatch<Rel>(text, /(\d+)\s*分鐘\s*[後后]?/g, (m) => ({
      minutes: Number(m[1]),
    })),
  );
  add(
    firstMatch<Rel>(
      text,
      /(?<![A-Za-z0-9.])(\d+)\s*(?:minutes?|mins?|m)(?![A-Za-z0-9])/gi,
      (m) => ({
        minutes: Number(m[1]),
      }),
    ),
  );
  add(
    firstMatch<Rel>(text, /(\d+)\s*天\s*[後后]/g, (m) => ({
      minutes: Number(m[1]) * 24 * 60,
    })),
  );
  const first = candidates.sort((a, b) => a.start - b.start)[0];
  if (!first || first.value.minutes <= 0) return null;
  const date = new Date(now.getTime() + first.value.minutes * 60_000);
  date.setSeconds(0, 0);
  return {
    date,
    rest: tidy(text, [first]),
    matched: text.slice(first.start, first.end).trim(),
  };
}

/** 讀一句話裡的時間。`now` 預設現在（測試時固定）。 */
export function parseWhen(input: string, now: Date = new Date()): WhenParse | null {
  const text = input;
  if (text.trim() === "") return null;

  const relative = parseRelative(text, now);
  if (relative) return relative;

  const taken: Span[] = [];
  const take = <T>(f: Found<T> | null): Found<T> | null => {
    if (f) taken.push({ start: f.start, end: f.end });
    return f;
  };

  // 日期
  const full = take(
    firstMatch(text, /(\d{4})\s*[/\-年]\s*(\d{1,2})\s*[/\-月]\s*(\d{1,2})\s*[日號号]?/g, (m) => {
      const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
      return validDate(y, mo, d) ? { y, mo, d } : null;
    }),
  );
  const monthDay = full
    ? null
    : take(
        firstMatch(text, /(?<![\d/])(\d{1,2})\s*[/月]\s*(\d{1,2})\s*[日號号]?(?![\d/])/g, (m) => {
          const [mo, d] = [Number(m[1]), Number(m[2])];
          return mo >= 1 && mo <= 12 && d >= 1 && d <= 31 ? { mo, d } : null;
        }),
      );
  const weekday = take(
    firstMatch(text, /(下|這|这|本)?\s*(?:週|周|星期|禮拜|礼拜)\s*([一二三四五六日天])/g, (m) => ({
      next: m[1] === "下",
      wd: WEEKDAYS[m[2] ?? ""] ?? 0,
    })),
  );
  const named = take(
    firstMatch(
      text,
      new RegExp(NAMED_DAYS.map((n) => `(${n.re})`).join("|"), "g"),
      (m) => {
        const at = NAMED_DAYS.findIndex((_, i) => m[i + 1] !== undefined);
        return NAMED_DAYS[at] ?? null;
      },
      taken,
    ),
  );
  const hasDay = Boolean(full || monthDay || weekday || named);

  // 時間
  const colon = firstMatch(
    text,
    /(\d{1,2})\s*[:：]\s*(\d{2})(?!\d)/g,
    (m) => {
      const [h, mi] = [Number(m[1]), Number(m[2])];
      return h <= 23 && mi <= 59 ? { h, mi } : null;
    },
    taken,
  );
  const dian = colon
    ? null
    : firstMatch(
        text,
        /(\d{1,2})\s*(?:點|点|時|时)(?:\s*(半)|\s*(\d{1,2})\s*分|(\d{2})(?![\d個个人位份次]))?/g,
        (m) => {
          const h = Number(m[1]);
          const mi = m[2] ? 30 : Number(m[3] ?? m[4] ?? 0);
          return h <= 24 && mi <= 59 ? { h: h === 24 ? 0 : h, mi } : null;
        },
        taken,
      );
  let clock = colon ?? dian;
  if (clock) taken.push({ start: clock.start, end: clock.end });

  // 時段
  const period = firstMatch(
    text,
    new RegExp(PERIODS.map((p) => `(${p.re})`).join("|"), "g"),
    (m) => {
      const at = PERIODS.findIndex((_, i) => m[i + 1] !== undefined);
      return PERIODS[at]?.period ?? null;
    },
    taken,
  );
  let periodValue: Period | undefined = named?.value.period;
  let periodTaken = false;
  if (period) {
    const after = text.slice(period.end);
    const anchored =
      hasDay ||
      clock !== null ||
      new RegExp(`^\\s*\\d{1,2}${NOT_HOUR_AFTER}`).test(after) ||
      after === "" ||
      /^[\s,，、。.!！?？:：;；]/.test(after);
    if (anchored) {
      taken.push({ start: period.start, end: period.end });
      periodValue = period.value;
      periodTaken = true;
    }
  }

  // 「明天 9」「今晚 9」「晚上 7」：沒有點、沒有冒號的整數，只認接在日期或時段後面的那一個。
  if (!clock) {
    const candidates: (Span | null)[] = [
      named,
      weekday,
      monthDay,
      full,
      periodTaken ? period : null,
    ];
    const anchors = candidates.filter((a): a is Span => a !== null).sort((a, b) => b.end - a.end);
    for (const anchor of anchors) {
      const tail = new RegExp(`^(\\s*)(\\d{1,2})${NOT_HOUR_AFTER}`).exec(text.slice(anchor.end));
      if (!tail) continue;
      const start = anchor.end + (tail[1]?.length ?? 0);
      const span = { start, end: start + (tail[2]?.length ?? 0) };
      const h = Number(tail[2]);
      if (h > 24 || overlaps(span, taken)) continue;
      clock = { ...span, value: { h: h === 24 ? 0 : h, mi: 0 } };
      taken.push(span);
      break;
    }
  }

  if (!hasDay && !clock && !periodValue) return null;

  // 算時間
  const today = startOfDay(now);
  let base = today;
  let explicitDay = hasDay;
  if (full) {
    base = new Date(full.value.y, full.value.mo - 1, full.value.d);
  } else if (monthDay) {
    base = new Date(now.getFullYear(), monthDay.value.mo - 1, monthDay.value.d);
    if (!validDate(base.getFullYear(), monthDay.value.mo, monthDay.value.d)) return null;
    if (base < today) {
      base = new Date(now.getFullYear() + 1, monthDay.value.mo - 1, monthDay.value.d);
    }
  } else if (named) {
    base = addDays(today, named.value.offset);
  } else if (weekday) {
    const { wd, next } = weekday.value;
    if (next) {
      const mondayOffset = (8 - today.getDay()) % 7 || 7; // 下一個週一
      const monday = addDays(today, mondayOffset);
      base = addDays(monday, wd === 0 ? 6 : wd - 1);
    } else {
      base = addDays(today, (wd - today.getDay() + 7) % 7);
    }
  } else {
    explicitDay = false;
  }

  let hour: number;
  let minute = 0;
  if (clock) {
    hour = clock.value.h;
    minute = clock.value.mi;
    switch (periodValue) {
      case "afternoon":
      case "dusk":
      case "evening":
        if (hour < 12) hour += 12;
        break;
      case "noon":
        if (hour <= 5) hour += 12;
        break;
      case "dawn":
        if (hour === 12) hour = 0;
        break;
      default:
        break;
    }
  } else if (periodValue) {
    hour = PERIOD_DEFAULT_HOUR[periodValue];
  } else {
    hour = 9; // 只有日期：早上 9 點（跟「明天早上 09:00」的快捷一致）
  }

  let date = new Date(base.getFullYear(), base.getMonth(), base.getDate(), hour, minute);
  if (!explicitDay) {
    // 只有時間：今天這個時間還沒過就今天，過了就明天。
    if (date.getTime() <= now.getTime()) date = addDays(date, 1);
  } else if (!clock && !periodValue && base.getTime() === today.getTime()) {
    // 只說「今天」：9 點還沒到用 9 點，不然用晚上 8 點，再不然一小時後。
    if (date.getTime() <= now.getTime())
      date = new Date(base.getFullYear(), base.getMonth(), base.getDate(), 20, 0);
    if (date.getTime() <= now.getTime()) {
      date = new Date(now.getTime() + 60 * 60_000);
      date.setMinutes(0, 0, 0);
    }
  } else if (
    weekday &&
    !weekday.value.next &&
    base.getTime() === today.getTime() &&
    date.getTime() <= now.getTime()
  ) {
    date = addDays(date, 7);
  }

  const matchedSpans = [...taken];
  const matched = matchedSpans
    .sort((a, b) => a.start - b.start)
    .map((s) => text.slice(s.start, s.end).trim())
    .join(" ");
  return { date, rest: tidy(text, matchedSpans), matched };
}

/** 只要時間（給 WhenField 用）：整句都當時間讀。 */
export function parseWhenOnly(input: string, now: Date = new Date()): Date | null {
  return parseWhen(input, now)?.date ?? null;
}
