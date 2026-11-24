import type { Notification, ThreadSummary } from "@evergenie/sdk-web";

// ── 顏色一套規則（WEB-70、REQ-382、REQ-383；DEV-50 S6）────────────────────────────────────────
// 狀態只有五種顏色，整個看板（清單、標題欄、訊息頁、徽章、秘書卡）都從這裡拿：
//   橘（reply）  ：等你處理——討論串 waiting_user、session 有 pending（等你）、訊息還要你回答。**只有這個用橘色。**
//   藍（busy）   ：處理中。
//   紅（trouble）：出錯——這句沒送到、她沒回完這一句（引擎回報的事實；「卡住」「掉包」S5 拿掉，核心不再猜）。
//   綠（fresh）  ：完成、有新回覆（未讀）。
//   灰（idle）   ：待命、停了、啟動中、排隊（含排隊等名額）、額度用完（灰色配文字說明）；不認得的狀態（含舊版那台送來的
//                  stuck／late）也是灰。
// 顏色本身是 tokens.css 的 token（--attention／--busy／--danger／--ok／--muted），這裡只決定「哪個狀態用哪個 token」。
// 這幾個檔案還有自己的「等你處理」膠囊（任務單 waiting_user、Goal 草稿、待辦 asking、權限申請 pending、DEV 驗證中），
// 也只在「等主人」時用橘色；它們不是聊天狀態，照舊各自的表。

export type StatusTone = "reply" | "busy" | "trouble" | "fresh" | "idle";

/** 對照表：狀態名（討論串、session、秘書在場、工作對話、泡泡）→ 顏色。沒列到的一律灰色。 */
export const STATUS_TONES = {
  // 橘：等你處理
  waiting_user: "reply", // 討論串（ThreadStatus）；只有「排隊等名額」不算，見 threadTone
  waiting: "reply", // session 狀態（SessionState，有 pending）、秘書在場（Presence）、工作對話
  // 藍：處理中
  working: "busy", // 討論串、秘書在場、工作對話
  busy: "busy", // session 狀態
  // 紅：出錯（引擎回報的事實）
  not_delivered: "trouble", // 這句沒送到（泡泡 failed）
  interrupted: "trouble", // 她沒回完這一句（泡泡 interrupted，S5）
  // 綠：完成、有新回覆
  done: "fresh",
  unread: "fresh",
  // 灰：待命、停了、額度用完
  idle: "idle",
  stopped: "idle",
  starting: "idle",
  queued: "idle",
  limited: "idle", // 額度用完（S4，REQ-378）
  none: "idle",
  unknown: "idle",
} as const satisfies Record<string, StatusTone>;

/** 任一種狀態名 → 顏色；沒列在表上的是灰色（新狀態不會意外變橘）。 */
export function statusTone(state: string | undefined | null): StatusTone {
  if (!state) return "idle";
  return (STATUS_TONES as Record<string, StatusTone>)[state] ?? "idle";
}

/** 判斷顏色只需要狀態與「在等什麼」。 */
type ThreadLike = Pick<ThreadSummary, "status"> & { waiting?: readonly string[] };

/** 這串真的在等你處理：waiting_user，但不是「只有排隊等名額」（那是在等名額，不是在等你，WEB-35）。 */
export function needsReply(th: ThreadLike | undefined): boolean {
  if (!th || th.status !== "waiting_user") return false;
  return !(th.waiting?.length === 1 && th.waiting[0] === "work_queue");
}

/** 討論串的顏色：排隊等名額是灰色，其他照對照表。 */
export function threadTone(th: ThreadLike): StatusTone {
  if (th.status === "waiting_user" && !needsReply(th)) return "idle";
  return statusTone(th.status);
}

/** 這則訊息還要你回答：「要你同意」、還沒回答（未讀或已讀）、沒有被自動收掉（S2 的 via auto）。 */
export function noticeNeedsReply(
  n: Pick<Notification, "category" | "state" | "answer"> | undefined,
): boolean {
  if (!n || n.category !== "approve") return false;
  if (n.state !== "unread" && n.state !== "read") return false;
  return n.answer?.via !== "auto";
}

/** 訊息列前面的點：要你回答＝橘點；其他未讀＝綠點（有新的）；看過、回答過、收掉了＝沒有點。 */
export function noticeTone(
  n: Pick<Notification, "category" | "state" | "answer">,
): "reply" | "fresh" | null {
  if (noticeNeedsReply(n)) return "reply";
  return n.state === "unread" ? "fresh" : null;
}

/** 字的顏色。 */
export const toneText: Record<StatusTone, string> = {
  reply: "text-attention-fg",
  busy: "text-busy",
  trouble: "text-danger",
  fresh: "text-ok",
  idle: "text-muted",
};

/** 小圓點的顏色。 */
export const toneDot: Record<StatusTone, string> = {
  reply: "bg-attention",
  busy: "bg-busy",
  trouble: "bg-danger",
  fresh: "bg-ok",
  idle: "bg-border-strong",
};

/** 膠囊（Pill）的 tone。 */
export const tonePill = {
  reply: "attention",
  busy: "busy",
  trouble: "danger",
  fresh: "ok",
  idle: "muted",
} as const satisfies Record<StatusTone, string>;

/** 數字徽章（未讀數、等你幾件）：底色＋字色。 */
export const toneBadge: Record<StatusTone, string> = {
  reply: "bg-attention text-on-attention",
  busy: "bg-busy text-on-busy",
  trouble: "bg-danger text-on-danger",
  fresh: "bg-ok text-on-ok",
  idle: "bg-surface-2 text-muted",
};

/** 選單徽章：數字是「等你處理」的項目用橘色（今天、聊天），其他照呈現元件自己的中性色。 */
export const REPLY_BADGES: ReadonlySet<string> = new Set(["core.overview", "core.chat"]);
