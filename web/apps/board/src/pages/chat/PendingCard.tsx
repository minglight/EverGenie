import {
  type Api,
  ApiError,
  type Decision,
  type Pending,
  api as primaryApi,
} from "@evergenie/sdk-web";
import { cn, Spinner } from "@evergenie/ui";
import { Check } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { t } from "../../i18n/zh-TW";

/** 回答之後留在這次畫面裡的小膠囊文字（不寫回伺服器）。 */
export type Answered = { pendingId: string; note: string } | { pendingId: string; note: null };

export interface PendingCardProps {
  /** 這一串那台的 api（看板連多台，S3-6）；不給＝主伺服器。 */
  api?: Api;
  session: string;
  pending: Pending;
  agentName: string;
  /** 送出中（pending）或結束（null）：讓外面在 session.watch 說 pending 沒了的時候，這張卡先留著。 */
  onHold: (pending: Pending | null) => void;
  /** 送出成功：note 是要加進訊息流的膠囊；null 表示已經在別處回答了（不加）。 */
  onAnswered: (answered: Answered) => void;
}

const ELSEWHERE_MS = 2000;

const actionButton =
  "flex h-11 items-center justify-center gap-1.5 rounded-xl px-4 text-[15px] font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-60";

// 等你回答的卡片（排在訊息流最後、跟對話一起滑，WEB-157；剛出現時訊息區會捲到卡片開頭）：
// 權限＝摘要＋允許／拒絕（拒絕就地展開理由）；問題＝每題的選項按鈕＋「其他（打字）」（補充契約 S4 第 1、7 點）。
export function PendingCard({
  api = primaryApi,
  session,
  pending,
  agentName,
  onHold,
  onAnswered,
}: PendingCardProps) {
  const [state, setState] = useState<"ask" | "sent" | "elsewhere">("ask");
  const [error, setError] = useState("");

  // 換了一個新的問題：重新開始。
  // biome-ignore lint/correctness/useExhaustiveDependencies: 只看 pending.id
  useEffect(() => {
    setState("ask");
    setError("");
  }, [pending.id]);

  const submit = async (decision: Decision, note: string) => {
    setState("sent");
    setError("");
    onHold(pending);
    try {
      const out = await api.session.resolve({
        session,
        pending_id: pending.id,
        decision,
      });
      if (out.applied) {
        onAnswered({ pendingId: pending.id ?? "", note });
        onHold(null);
      } else {
        setState("elsewhere");
        window.setTimeout(() => {
          onAnswered({ pendingId: pending.id ?? "", note: null });
          onHold(null);
        }, ELSEWHERE_MS);
      }
    } catch (err) {
      onHold(null);
      setState("ask");
      setError(
        t("pending.failed", { reason: err instanceof ApiError ? err.message : String(err) }),
      );
    }
  };

  const isPermission = pending.kind === "permission";
  // 逾時後回到她畫面上等的確認（SES-48、WEB-74）：權限照樣能按（由引擎代按，拒絕不附理由）；
  // 選擇題一題單選照樣能按（引擎在她畫面上代按，WEB-134、ECL-58），多題或複選才只列出題目與提示（WEB-131）。
  const native = Boolean(pending.native);
  const summary = pending.summary || pending.tool || "";
  return (
    <section
      aria-label={
        isPermission
          ? t("pending.permission", { name: agentName })
          : t("pending.question", { name: agentName })
      }
      data-pending-card
      // 不限高、不自己捲（WEB-157）：卡片裡再一層捲動，手機上會卡住、看不到後面的字；整張交給訊息區捲。
      // 長網址、長路徑要折行（會繼承到裡面的文字），不然整個訊息區變成能左右滑。
      className="hg-rise flex min-w-0 flex-col gap-2.5 rounded-2xl bg-attention-soft px-3.5 py-3 [overflow-wrap:anywhere]"
    >
      <div className="flex items-center gap-2">
        <span className="size-2 shrink-0 rounded-full bg-attention" aria-hidden="true" />
        <span className="text-[13px] font-semibold text-attention-fg">
          {isPermission
            ? t("pending.permission", { name: agentName })
            : t("pending.question", { name: agentName })}
        </span>
      </div>
      {isPermission ? (
        <div className="truncate rounded-[10px] bg-surface px-2.5 py-2 font-mono text-[13px] text-fg">
          {summary}
        </div>
      ) : pending.summary ? (
        // 問之前她說的那段話（為什麼問、選項的背景；作者 2026-11-02「缺了一開始總說明」）。
        <p
          data-testid="pending-intro"
          className="whitespace-pre-wrap text-[14px] leading-relaxed text-fg"
        >
          {pending.summary}
        </p>
      ) : null}
      {state === "sent" ? (
        <SentButton />
      ) : state === "elsewhere" ? (
        <p role="status" className="text-[13px] font-medium text-muted">
          {t("pending.elsewhere")}
        </p>
      ) : isPermission ? (
        <>
          {native ? (
            <p className="text-[13px] text-muted">{t("pending.native", { name: agentName })}</p>
          ) : null}
          <PermissionActions summary={summary} onSubmit={submit} direct={native} />
        </>
      ) : native && !answerableOnScreen(pending) ? (
        <NativeQuestion pending={pending} agentName={agentName} />
      ) : (
        <QuestionForm pending={pending} onSubmit={submit} />
      )}
      {error ? (
        <p role="alert" className="text-[13px] text-danger">
          {error}
        </p>
      ) : null}
    </section>
  );
}

// 按下去立刻有回饋（UXC-9）：按鈕換成停用的「已送出」。
function SentButton() {
  return (
    <button
      type="button"
      disabled
      className={cn(actionButton, "w-full bg-surface text-muted")}
      aria-live="polite"
    >
      <Spinner className="size-4" />
      {t("pending.sent")}
    </button>
  );
}

/** 被交回她畫面的選擇題，引擎會代按的：只有一題、單選（ECL-58）。 */
export function answerableOnScreen(pending: Pending): boolean {
  const qs = pending.questions ?? [];
  return qs.length === 1 && !qs[0]?.multi_select;
}

// 選擇題在她的畫面上等、引擎不會代按的（多題、複選）：題目與每個選項都照樣列出（只能看、不能按），請主人到她的對話裡回答（WEB-131）。
// 同樣的文字出現第二次起加上「#次數」，讓 React 的 key 不重複（題目、選項 label 都可能重複，也可能沒填）。
function uniqueKeys(texts: (string | undefined)[]): string[] {
  const seen = new Map<string, number>();
  return texts.map((x) => {
    const base = x ?? "";
    const n = (seen.get(base) ?? 0) + 1;
    seen.set(base, n);
    return n === 1 ? base : `${base}#${n}`;
  });
}

function NativeQuestion({ pending, agentName }: { pending: Pending; agentName: string }) {
  return (
    <div className="flex flex-col gap-2.5">
      {(pending.questions ?? []).map((q, qi, qs) => (
        <div key={uniqueKeys(qs.map((x) => x.text))[qi]} className="flex flex-col gap-1.5">
          {q.header ? <span className="text-xs text-muted">{q.header}</span> : null}
          <p className="text-[15px] font-semibold">{q.text}</p>
          {(q.options ?? []).length > 0 ? (
            <ul aria-label={q.text} className="flex flex-col gap-1.5">
              {(q.options ?? []).map((o, oi, os) => (
                <li
                  key={uniqueKeys(os.map((x) => x.label))[oi]}
                  className="rounded-[10px] bg-surface px-2.5 py-2"
                >
                  <p className="text-[14px] font-medium text-fg">{o.label}</p>
                  {(o.description ?? "").trim() !== "" ? (
                    <p className="mt-0.5 whitespace-pre-wrap text-[13px] leading-snug text-muted">
                      {o.description}
                    </p>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ))}
      <p role="note" className="text-[13px] text-muted">
        {t("pending.nativeQuestion", { name: agentName })}
      </p>
    </div>
  );
}

function PermissionActions({
  summary,
  onSubmit,
  direct = false,
}: {
  summary: string;
  onSubmit: (d: Decision, note: string) => void;
  /** 拒絕直接送出、不問理由（在她畫面上代按時理由送不過去）。 */
  direct?: boolean;
}) {
  const [denying, setDenying] = useState(false);
  const [reason, setReason] = useState("");
  if (!denying) {
    return (
      <div className="flex gap-2">
        <button
          type="button"
          onClick={() => onSubmit({ allow: true }, t("pending.note.allowed", { summary }))}
          className={cn(actionButton, "flex-1 bg-accent text-accent-fg")}
        >
          {t("pending.allow")}
        </button>
        <button
          type="button"
          onClick={() =>
            direct
              ? onSubmit({ allow: false }, t("pending.note.denied", { summary }))
              : setDenying(true)
          }
          className={cn(actionButton, "flex-1 bg-surface font-medium text-fg")}
        >
          {t("pending.deny")}
        </button>
      </div>
    );
  }
  const confirm = (e: FormEvent) => {
    e.preventDefault();
    const why = reason.trim();
    onSubmit(
      { allow: false, message: why || undefined },
      why
        ? t("pending.note.deniedWhy", { summary, reason: why })
        : t("pending.note.denied", { summary }),
    );
  };
  return (
    <form onSubmit={confirm} className="hg-rise flex flex-col gap-2">
      <label htmlFor="hg-deny-reason" className="text-[13px] font-semibold">
        {t("pending.reason")}
      </label>
      <input
        id="hg-deny-reason"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        placeholder={t("pending.reason.example")}
        autoComplete="off"
        // biome-ignore lint/a11y/noAutofocus: 按了「拒絕」才展開，直接輸入理由
        autoFocus
        className="h-11 rounded-xl border border-border-strong bg-surface px-3 text-base text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus"
      />
      <div className="flex gap-2">
        <button type="submit" className={cn(actionButton, "flex-1 bg-danger text-accent-fg")}>
          {t("pending.denyConfirm")}
        </button>
        <button
          type="button"
          onClick={() => setDenying(false)}
          className={cn(actionButton, "bg-transparent font-normal text-muted")}
        >
          {t("pending.cancel")}
        </button>
      </div>
    </form>
  );
}

function QuestionForm({
  pending,
  onSubmit,
}: {
  pending: Pending;
  onSubmit: (d: Decision, note: string) => void;
}) {
  const questions = pending.questions ?? [];
  const [picked, setPicked] = useState<Record<string, string[]>>({});
  const [typed, setTyped] = useState<Record<string, string>>({});
  const [typing, setTyping] = useState<Record<string, boolean>>({});

  const answerOf = (text: string, multi: boolean): string => {
    if (typing[text]) return (typed[text] ?? "").trim();
    return (picked[text] ?? []).join(multi ? ", " : "");
  };
  const complete = questions.every((q) => answerOf(q.text ?? "", q.multi_select) !== "");

  const send = (answers: Record<string, string>) =>
    onSubmit(
      { allow: true, answers },
      t("pending.note.answered", { answer: Object.values(answers).join("、") }),
    );

  // 只有一題、單選：點了選項就直接送出（最短路徑）。
  const single = questions.length === 1 && !questions[0]?.multi_select;
  const choose = (q: (typeof questions)[number], label: string) => {
    const text = q.text ?? "";
    if (single) {
      send({ [text]: label });
      return;
    }
    setTyping((s) => ({ ...s, [text]: false }));
    setPicked((s) => {
      const have = s[text] ?? [];
      if (!q.multi_select) return { ...s, [text]: [label] };
      return {
        ...s,
        [text]: have.includes(label) ? have.filter((x) => x !== label) : [...have, label],
      };
    });
  };

  const submitAll = (e: FormEvent) => {
    e.preventDefault();
    if (!complete) return;
    send(
      Object.fromEntries(
        questions.map((q) => [q.text ?? "", answerOf(q.text ?? "", q.multi_select)]),
      ),
    );
  };

  const started =
    Object.values(picked).some((v) => v.length > 0) ||
    Object.values(typed).some((v) => v.trim() !== "");
  return (
    <form
      onSubmit={submitAll}
      className="flex flex-col gap-3"
      data-unsaved={started ? "" : undefined}
    >
      {questions.map((q) => {
        const text = q.text ?? "";
        const selected = picked[text] ?? [];
        const canType = q.allow_free_text || (q.options ?? []).length === 0;
        const described = (q.options ?? []).some((o) => (o.description ?? "").trim() !== "");
        return (
          <fieldset key={text} className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
            {/* 小標（引擎給的分類）放在題目上面；可以選好幾個的寫出來，不然看不出能多選（WEB-122）。 */}
            <legend className="flex flex-col gap-0.5 p-0">
              {q.header ? <span className="text-xs text-muted">{q.header}</span> : null}
              <span className="text-[15px] font-semibold">{text}</span>
            </legend>
            {q.multi_select ? (
              <span data-testid="multi-hint" className="-mt-1 text-xs text-muted">
                {t("pending.multiHint")}
              </span>
            ) : null}
            {/* 選項有說明時一列一個、說明寫在名字下面（作者 2026-11-02「缺了每一項的子說明」）；都沒有說明才排成一排小按鈕。 */}
            <div className={cn("flex gap-2", described ? "flex-col" : "flex-wrap")}>
              {(q.options ?? []).map((o) => {
                const on = !typing[text] && selected.includes(o.label ?? "");
                return (
                  <button
                    key={o.label}
                    type="button"
                    aria-pressed={on}
                    onClick={() => choose(q, o.label ?? "")}
                    className={cn(
                      "min-h-11 rounded-xl px-4 text-[15px] font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
                      described && "flex flex-col items-start gap-0.5 py-2.5 text-left",
                      on ? "bg-accent text-accent-fg" : "bg-surface text-fg",
                    )}
                  >
                    <span>{o.label}</span>
                    {o.description ? (
                      <span
                        data-testid="option-description"
                        className={cn(
                          "text-[13px] font-normal leading-snug",
                          on ? "text-accent-fg/85" : "text-muted",
                        )}
                      >
                        {o.description}
                      </span>
                    ) : null}
                  </button>
                );
              })}
              {canType ? (
                <button
                  type="button"
                  aria-pressed={Boolean(typing[text])}
                  onClick={() => setTyping((s) => ({ ...s, [text]: !s[text] }))}
                  className={cn(
                    "min-h-11 rounded-xl px-4 text-[15px] font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
                    typing[text] ? "bg-accent text-accent-fg" : "bg-surface text-muted",
                  )}
                >
                  {t("pending.other")}
                </button>
              ) : null}
            </div>
            {typing[text] ? (
              <>
                <label htmlFor={`hg-other-${text}`} className="sr-only">
                  {t("pending.otherLabel")}
                </label>
                <input
                  id={`hg-other-${text}`}
                  value={typed[text] ?? ""}
                  onChange={(e) => setTyped((s) => ({ ...s, [text]: e.target.value }))}
                  placeholder={t("pending.otherLabel")}
                  autoComplete="off"
                  enterKeyHint="send"
                  // biome-ignore lint/a11y/noAutofocus: 按了「其他（打字）」才展開，直接輸入
                  autoFocus
                  className="h-11 rounded-xl border border-border-strong bg-surface px-3 text-base text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus"
                />
              </>
            ) : null}
          </fieldset>
        );
      })}
      <p data-testid="type-to-skip" className="text-xs text-muted">
        {t("pending.typeToSkip")}
      </p>
      {single && !typing[questions[0]?.text ?? ""] ? null : (
        <button
          type="submit"
          disabled={!complete}
          className={cn(actionButton, "w-full bg-accent text-accent-fg")}
        >
          <Check className="size-4" strokeWidth={2} aria-hidden="true" />
          {t("pending.submit")}
        </button>
      )}
    </form>
  );
}
