// 習慣套件頁面的純函式（REQ-124）：把 vault 記錄轉成頁面用的形狀、存檔前的檢查（跟程式 internal/rules 同一套規則）、進度文字。
// 沒有任何 I/O。頁面不算點數：點數、連續週數都由套件的程式算好寫進 habits.status／habits.wallet，這裡只讀。

export const CADENCES = [
  { value: "daily", label: "每天" },
  { value: "weekly", label: "每週" },
] as const;
export const MODES = [
  { value: "cumulative", label: "累計" },
  { value: "streak", label: "連續" },
] as const;
export const STATUSES = [
  { value: "active", label: "進行中" },
  { value: "ended", label: "已結束" },
] as const;

export const SUB_SOURCES = ["body:weight", "nutrition:meals"] as const;

/** vault 記錄的最小形狀（不 import sdk 型別，讓這個檔能被單元測試直接載入）。 */
export interface RecordLike {
  id?: string;
  fields?: Record<string, unknown>;
  body?: string;
}

export interface Habit {
  id: string;
  name: string;
  agent: string;
  cadence: string;
  target: number;
  missDays: number;
  weekSkips: number;
  mode: string;
  grace: number;
  start: string;
  status: string;
  ended: string;
  identity: string;
  cue: string;
  twoMin: string;
  sources: string[];
  subtasks: string[];
  allowMiss: number;
  /** 連續滿幾週加 1 點（沒寫＝4；0＝不加分，DEV-142）。 */
  bonusWeeks: number;
  body: string;
}

export interface WeekCell {
  week: string;
  count: number;
  result: string;
}

export interface SubToday {
  name: string;
  done: boolean;
  how: string;
}

export interface HabitStatus {
  habit: string;
  streakWeeks: number;
  achievedWeeks: number;
  earnedPoints: number;
  /** 離下一點還差幾週。 */
  nextNeedWeeks: number;
  /** 連續滿幾週加 1 點（0＝不加分）。 */
  bonusWeeks: number;
  /** 連續加分拿到的點（已含在 earnedPoints 裡）。 */
  bonusPoints: number;
  /** 再連續幾週拿到下一次連續加分。 */
  nextBonusWeeks: number;
  done: number;
  need: number;
  days: string[];
  weeks: WeekCell[];
  todaySubs: SubToday[];
  todayManual: number;
  doneDays: string[];
  skippedDays: string[];
  brokenDays: string[];
  manualDays: string[];
  computedAt: string;
}

export interface Wallet {
  earned: number;
  spent: number;
  points: number;
  /** 待辦累積的點數（完成待辦就往上加，只增不減）。 */
  taskPoints: number;
  /** 已經換成錢包點的待辦點數（每 TASK_POINTS 點換 1，HAB-18）。 */
  taskExchanged: number;
  /**
   * 點數現在換不換得到衣服（錢包的 outfits_ready，HAB-23）：套件的程式算好的唯一判斷——衣櫃在用、而且有上架要點數的衣服。
   * 沒有時頁面不承諾「換衣服」。舊錢包沒有這欄當 false（寧可少說）。
   */
  outfitsReady: boolean;
}

/** 還沒換的待辦點數，以及現在能不能按「兌換」（滿 TASK_POINTS 才行，HAB-18）。 */
export function taskLeft(w: Wallet): { left: number; canExchange: boolean } {
  const left = Math.max(w.taskPoints - w.taskExchanged, 0);
  return { left, canExchange: left >= TASK_POINTS };
}

/** 待辦累積滿這麼多換 1 點（DEV-149 作者 2026-11-12 從 20 改 30；跟 internal/rules 的 TaskPointsPerWalletPoint 一致）。 */
export const TASK_POINTS = 30;

/**
 * 待辦頁那張卡的下一步（DEV-156 U-14）：畫面上只有一種「點數」（錢包、換衣服用的），
 * 待辦做完攢的叫「累積」——滿 TASK_POINTS 換 1 點。不寫「做完 30 件」：一筆給 1～3 再加過期（RMD-26）。
 */
export function todoNextLine(w: Wallet): string {
  const { left, canExchange } = taskLeft(w);
  if (canExchange) return `累積滿了，可以換 ${Math.floor(left / TASK_POINTS)} 點`;
  return `再累積 ${TASK_POINTS - left} 就多 1 點`;
}

/** 這一份累積到哪（進度條用）：0～TASK_POINTS；滿了就是 TASK_POINTS。 */
export function todoProgress(w: Wallet): number {
  return Math.min(taskLeft(w).left, TASK_POINTS);
}

/** 「怎麼算」點開的完整規則（跟 RMD-26、HAB-18 一致）。換不到衣服時不承諾換衣服（HAB-23、U-09）。 */
export function todoHowLines(outfitsReady: boolean): string[] {
  return [
    "跟進方式是「問我做了沒」或「秘書幫我確認」的待辦，做完才會累積；只是提醒的不算。",
    "一筆累積多少看難度：簡單 1、普通 2、困難 3；拖過期的每滿一週再多 1。會累積的待辦，清單上那一筆寫著「累積 +幾」。",
    `累積滿 ${TASK_POINTS} 按「換成 1 點」，就多 1 點${outfitsReady ? "，點數可以到展示間換衣服" : ""}。累積不會不見，可以先存著。`,
  ];
}
/** 連續加分週數的預設（跟 internal/rules 一致）。 */
export const DEFAULT_BONUS_WEEKS = 4;
/** 每天型每週跳過上限的預設（跟 internal/rules 的 DefaultWeekSkips 一致）。 */
export const DEFAULT_WEEK_SKIPS = 2;

export function num(v: unknown, fallback = 0): number {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    if (Number.isFinite(n)) return n;
  }
  return fallback;
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const strs = (v: unknown): string[] =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
const obj = (v: unknown): Record<string, unknown> =>
  v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};

export function toHabit(r: RecordLike): Habit {
  const f = r.fields ?? {};
  const cadence = str(f.cadence);
  return {
    id: r.id ?? "",
    name: str(f.name),
    agent: str(f.agent),
    cadence,
    target: num(f.target, cadence === "daily" ? 7 : 1),
    missDays: num(f.miss_days, 1),
    weekSkips: num(f.week_skips, DEFAULT_WEEK_SKIPS),
    mode: str(f.mode) || "cumulative",
    grace: num(f.grace, 1),
    start: str(f.start).slice(0, 10),
    status: str(f.status) || "active",
    ended: str(f.ended).slice(0, 10),
    identity: str(f.identity),
    cue: str(f.cue),
    twoMin: str(f.two_min),
    sources: strs(f.sources),
    subtasks: strs(f.subtasks),
    allowMiss: num(f.allow_miss, 0),
    bonusWeeks: num(f.bonus_weeks, DEFAULT_BONUS_WEEKS),
    body: r.body ?? "",
  };
}

export function toStatus(r: RecordLike): HabitStatus {
  const f = r.fields ?? {};
  const week = obj(f.this_week);
  return {
    habit: str(f.habit),
    streakWeeks: num(f.streak_weeks),
    achievedWeeks: num(f.achieved_weeks),
    earnedPoints: num(f.earned_points),
    nextNeedWeeks: num(f.next_need_weeks),
    bonusWeeks: num(f.bonus_weeks, DEFAULT_BONUS_WEEKS),
    bonusPoints: num(f.bonus_points),
    nextBonusWeeks: num(f.next_bonus_weeks),
    done: num(week.done),
    need: num(week.need),
    days: strs(week.days),
    weeks: (Array.isArray(f.weeks) ? f.weeks : []).map((w) => {
      const o = obj(w);
      return { week: str(o.week), count: num(o.count), result: str(o.result) };
    }),
    todaySubs: (Array.isArray(f.today_subs) ? f.today_subs : []).map((s) => {
      const o = obj(s);
      return { name: str(o.name), done: o.done === true, how: str(o.how) };
    }),
    todayManual: num(f.today_manual),
    doneDays: strs(f.done_days),
    skippedDays: strs(f.skipped_days),
    brokenDays: strs(f.broken_days),
    manualDays: strs(f.manual_days),
    computedAt: str(f.computed_at),
  };
}

export function toWallet(r: RecordLike | null | undefined): Wallet {
  const f = r?.fields ?? {};
  return {
    earned: num(f.earned),
    spent: num(f.spent),
    points: num(f.points),
    taskPoints: num(f.task_points),
    taskExchanged: num(f.task_exchanged),
    outfitsReady: f.outfits_ready === true,
  };
}

/**
 * 家裡只有一位秘書時是那位的職位 id，否則空字串（名單還沒讀到、沒有或不只一位）：
 * 只有一位就不問跟誰約，按了直接開那位的聊天（HAB-22，DEV-157）。names 是職位 id → 秘書名字。
 */
export function soleAgent(names: Record<string, string> | undefined): string {
  const ids = Object.keys(names ?? {});
  return ids.length === 1 ? (ids[0] ?? "") : "";
}

/** 編號的數字（H-12 → 12），排序用。 */
export function seqOf(id: string): number {
  const n = Number(id.split("-")[1]);
  return Number.isFinite(n) ? n : Number.MAX_SAFE_INTEGER;
}

export function sortHabits(habits: Habit[]): Habit[] {
  return [...habits].sort((a, b) => seqOf(a.id) - seqOf(b.id));
}

/** vault 的 status 編號：習慣編號的小寫（H-3 → h-3）。 */
export const statusId = (habitId: string): string => habitId.toLowerCase();

/** 手動打卡的去重鍵（跟套件文件一致）。 */
export const checkinKey = (habit: string, date: string, sub = ""): string =>
  `manual:${habit}:${date}:${sub}`;

export interface Sub {
  name: string;
  source: string;
  min: number;
}

/** 「名稱」「名稱=來源」「名稱=nutrition:meals>=N」拆開；最少筆數不是整數回 null。 */
export function parseSub(s: string): Sub | null {
  const eq = s.indexOf("=");
  const name = (eq < 0 ? s : s.slice(0, eq)).trim();
  const rest = eq < 0 ? "" : s.slice(eq + 1);
  const ge = rest.indexOf(">=");
  const source = (ge < 0 ? rest : rest.slice(0, ge)).trim();
  let min = 1;
  if (ge >= 0 && rest.slice(ge + 2).trim() !== "") {
    const raw = rest.slice(ge + 2).trim();
    if (!/^-?\d+$/.test(raw)) return null;
    min = Number(raw);
  }
  return { name, source, min };
}

/** 子項目的自動來源怎麼念給人聽。 */
export function subLabel(s: string): string {
  const p = parseSub(s);
  if (!p) return s;
  if (p.source === "body:weight") return `${p.name}（有體重記錄就算）`;
  if (p.source === "nutrition:meals") return `${p.name}（一天記滿 ${p.min} 餐就算）`;
  return p.name;
}

export interface HabitInput {
  name: string;
  cadence: string;
  target: number;
  missDays: number;
  weekSkips: number;
  mode: string;
  grace: number;
  status: string;
  sources: string[];
  subtasks: string[];
  allowMiss: number;
  identity: string;
  cue: string;
  twoMin: string;
  bonusWeeks: number;
}

const isInt = (n: number): boolean => Number.isInteger(n);

/**
 * 頁面表單存檔前的檢查：只看表單上有的欄位（名稱、頻率、每週幾次、狀態），DEV-142。
 * 其他規則頁面不改，秘書寫壞了也不該擋主人改名字；完整檢查在套件程式（internal/rules）。
 */
export function validateForm(h: HabitInput): string | null {
  if (!h.name.trim()) return "習慣要有名字";
  if (h.cadence !== "daily" && h.cadence !== "weekly") return "頻率要選每天或每週";
  if (h.status !== "active" && h.status !== "ended") return "狀態要選進行中或已結束";
  if (h.cadence === "weekly" && (!isInt(h.target) || h.target < 1 || h.target > 7))
    return "每週幾次要在 1 到 7 之間";
  return null;
}

/** 整筆習慣的檢查，跟套件程式（internal/rules）同一套規則；回第一個錯誤（白話），沒問題回 null。 */
export function validateHabit(h: HabitInput): string | null {
  if (!h.name.trim()) return "習慣要有名字";
  if (h.cadence !== "daily" && h.cadence !== "weekly") return "頻率要選每天或每週";
  if (h.mode !== "cumulative" && h.mode !== "streak") return "模式要選累計或連續";
  if (h.status !== "active" && h.status !== "ended") return "狀態要選進行中或已結束";
  if (h.cadence === "weekly" && (!isInt(h.target) || h.target < 1 || h.target > 7))
    return "每週幾次要在 1 到 7 之間";
  if (!isInt(h.missDays) || h.missDays < 0 || h.missDays > 6)
    return "最多可以連續漏幾天要在 0 到 6 之間";
  if (!isInt(h.weekSkips) || h.weekSkips < 0 || h.weekSkips > 6)
    return "一週最多跳過幾天要在 0 到 6 之間";
  if (!isInt(h.grace) || h.grace < 0 || h.grace > 6) return "寬容週數要在 0 到 6 之間";
  if (!isInt(h.bonusWeeks) || h.bonusWeeks < 0 || h.bonusWeeks === 1 || h.bonusWeeks > 12)
    return "連續幾週加分要是 0 或 2 到 12（0＝不加分）";
  for (const s of h.sources) {
    const i = s.indexOf(":");
    const kind = i < 0 ? s : s.slice(0, i);
    const arg = i < 0 ? "" : s.slice(i + 1);
    if ((kind !== "fitness" && kind !== "reminder") || arg === "")
      return `自動打卡來源看不懂：${s}（可以寫 fitness:重訓、fitness:跑步（或划船、游泳、騎車）、fitness:有氧、fitness:*、reminder:R-編號）`;
  }
  const subs: Sub[] = [];
  for (const raw of h.subtasks) {
    const p = parseSub(raw);
    if (!p) return `子項目「${raw}」的最少筆數要是整數（>=N）`;
    subs.push(p);
  }
  const names = subs.map((s) => s.name);
  if (names.includes("") || new Set(names).size !== names.length)
    return "子項目的名稱不能是空的，也不能重複";
  for (const s of subs) {
    if (s.source && !(SUB_SOURCES as readonly string[]).includes(s.source))
      return `子項目「${s.name}」的自動來源看不懂：${s.source}（可以寫 body:weight、nutrition:meals，或不寫＝手動）`;
    if (s.min < 1) return `子項目「${s.name}」的最少筆數要 1 以上`;
  }
  if (!isInt(h.allowMiss)) return "最多可漏幾項要是整數";
  if (subs.length > 0 && (h.allowMiss < 0 || h.allowMiss >= subs.length))
    return `最多可漏幾項要在 0 到 ${subs.length - 1} 之間（至少要做 1 項）`;
  if (subs.length === 0 && h.allowMiss !== 0) return "沒有子項目，最多可漏幾項只能是 0";
  return null;
}

/**
 * 存檔用的欄位：只含頁面表單管的那幾欄（DEV-142 縮成常改的）；其他規則（連漏幾天、自動來源、子項目、
 * 連續加分週數…）跟秘書討論，頁面不寫，所以存檔不會動到它們。連續加分週數改了會回頭重算整段歷史，
 * 點數又只增不減，所以只在約定時跟秘書定，不放在頁面上隨手改。
 */
export function fieldsFromInput(h: HabitInput): Record<string, unknown> {
  return {
    name: h.name.trim(),
    cadence: h.cadence,
    status: h.status,
    cue: h.cue.trim(),
    two_min: h.twoMin.trim(),
    ...(h.cadence === "weekly" ? { target: h.target } : {}),
  };
}

/** 本週進度的文字，例：本週 2 / 3 次、本週 5 / 7 天。 */
export function progressText(cadence: string, st: HabitStatus | undefined): string {
  if (!st) return "還在算";
  return cadence === "daily"
    ? `本週 ${st.done} / ${st.need} 天`
    : `本週 ${st.done} / ${st.need} 次`;
}

/** 離下一點還差幾週的文字（詳情頁；跟卡片同一句，DEV-156 U-14）。 */
export function nextPointText(st: HabitStatus | undefined): string {
  if (!st) return "";
  return pointLine(st);
}

/** 剛按下去、程式還沒重算的那一下（打卡或撤銷；sub＝子項目的名字）。 */
export interface PendingMark {
  kind: "checkin" | "undo";
  sub?: string;
}

/**
 * 打卡／撤銷送出後、程式重算好之前，先把畫面上的進度改掉（DEV-156 U-20）：不讓主人盯著舊數字等。
 * 只改看得到的幾格（今天做了沒、本週幾次、子項目勾了沒、能不能撤銷）；點數、連續週數等程式重算（約 2 秒）。
 * 有子項目的習慣：勾到夠的項數（總數減可漏的）今天才算做了。
 */
export function withPendingMark(
  h: Pick<Habit, "subtasks" | "allowMiss">,
  st: HabitStatus,
  today: string,
  mark: PendingMark,
): HabitStatus {
  const on = mark.kind === "checkin";
  let todaySubs = st.todaySubs;
  let counted = on;
  if (h.subtasks.length > 0) {
    if (mark.sub) {
      const rest = st.todaySubs.filter((s) => s.name !== mark.sub);
      todaySubs = on ? [...rest, { name: mark.sub, done: true, how: "manual" }] : rest;
    }
    // 跟程式那邊同一條規則（internal/rules/habit.go 的子項目達標數）；這裡只是重算好之前先顯示，正本在 Go。
    const need = Math.max(h.subtasks.length - h.allowMiss, 1);
    counted = todaySubs.filter((s) => s.done).length >= need;
  }
  const was = st.days.includes(today);
  const set = (days: string[]) =>
    counted ? (days.includes(today) ? days : [...days, today]) : days.filter((d) => d !== today);
  return {
    ...st,
    todaySubs,
    days: set(st.days),
    doneDays: set(st.doneDays),
    done: Math.max(st.done + (counted === was ? 0 : counted ? 1 : -1), 0),
    todayManual: mark.sub ? st.todayManual : on ? st.todayManual + 1 : 0,
  };
}

/** 同一份進度底下的一串動作照順序套上去（連續勾兩個子項目時，前一個不會彈回沒勾）。 */
export function withPendingMarks(
  h: Pick<Habit, "subtasks" | "allowMiss">,
  st: HabitStatus,
  today: string,
  marks: readonly PendingMark[],
): HabitStatus {
  return marks.reduce((acc, mark) => withPendingMark(h, acc, today, mark), st);
}

/** 今天算做了嗎：本週做過的日子裡有今天（有子項目的習慣，只有做到足夠的項數那天才列進來）。 */
export function doneToday(st: HabitStatus | undefined, today: string): boolean {
  return !!st && st.days.includes(today);
}

/** 今天還沒打卡的進行中習慣數（今天頁的卡片用）。 */
export function notDoneCount(
  habits: Habit[],
  statuses: Record<string, HabitStatus>,
  today: string,
): number {
  return habits.filter(
    (h) => h.status === "active" && h.start <= today && !doneToday(statuses[statusId(h.id)], today),
  ).length;
}

/** 週結果的白話。 */
export function weekLabel(result: string): string {
  switch (result) {
    case "hit":
      return "達標";
    case "miss":
      return "沒達標";
    case "open":
      return "進行中";
    case "skip":
      return "不計（約定那週）";
    default:
      return result;
  }
}

/** 週一日期 YYYY-MM-DD 顯示成 M/D。 */
export function weekStartLabel(week: string): string {
  const m = /^\d{4}-(\d{2})-(\d{2})$/.exec(week);
  return m ? `${Number(m[1])}/${Number(m[2])}` : week;
}

/** 本地時區的 YYYY-MM-DD。 */
export function dateStr(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/** 頻率的白話，例：每週 3 次、每天（最多連漏 1 天）。 */
export function cadenceText(h: Pick<Habit, "cadence" | "target" | "missDays">): string {
  return h.cadence === "daily" ? `每天（最多連漏 ${h.missDays} 天）` : `每週 ${h.target} 次`;
}

/**
 * 存好的習慣變了（秘書打卡、即時更新重讀、重連全部重讀）時表單該顯示什麼（PKG-49 補）：
 * 主人還沒動過表單（cur 跟上次套用的 base 一樣）→ 換成新的；已經改了還沒存 → 留著主人改的，不洗掉。
 */
export function nextFormValue(cur: HabitInput, base: HabitInput, next: HabitInput): HabitInput {
  return JSON.stringify(cur) === JSON.stringify(base) ? next : cur;
}

/** 今天頁那張卡現在該顯示什麼：任何一份讀不到就是錯誤（不能一直轉圈，HAB-12），每一份都到才算好。 */
export function todayCardState(
  parts: readonly { data: unknown; error: string }[],
): "error" | "loading" | "ready" {
  if (parts.some((p) => p.error)) return "error";
  return parts.every((p) => p.data !== undefined) ? "ready" : "loading";
}

/** 今天該不該做這個習慣：進行中、已開始、今天還沒做、每週型本週還沒滿。 */
function dueToday(h: Habit, st: HabitStatus | undefined, today: string): boolean {
  if (h.status !== "active" || h.start > today || doneToday(st, today)) return false;
  return !(h.cadence === "weekly" && st && st.done >= st.need);
}

/** 每天型昨天也沒做：今天再不做就連兩次漏了。 */
function atRisk(h: Habit, st: HabitStatus | undefined, today: string, yesterday: string): boolean {
  return (
    dueToday(h, st, today) &&
    h.cadence === "daily" &&
    h.start <= yesterday &&
    !st?.doneDays.includes(yesterday)
  );
}

/**
 * 今天只有一件事（不連環催）：快連兩次漏的先；今天還沒做過任何習慣才挑一個一般的；
 * 其他都不顯示。回 null＝今天這樣就夠了。
 */
export function pickOneThing(
  habits: Habit[],
  statuses: Record<string, HabitStatus>,
  today: string,
  yesterday: string,
): Habit | null {
  const sorted = sortHabits(habits);
  const of = (h: Habit) => statuses[statusId(h.id)];
  const risk = sorted.find((h) => atRisk(h, of(h), today, yesterday));
  if (risk) return risk;
  if (sorted.some((h) => doneToday(of(h), today))) return null;
  const daily = sorted.find((h) => h.cadence === "daily" && dueToday(h, of(h), today));
  return daily ?? sorted.find((h) => dueToday(h, of(h), today)) ?? null;
}

/** 月曆一格的狀態。 */
export type DayState = "done" | "skipped" | "broken" | "open" | "none";

export interface CalendarCell {
  date: string;
  day: number;
  state: DayState;
  /** 點得動：開始後、今天以前（已結束的到結束日）。 */
  clickable: boolean;
  /** 點下去是撤銷（手動打的才能撤；自動來源的不能）。 */
  canUndo: boolean;
  /** 跳過的格子：這週第幾次跳過（1 起算；不是跳過是 0），第二次起顏色加深（HAB-17）。 */
  skipNo: number;
}

/** date 那週（週一到週日）到 date 為止跳過了幾天。 */
function skipOrdinal(date: string, skipped: string[], shown: Set<string>): number {
  const [y = 1970, m = 1, d = 1] = date.split("-").map(Number);
  const at = new Date(y, m - 1, d);
  const monday = dateStr(new Date(y, m - 1, d - ((at.getDay() + 6) % 7)));
  // 同一天又是做了或斷了的，畫面不畫成跳過，也不算次數。
  return skipped.filter((s) => s >= monday && s <= date && !shown.has(s)).length;
}

/** 一個月的格子（週一開頭，前面用 null 補空）。month 是 1–12。 */
export function monthCells(
  year: number,
  month: number,
  h: Pick<Habit, "start" | "status" | "ended">,
  st: Pick<HabitStatus, "doneDays" | "skippedDays" | "brokenDays" | "manualDays"> | undefined,
  today: string,
): (CalendarCell | null)[] {
  const first = new Date(year, month - 1, 1);
  const count = new Date(year, month, 0).getDate();
  const out: (CalendarCell | null)[] = Array((first.getDay() + 6) % 7).fill(null);
  for (let day = 1; day <= count; day++) {
    out.push(dayCell(new Date(year, month - 1, day), h, st, today));
  }
  return out;
}

// 本週（週一到週日）七格，跟月曆同一套顏色規則：習慣清單的卡片上直接看這週哪幾天做了（HAB-17）。
export function weekCells(
  h: Pick<Habit, "start" | "status" | "ended">,
  st: Pick<HabitStatus, "doneDays" | "skippedDays" | "brokenDays" | "manualDays"> | undefined,
  today: string,
): CalendarCell[] {
  const [y = 1970, m = 1, d = 1] = today.split("-").map(Number);
  const t = new Date(y, m - 1, d);
  const monday = d - ((t.getDay() + 6) % 7);
  return Array.from({ length: 7 }, (_, i) => dayCell(new Date(y, m - 1, monday + i), h, st, today));
}

function dayCell(
  at: Date,
  h: Pick<Habit, "start" | "status" | "ended">,
  st: Pick<HabitStatus, "doneDays" | "skippedDays" | "brokenDays" | "manualDays"> | undefined,
  today: string,
): CalendarCell {
  const last = h.status === "ended" && h.ended && h.ended < today ? h.ended : today;
  const date = dateStr(at);
  const inRange = date >= h.start && date <= last;
  const done = !!st?.doneDays.includes(date);
  let state: DayState = inRange ? "open" : "none";
  if (done) state = "done";
  else if (st?.brokenDays.includes(date)) state = "broken";
  else if (st?.skippedDays.includes(date)) state = "skipped";
  const manual = !!st?.manualDays.includes(date);
  return {
    date,
    day: at.getDate(),
    state,
    canUndo: done && manual,
    clickable: inRange && (!done || manual),
    skipNo:
      state === "skipped"
        ? skipOrdinal(
            date,
            st?.skippedDays ?? [],
            new Set([...(st?.doneDays ?? []), ...(st?.brokenDays ?? [])]),
          )
        : 0,
  };
}

/** 自動來源念給人聽，例：fitness:重訓 → 記了重訓。 */
export function sourceText(s: string): string {
  const i = s.indexOf(":");
  const kind = i < 0 ? s : s.slice(0, i);
  const arg = i < 0 ? "" : s.slice(i + 1);
  if (kind === "fitness") {
    if (arg === "*") return "記了任何一筆運動";
    if (arg === "有氧") return "記了有氧（跑步、划船、游泳、騎車都算）";
    return `記了${arg}`;
  }
  if (kind === "reminder") return `待辦 ${arg} 完成`;
  return s;
}

/** 這個習慣怎麼打卡：auto＝全部自動算、manual＝自己按、mixed＝子項目有的自動有的自己按。 */
export function howDone(h: Pick<Habit, "sources" | "subtasks">): "auto" | "manual" | "mixed" {
  if (h.subtasks.length > 0) {
    const auto = h.subtasks.filter((s) => parseSub(s)?.source).length;
    if (auto === h.subtasks.length) return "auto";
    return auto === 0 ? "manual" : "mixed";
  }
  return h.sources.length > 0 ? "auto" : "manual";
}

function subNames(h: Pick<Habit, "subtasks">): string[] {
  return h.subtasks.map((s) => parseSub(s)?.name ?? s);
}

/** 卡片上「怎樣算完成」那一行，例：每週 2 次・記了重訓就自動算。 */
export function doneLine(h: Habit): string {
  const freq = h.cadence === "daily" ? "每天" : `每週 ${h.target} 次`;
  const how = howDone(h);
  if (h.subtasks.length > 0) {
    const need = h.allowMiss > 0 ? `做到 ${h.subtasks.length - h.allowMiss} 項` : "都做到";
    const tail = how === "auto" ? "（自動算）" : how === "mixed" ? "（部分自動）" : "（自己按）";
    return `${freq}・${subNames(h).join("＋")}${need}才算${tail}`;
  }
  if (how === "auto") return `${freq}・${h.sources.map(sourceText).join("或")}就自動算`;
  return `${freq}・做完自己按打卡`;
}

/**
 * 卡片上「點數」那一行只寫下一步（DEV-156 U-14），例：再達標 1 週就拿到 1 點；
 * 完整規則（累計 2 週、之後每 4 週、連續加分）收在「怎麼算」裡（ruleSections 的「怎樣拿點」）。
 */
export function pointLine(st: Pick<HabitStatus, "nextNeedWeeks"> | null | undefined): string {
  if (!st) return "還在算";
  if (st.nextNeedWeeks <= 0) return "這週達標就拿到 1 點";
  return `再達標 ${st.nextNeedWeeks} 週就拿到 1 點`;
}

export interface RuleSection {
  id: "done" | "points" | "now";
  title: string;
  rows: [string, string][];
}

/** 「怎麼算」說明的三段：怎樣算完成、怎樣拿點、現在進度；每一欄分開寫（DEV-142）。 */
export function ruleSections(h: Habit, st: HabitStatus | null | undefined): RuleSection[] {
  const how = howDone(h);
  const done: [string, string][] = [
    ["頻率", h.cadence === "daily" ? "每天" : `每週 ${h.target} 次（哪幾天都可以）`],
  ];
  if (h.subtasks.length > 0) {
    done.push(["子項目", h.subtasks.map(subLabel).join("、")]);
    done.push([
      "一天怎樣算做到",
      h.allowMiss > 0
        ? `${h.subtasks.length} 項做到 ${h.subtasks.length - h.allowMiss} 項就算`
        : `${h.subtasks.length} 項都做到才算`,
    ]);
  }
  done.push([
    "怎樣打卡",
    how === "auto"
      ? h.subtasks.length > 0
        ? `自動算，不用按；沒記到可以自己補${h.sources.length > 0 ? `。另外${h.sources.map(sourceText).join("或")}，當天也直接算做到` : ""}`
        : `${h.sources.map(sourceText).join("或")}，當天就自動算；沒記到可以自己補`
      : how === "mixed"
        ? `有標自動的項目會自己算，其他自己按${h.sources.length > 0 ? `；${h.sources.map(sourceText).join("或")}，當天也直接算做到` : ""}`
        : h.subtasks.length > 0 && h.sources.length > 0
          ? `每項做完自己按；${h.sources.map(sourceText).join("或")}，當天也直接算做到`
          : "做完在卡片上按「今天打卡」，或跟秘書說一聲，秘書會幫你按",
  ]);
  done.push([
    "一週怎樣算達標",
    h.cadence === "daily"
      ? `一週最多休 ${h.weekSkips} 天，而且不能連休超過 ${h.missDays} 天`
      : `這週做滿 ${h.target} 次`,
  ]);
  done.push(["從哪天開始算", `${h.start}（約定那週沒做滿不算漏）`]);
  done.push(["補打或撤銷", "進詳情頁，在月曆上點那一天"]);

  const points: [string, string][] = [
    ["累計", "每達標一週就累計一週：累計 2 週得第 1 點，之後每 4 週再 +1"],
    [
      "連續加分",
      h.bonusWeeks > 0
        ? `連續 ${h.bonusWeeks} 週都達標再 +1（${h.bonusWeeks * 2}、${h.bonusWeeks * 3} 週…也一樣）；中間漏一週就從 0 重新數。幾週加一次是約定時跟秘書定的`
        : "這個習慣沒有連續加分（要的話跟秘書討論）",
    ],
    ["點數會不會少", "不會：連續斷了、改規則、結束習慣，已經拿到的點都留著"],
  ];
  if (h.mode === "streak")
    points.push(["連續模式", `連續漏超過 ${h.grace} 週，累計到一半的週數歸零`]);

  const now: [string, string][] = st
    ? [
        ["累計達標", `${st.achievedWeeks} 週`],
        ["連續達標", `${st.streakWeeks} 週`],
        ["離下一點", st.nextNeedWeeks <= 0 ? "這週達標就拿到" : `再 ${st.nextNeedWeeks} 週`],
        ...(h.bonusWeeks > 0
          ? ([["離連續加分", `再連續 ${st.nextBonusWeeks} 週`]] as [string, string][])
          : []),
        [
          "這個習慣賺到",
          st.bonusPoints > 0
            ? `${st.earnedPoints} 點（含連續加分 ${st.bonusPoints} 點）`
            : `${st.earnedPoints} 點`,
        ],
      ]
    : [["狀態", "還在算"]];

  return [
    { id: "done", title: "怎樣算完成", rows: done },
    { id: "points", title: "怎樣拿點", rows: points },
    { id: "now", title: "現在進度", rows: now },
  ];
}
