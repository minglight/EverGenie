import type { ThreadSummary } from "@evergenie/sdk-web";
import { t } from "../../i18n/zh-TW";
import { timeLabel } from "../../lib/format";
import { type StatusTone, threadTone, toneText } from "../../lib/statusTone";

export type { StatusTone } from "../../lib/statusTone";

export interface StatusView {
  /** 給人看的白話狀態；idle 沒有（清單不顯示）。 */
  text: string;
  /** 顏色照 lib/statusTone 的對照表（WEB-70）：橘只給等你處理。 */
  tone: StatusTone;
}

/**
 * 要不要畫三點（WEB-160）。工程師模式：最後一則是你的話才畫（她說的話、工具膠囊就是進度）。
 * 預設模式：她忙的整段都畫，中途說過話也一樣（工具藏起來了，不然畫面像沒動靜）；有卡片在等你回答就不畫。
 */
export function showTypingDots(s: {
  working: boolean;
  toolDetails: boolean;
  lastIsUser: boolean;
  awaitingAnswer: boolean;
}): boolean {
  if (!s.working) return false;
  return s.toolDetails ? s.lastIsUser : !s.awaitingAnswer;
}

/**
 * 等你處理的串寫哪一種（DEV-156 U-10，名詞表「等你處理」）：只在等權限或通知點頭＝「要你同意」；
 * 有問題、待辦要你回（或伺服器沒說原因）＝「要你回答」。
 */
export function waitingWord(waiting: readonly string[] | undefined): string {
  const real = (waiting ?? []).filter((w) => w !== "work_queue");
  if (real.length > 0 && real.every((w) => w === "permission" || w === "approval"))
    return t("chat.row.waitingApprove");
  return t("chat.row.waiting");
}

/** 討論串現在最該讓人知道的一件事（標題欄與清單共用）；狀態由伺服器算，這裡只翻成白話，顏色查對照表。 */
export function statusView(
  th: Pick<ThreadSummary, "status" | "activity" | "queued" | "limited_until"> &
    Partial<Pick<ThreadSummary, "kind" | "task" | "status_since">> & {
      waiting?: readonly string[];
    },
  now: Date = new Date(),
  /** 工程師模式才寫正在做什麼（讀檔、跑指令…）；預設模式一律寫「處理中」（WEB-160）。 */
  toolDetails = false,
): StatusView {
  const tone = threadTone(th);
  switch (th.status) {
    case "waiting_user":
      // 舊版那台：排隊等名額（沒有別的在等你）也送 waiting_user——說白話，不是「等你處理」，也不是橘色（P7-5、D-33、WEB-70）
      if (tone !== "reply") return { text: t("chat.row.workQueue"), tone };
      return { text: waitingWord(th.waiting), tone };
    case "working":
      return { text: (toolDetails && th.activity) || t("chat.row.working"), tone };
    case "queued":
      // 工作 session 排隊等名額（S5 D6：伺服器給 queued＋work_queue；灰色，不是等你處理）
      if (th.waiting?.includes("work_queue")) return { text: t("chat.row.workQueue"), tone };
      // 你的話排了 QUEUED_STUCK_MS 還沒被收：平常幾秒就收，這麼久就是她那一側沒回應（卡住、開不起來、貼不進去）。
      // 只看核心記下的狀態（排隊中從什麼時候開始），不去問引擎（WEB-175）。
      if (queuedTooLong(th.status_since, now))
        return { text: t("chat.row.queuedStuck", { n: th.queued }), tone };
      return { text: t("chat.row.queued", { n: th.queued }), tone };
    case "done":
      return { text: t("chat.row.done"), tone };
    case "starting":
      return { text: t("chat.row.starting"), tone };
    case "stopped":
      // 單還沒結、先收起來的任務對話：留在清單，說話就接回原本那段（DEV-147）
      if (th.kind === "work" && th.task) return { text: t("chat.row.parked"), tone };
      return { text: t("chat.row.stopped"), tone };
    case "limited":
      // 額度用完：灰色、附恢復時間；不是卡住、不是等你處理（REQ-378、WEB-73）。
      return { text: limitedText(th.limited_until, now), tone };
    default:
      // 不認得的狀態（含舊版那台送來的 stuck）：不寫字、灰色，不猜（S5）
      return { text: "", tone };
  }
}

/** 你的話排隊超過這麼久還沒被收，就寫「她那邊沒回應」（核心貼不進去時每 12 秒重試一次，30 秒是兩次以上）。 */
export const QUEUED_STUCK_MS = 30_000;

function queuedTooLong(since: string | undefined, now: Date): boolean {
  const at = Date.parse(since || "");
  return Number.isFinite(at) && at > 0 && now.getTime() - at >= QUEUED_STUCK_MS;
}

/** 「額度用完，18:00 恢復」；不是今天就帶「明天」或日期；說不出恢復時間就只寫額度用完。 */
export function limitedText(until: string | undefined, now: Date = new Date()): string {
  const at = timeLabel(until, now);
  return at ? t("chat.row.limited", { at }) : t("chat.row.limitedNoTime");
}

/** 狀態字的顏色（清單、標題欄共用；就是 lib/statusTone 的 toneText）。 */
export const toneClass: Record<StatusTone, string> = toneText;

function activityAt(th: ThreadSummary): number {
  return Date.parse(th.last_assistant_at || th.status_since || "") || 0;
}

// 第一組：等你處理；第二組：處理中、排隊、啟動；其餘最後。
function rank(th: ThreadSummary): number {
  switch (th.status) {
    case "waiting_user":
      return 0;
    case "working":
    case "queued":
    case "starting":
      return 1;
    default:
      return 2;
  }
}

/** 排序：等你處理 → 處理中／排隊 → 其他；同一組內最近有動靜的在前。 */
export function sortThreads(threads: readonly ThreadSummary[]): ThreadSummary[] {
  return [...threads].sort((a, b) => rank(a) - rank(b) || activityAt(b) - activityAt(a));
}

// ── 工作 session（P7-5、D-33）：一張任務單一個，列在她的秘書底下 ─────────────────────────────

/** 引擎的 prompt cache 約一小時：上一輪結束起算 60 分鐘（D-33、D-34；倒數只做在前端）。 */
export const HOT_CACHE_MINUTES = 60;
/** 剩這麼多分鐘以內算「快過期」：倒數換成提醒的顏色與說法，趕在過期前接回比較省。 */
export const CACHE_SOON_MINUTES = 10;
/** 倒數的更新間隔：以分鐘顯示，3 秒更新一次，剩餘分鐘變了最多晚 3 秒（REQ-389；只重算本機時間，不打 API）。 */
export const CACHE_TICK_MS = 3_000;

export function isWork(th: Pick<ThreadSummary, "kind" | "task">): boolean {
  return th.kind === "work" && !!th.task;
}

/** 把工作 session 收進她的秘書底下：回傳排好的頂層串，與「主要討論串的名稱 → 她的工作 session」。
 * 頂層的順序把底下工作 session 的急迫度算進去（WEB-121）。
 * 工作 session 收在她的主要討論串（名稱就是秘書 id 的那串）底下。
 * 這位秘書的主要討論串不在清單裡（被篩掉、沒開）時，她的工作 session 留在頂層，不會不見。 */
export function groupWork(threads: readonly ThreadSummary[]): {
  top: ThreadSummary[];
  nested: Map<string, ThreadSummary[]>;
} {
  const parent = new Map<string, string>();
  for (const th of threads) {
    if (th.agent && th.name === th.agent && !parent.has(th.agent)) {
      parent.set(th.agent, th.name);
    }
  }
  const nested = new Map<string, ThreadSummary[]>();
  const top: ThreadSummary[] = [];
  for (const th of threads) {
    const key = isWork(th) && th.agent ? parent.get(th.agent) : undefined;
    if (key !== undefined) {
      const list = nested.get(key) ?? [];
      list.push(th);
      nested.set(key, list);
    } else {
      top.push(th);
    }
  }
  for (const list of nested.values()) {
    list.sort((a, b) => (a.task ?? "").localeCompare(b.task ?? "", "en", { numeric: true }));
  }
  // WEB-121：她底下有工作 session 等你處理或在處理時，她那一列跟著排上去（不然有空的她排在最後，
  // 等你處理的那張單埋在清單底部）。用最急的那一個的組別；同組照原本的順序（sort 是穩定的）。
  const urgency = (th: ThreadSummary) =>
    Math.min(rank(th), ...(nested.get(th.name ?? "") ?? []).map(rank));
  top.sort((a, b) => urgency(a) - urgency(b));
  return { top, nested };
}

/** 工作 session 的標題去掉前面的單號（單號另外顯示）。 */
export function workTitle(th: Pick<ThreadSummary, "title" | "task">): string {
  const title = (th.title ?? "").trim();
  const task = th.task ?? "";
  return task && title.startsWith(task) ? title.slice(task.length).trim() : title;
}

/** hot cache 還剩幾分鐘（上一輪結束起 60 分鐘）；沒跑過、正在跑、已過期都回 null。 */
export function cacheLeftMinutes(
  th: Pick<ThreadSummary, "status" | "last_turn_end_at">,
  now: Date,
): number | null {
  if (th.status === "working" || !th.last_turn_end_at) return null;
  const end = Date.parse(th.last_turn_end_at);
  if (!Number.isFinite(end)) return null;
  const left = Math.ceil((end + HOT_CACHE_MINUTES * 60_000 - now.getTime()) / 60_000);
  return left > 0 ? Math.min(left, HOT_CACHE_MINUTES) : null;
}

/** 倒數的白話與是不是快過期（清單列與討論串標題欄共用同一套）。 */
export function cacheView(minutes: number): { text: string; soon: boolean } {
  const soon = minutes <= CACHE_SOON_MINUTES;
  return {
    text: soon ? t("chat.work.cacheSoon", { n: minutes }) : t("chat.work.cache", { n: minutes }),
    soon,
  };
}
