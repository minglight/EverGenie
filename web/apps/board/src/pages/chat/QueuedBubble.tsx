import { cn, Markdown } from "@evergenie/ui";
import { useState } from "react";
import { t } from "../../i18n/zh-TW";
import { timeLabel } from "../../lib/format";
import { splitAttachments } from "./attachments";
import type { QueuedMessage } from "./flow";

// 使用者的話：文字照原樣顯示；附件行（Composer 組的 /share 連結）畫成縮圖或下載 chip（REQ-82）。
export function UserText({
  text,
  dim,
  shareBase,
}: {
  text: string;
  dim?: boolean;
  /** 附件在哪台（那台的 origin；主伺服器不給）。 */
  shareBase?: string;
}) {
  const { text: body, attachments } = splitAttachments(text);
  return (
    <div
      className={cn(
        "flex max-w-full flex-col items-end gap-1 rounded-[20px_20px_6px_20px] bg-accent px-3.5 py-2.5 text-[15px] leading-normal break-words text-accent-fg",
        dim && "opacity-70",
      )}
    >
      {body ? <div className="max-w-full whitespace-pre-wrap">{body}</div> : null}
      {attachments.length > 0 ? (
        <div data-testid="user-attachments" className="flex max-w-full flex-col items-end">
          <Markdown className="text-fg" shareBase={shareBase}>
            {attachments.join("\n\n")}
          </Markdown>
        </div>
      ) : null}
    </div>
  );
}

// shrink-0＋不換行：原因很長時「再說一次」「重送」不會被擠成兩行（WEB-119）。
const ACTION =
  "min-h-11 shrink-0 whitespace-nowrap px-2 font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus";

/**
 * 還沒進對話記錄的那句（淡色泡泡，WEB-60、WEB-67）：
 * - failed＝沒送到：紅字「這句沒送到：<原因>」＋「重送」；
 * - interrupted＝她收到了、這一輪沒做完（引擎回報的）：紅字「她沒回完這一句：<白話原因>」，不給重送（她可能做了一半，
 *   避免做兩次），只給「再說一次」（放回輸入框）；帳號登入、帳單這種要主人去處理的，再說一次也沒用，改給「看怎麼處理」
 *   （到訊息頁看那則提醒）；平台原話收在「詳細」（DEV-156 U-08）；
 * - 其他：傳送中／已送出；還沒收到時寫伺服器給的原因，說不出原因就只寫「已送出」、不標紅（WEB-71、S5）。
 */
export function QueuedBubble({
  item,
  agentName,
  onResend,
  onSayAgain,
  shareBase,
}: {
  item: QueuedMessage;
  /** 這串秘書的名字：小字寫「{name} 正在忙」這類（秘書不一定是女生，不寫「她」）。 */
  agentName: string;
  /** 附件在哪台（那台的 origin；主伺服器不給）。 */
  shareBase?: string;
  onResend: () => void;
  onSayAgain: () => void;
}) {
  return (
    <div
      className="hg-rise flex max-w-[78%] flex-col items-end gap-1 self-end"
      data-queued={item.status}
      data-unsaved={item.fromServer ? undefined : ""}
    >
      {item.inLog ? null : <UserText text={item.text} dim shareBase={shareBase} />}
      {item.status === "failed" ? (
        <div className="flex items-center gap-2 text-xs text-danger" role="alert">
          <span>{t("chat.sendFailed", { reason: item.error ?? "" })}</span>
          <button type="button" onClick={onResend} className={ACTION}>
            {t("chat.resend")}
          </button>
        </div>
      ) : item.status === "interrupted" ? (
        <Interrupted item={item} name={agentName} onSayAgain={onSayAgain} />
      ) : (
        <QueuedHint item={item} name={agentName} />
      )}
    </div>
  );
}

// 只有主人親手能解的錯（伺服器給的種類）：再說一次也一樣，給「看怎麼處理」。
const OWNER_FIX = new Set(["auth", "billing"]);

function Interrupted({
  item,
  name,
  onSayAgain,
}: {
  item: QueuedMessage;
  name: string;
  onSayAgain: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex max-w-full flex-col items-end">
      <div
        className="flex flex-wrap items-center justify-end gap-x-2 text-xs text-danger"
        role="status"
      >
        <span>
          {item.error
            ? t("chat.interruptedTurn", { name, reason: item.error })
            : t("chat.interruptedTurnNoReason", { name })}
        </span>
        {item.detail ? (
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
            className={cn(ACTION, "font-normal text-muted underline")}
          >
            {t("chat.interruptedDetail")}
          </button>
        ) : null}
        {OWNER_FIX.has(item.kind ?? "") ? (
          // 一般連結：外殼會改走前端路由（shell/internalLinks.ts，FEN-29），不整頁重載。
          <a href="/messages" className={cn(ACTION, "inline-flex items-center")}>
            {t("chat.interruptedFix")}
          </a>
        ) : (
          <button type="button" onClick={onSayAgain} className={ACTION}>
            {t("chat.sayAgain")}
          </button>
        )}
      </div>
      {open && item.detail ? (
        <p
          data-testid="interrupted-detail"
          className="max-w-full rounded-lg bg-surface-2 px-2.5 py-1.5 text-xs break-words whitespace-pre-wrap text-muted"
        >
          {t("chat.interruptedDetail.said", { text: item.detail })}
        </p>
      ) : null}
    </div>
  );
}

// 淡色泡泡下面的小字（WEB-71）：傳送中 → 已送出 → 3 秒內她收到了（泡泡換成正式的），或伺服器給的原因；
// 顏色（WEB-70）：等你回答上面的問題＝橘（等你處理）；中途補話＝藍（她在處理）；叫醒、收前一句、重試中＝灰。
// 不認得的原因（含舊版那台送來的 late）照「已送出」灰字，不猜（S5）。
function QueuedHint({ item, name }: { item: QueuedMessage; name: string }) {
  if (item.status === "queued")
    return <span className="text-xs text-muted">{t("chat.sending")}</span>;
  switch (item.wait) {
    case "answer":
      return <span className="text-xs text-attention-fg">{t("chat.wait.answer", { name })}</span>;
    case "waking":
      return <span className="text-xs text-muted">{t("chat.wait.waking", { name })}</span>;
    case "busy":
      return <span className="text-xs text-muted">{t("chat.wait.busy", { name })}</span>;
    case "retrying":
      return <span className="text-xs text-muted">{t("chat.wait.retrying", { name })}</span>;
    case "interject":
      return <span className="text-xs text-busy">{t("chat.sent.interject", { name })}</span>;
    case "limited": {
      // 額度用完：灰字＋恢復時間，不是失敗、不是要你做什麼（REQ-378、WEB-73）。
      const at = timeLabel(item.waitUntil, new Date());
      return (
        <span className="text-xs text-muted">
          {at ? t("chat.wait.limited", { at }) : t("chat.wait.limitedNoTime")}
        </span>
      );
    }
  }
  // 舊的伺服器沒有 wait：中途補話照原本的旗標
  if (item.interject)
    return <span className="text-xs text-busy">{t("chat.sent.interject", { name })}</span>;
  return <span className="text-xs text-muted">{t("chat.sent")}</span>;
}
