import type { AnswerVia, Notification } from "@evergenie/sdk-web";
import { cn } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Check, ChevronRight } from "lucide-react";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import {
  answerNotification,
  reportFailure,
  type SettleContext,
  useFailures,
  useSettled,
} from "../data/answers";
import { claimFollowUp } from "../data/claim";
import { homeSearch, PRIMARY, useSite, useSiteApi } from "../data/sites";
import { itemKey } from "../data/waiting";
import { t } from "../i18n/zh-TW";
import { noticeNeedsReply } from "../lib/statusTone";
import { ActionButton } from "./ActionButton";
import { siteName } from "./SiteTag";

/** 桌機右欄用鍵盤操作：A＝主要動作、R＝第二個（退回或第二個選項）。 */
export interface PaneActions {
  primary: () => void;
  secondary: () => void;
}

/** 通知自己的選項；沒有就用「好」「不要」（補充契約 S5 第 1 點）。只收打字回答的（`typed`，D-57）沒有按鈕。 */
export function optionsOf(n: Notification): string[] {
  if (n.typed) return [];
  const own = (n.options ?? []).filter((o) => o.trim() !== "");
  return own.length > 0 ? own : [t("answer.yes"), t("answer.no")];
}

/** 這則還等著回答嗎（要你同意、還沒答）。 */
/** 還要你回答（跟訊息列的橘點同一個判斷，lib/statusTone 的 noticeNeedsReply，WEB-70）。 */
export function isPending(n: Notification): boolean {
  return noticeNeedsReply(n);
}

/**
 * 畫面上要不要放回答的按鈕：等你回答的，加上到期待辦的一鍵回覆（做了／晚點／今天不做，RMD-5）。
 * 到期待辦不算「等你回答」（isPending）：沒回不亮橘點、不進要你同意，不念。
 */
export function canAnswer(n: Notification): boolean {
  if (isPending(n)) return true;
  const open = n.state === "unread" || n.state === "read";
  return n.category === "due" && open && (n.options ?? []).some((o) => o.trim() !== "");
}

interface AnswerOptions extends SettleContext {
  onAnswered?: (text: string) => void;
}

// 這則在哪一台（site，預設主伺服器）就回答到哪一台；那台現在不是 ok 就不送，說一句「連不上，沒有送出」（不退回主伺服器，FEN-28）。
function useAnswer(
  n: Notification | undefined,
  { item, index, site = PRIMARY, settleAs, onAnswered }: AnswerOptions,
) {
  const client = useQueryClient();
  const key = settleAs?.key ?? itemKey(site, n?.id ?? "");
  const settled = useSettled().get(key);
  const failure = useFailures().get(key);
  const siteApi = useSiteApi(site.key);
  const found = useSite(site.key);
  const navigate = useNavigate();
  const send = useCallback(
    async (text: string, via: AnswerVia): Promise<boolean> => {
      if (!n) return false;
      if (!siteApi) {
        reportFailure(key, t("sitePart.unavailable", { site: found ? siteName(found) : site.key }));
        return false;
      }
      const ok = await answerNotification(client, n, text, via, { item, index, site, settleAs });
      if (!ok) return false;
      onAnswered?.(text);
      // 認領通知按「換一個」：直接開設定→職位與秘書那張卡（SEC-302 S9、REQ-346）。
      // 設定頁只看主伺服器（不合併），別台的認領不跳過去。
      const next = site.key === "" ? claimFollowUp(n, text) : undefined;
      if (next) void navigate(next);
      return true;
    },
    [client, n, key, item, index, site, settleAs, siteApi, found, onAnswered, navigate],
  );
  const serverAnswer = n?.state === "answered" ? (n.answer?.text ?? "") : undefined;
  return { send, answered: settled?.short ?? serverAnswer, failure, key };
}

// 打到一半的回答，用「已回」標記的鍵記（在任務單上就地回答是單的鍵，催的時候換了一則也一樣）。
// 送出時先收成「已回」、輸入框拆掉；沒送出去（網路斷、那則剛好被收掉）再長回來時字還在，不用重打（DEV-156 review）。
const drafts = new Map<string, string>();

export interface AnswerButtonsProps extends AnswerOptions {
  notification: Notification;
  size: "card" | "page" | "pane";
  /** 桌機右欄：按鈕旁的鍵帽，並把 A／R 交給外面的鍵盤處理。 */
  keycaps?: boolean;
  bind?: (actions: PaneActions | null) => void;
  className?: string;
}

// 選項按鈕：第一個實心（主要），其他次要。按下立刻送出、不給復原（它會馬上通知秘書）。
export function AnswerButtons({
  notification,
  size,
  keycaps,
  bind,
  className,
  ...ctx
}: AnswerButtonsProps) {
  const { send, answered, failure } = useAnswer(notification, ctx);
  const options = optionsOf(notification);

  useEffect(() => {
    if (!bind) return;
    bind({
      primary: () => {
        const first = options[0];
        if (first && answered === undefined) void send(first, "button");
      },
      secondary: () => {
        const second = options[1];
        if (second && answered === undefined) void send(second, "button");
      },
    });
    return () => bind(null);
  }, [bind, options, send, answered]);

  if (answered !== undefined) return null;
  // 只收打字回答（要你回答的任務問句，WEB-112）：不畫按鈕，只留打字回答與「去聊」。
  if (options.length === 0) {
    return failure ? (
      <p role="alert" className={cn("text-[13px] text-danger", className)}>
        {failure}
      </p>
    ) : null;
  }
  const compact = options.length <= 2;
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <fieldset
        className={cn(
          "m-0 flex min-w-0 gap-2 border-0 p-0",
          compact && size !== "pane" ? "" : "flex-wrap",
        )}
      >
        <legend className="sr-only">{t("answer.options")}</legend>
        {options.map((text, i) => (
          <ActionButton
            key={text}
            size={size}
            tone={i === 0 ? "primary" : size === "pane" || !compact ? "outline" : "secondary"}
            keycap={keycaps ? (i === 0 ? "A" : i === 1 ? "R" : undefined) : undefined}
            className={compact && size !== "pane" ? "flex-1" : undefined}
            onClick={() => void send(text, "button")}
          >
            {text}
          </ActionButton>
        ))}
      </fieldset>
      {failure ? (
        <p role="alert" className="text-[13px] text-danger">
          {failure}
        </p>
      ) : null}
    </div>
  );
}

export interface AnswerTypedProps extends AnswerOptions {
  notification: Notification;
  className?: string;
}

// 「或打字回答」：一行輸入框，Enter 送出（via: chat）。
export function AnswerTyped({ notification, className, ...ctx }: AnswerTypedProps) {
  const { send, answered, key } = useAnswer(notification, ctx);
  const [text, setTextState] = useState(() => drafts.get(key) ?? "");
  if (answered !== undefined) return null;
  const setText = (value: string) => {
    setTextState(value);
    if (value) drafts.set(key, value);
    else drafts.delete(key);
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const value = text.trim();
    if (value === "") return;
    void send(value, "chat").then((ok) => {
      if (ok) drafts.delete(key);
    });
  };
  return (
    <form onSubmit={submit} className={cn("flex flex-col gap-1.5", className)}>
      <label htmlFor={`typed-${notification.id}`} className="text-[13px] font-semibold">
        {notification.typed ? t("answer.typed.only") : t("answer.typed")}
      </label>
      <div className="flex gap-2">
        <input
          id={`typed-${notification.id}`}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={t("answer.typed.placeholder")}
          autoComplete="off"
          enterKeyHint="send"
          className="h-11 min-w-0 flex-1 rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus"
        />
        <ActionButton type="submit" size="page" tone="secondary" disabled={text.trim() === ""}>
          {t("ui.send")}
        </ActionButton>
      </div>
    </form>
  );
}

// 已經回答了：綠色一列（UXC-9），下面一行小字說怎麼回的、秘書收到了沒。
export function AnsweredRow({
  notification,
  answer,
}: {
  notification: Notification;
  answer: string;
}) {
  const a = notification.answer;
  const meta =
    a?.via === "chat"
      ? t("message.answered.viaChat")
      : a?.via === "button"
        ? t("message.answered.via")
        : "";
  // 系統提醒、套件、綁單的通知回答了就算數，沒有秘書要讀（delivers_answer = false，NTF-59）：不說「秘書還沒讀到」。
  // 舊版的台沒給這欄（undefined）照以前的說法。
  const delivery =
    notification.state !== "answered" || notification.delivers_answer === false
      ? ""
      : a?.delivered_at && !a.delivered_at.startsWith("0001")
        ? t("message.answered.delivered")
        : t("message.answered.waiting");
  return (
    <div
      role="status"
      className="hg-rise flex flex-col gap-0.5 rounded-[14px] bg-ok-soft px-4 py-3 text-ok"
    >
      <span className="flex items-center gap-2 text-[15px] font-semibold">
        <Check className="size-[18px] shrink-0" strokeWidth={2} aria-hidden="true" />
        {t("message.answered", { answer })}
      </span>
      {meta || delivery ? (
        <span className="pl-[26px] text-xs">{[meta, delivery].filter(Boolean).join(" · ")}</span>
      ) : null}
    </div>
  );
}

/** 今天頁手機卡片用：選項按鈕＋一個「打字回答」小連結（到 /messages/$id）。 */
export function CardAnswer({
  notification,
  ...ctx
}: { notification: Notification } & AnswerOptions) {
  return (
    <div className="flex flex-col gap-1">
      <AnswerButtons notification={notification} size="card" {...ctx} />
      <Link
        to="/messages/$id"
        params={{ id: notification.id ?? "" }}
        search={homeSearch(ctx.site ?? PRIMARY)}
        className="flex h-11 w-fit items-center gap-0.5 text-[13px] font-medium text-accent focus-visible:outline-2 focus-visible:outline-focus"
      >
        {t("answer.typed.link")}
        <ChevronRight className="size-4" aria-hidden="true" />
      </Link>
    </div>
  );
}

/** 詳細頁：選項按鈕＋「或打字回答」；已經回答了就顯示綠色一列。 */
export function AnswerPanel({
  notification,
  ...ctx
}: { notification: Notification } & AnswerOptions) {
  const { answered } = useAnswer(notification, ctx);
  if (answered !== undefined) return <AnsweredRow notification={notification} answer={answered} />;
  return (
    <div className="flex flex-col gap-4">
      <AnswerButtons notification={notification} size="page" {...ctx} />
      {/* 到期待辦只收三顆按鈕，打字的話不會生效（RMD-5） */}
      {notification.category === "due" ? null : (
        <AnswerTyped notification={notification} {...ctx} />
      )}
    </div>
  );
}

export { useAnswer };
