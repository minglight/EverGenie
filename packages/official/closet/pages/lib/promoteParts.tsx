// 「修改」分頁那套衣服塊裡的「升成官方（公開給所有人）」（CMI-16，DEV-160 審查 S1 從 closet-mine 的「衣服」分頁搬過來，流程不變）：
// 只在衣櫃的程式算好可以升（closet.person 的 promotable，CLO-29）、closet-mine 開著時出現。
// 按了先展開：填官方的新編號（給建議值）→ 預演（寫一筆 dry_run 的 closet-mine.request，什麼都不動）→ 白話說會怎麼做
// → 寫明收不回、按「確定升成官方」才正式送。判斷全部以 closet-mine 的程式為準（CMI-14），頁面只顯示結果或被拒的原因。
import { Button } from "@evergenie/ui";
import { useEffect, useRef, useState } from "react";
import {
  checkNewId,
  POLL_MS,
  type PromoteRequest,
  promotePreview,
  promotePreviewLines,
  suggestNewId,
  T,
  TIMEOUT_MS,
} from "./promote";
import { loadMineRequest, reasonOf, sendPromote } from "./store";

type Step =
  | { kind: "closed" }
  | { kind: "form" }
  | { kind: "busy"; real: boolean }
  | { kind: "preview"; id: string; result: string; detail: string };

/**
 * 等程式處理完那筆請求（最多 10 秒）；沒等到回 undefined。讀結果時出錯（網路閃一下）不算失敗，下一輪再讀——請求已經寫進去了，程式會照做。
 * stop() 回 true 就不等了（預演時面板關掉了）；正式送出不停：就算面板被重畫掉，也要等到結果告訴主人。
 */
async function waitFor(id: string, stop: () => boolean): Promise<PromoteRequest | undefined> {
  const started = Date.now();
  while (Date.now() - started < TIMEOUT_MS) {
    await new Promise((r) => setTimeout(r, POLL_MS));
    if (stop()) return undefined;
    try {
      const req = await loadMineRequest(id);
      if (req && (req.state === "done" || req.state === "rejected")) return req;
    } catch {
      // 下一輪再讀。
    }
  }
  return undefined;
}

export function PromoteOutfit({
  record,
  outfit,
  onDone,
}: {
  /** closet.outfit 的編號（例 helper-my-12）。 */
  record: string;
  /** 衣服 id（例 my-12）：建議的新編號照它。 */
  outfit: string;
  /**
   * 正式送出的結果，給上層（整個分頁）顯示的一行：ok = true 是升成官方了（頁面重讀，這套會換成官方的那筆）；
   * ok = false 是面板已經不在時的被拒原因或「還在處理」。
   */
  onDone: (text: string, ok: boolean) => void;
}) {
  const [step, setStep] = useState<Step>({ kind: "closed" });
  const [newId, setNewId] = useState(() => suggestNewId(outfit));
  const [error, setError] = useState("");
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const busy = step.kind === "busy";
  const idError = checkNewId(newId);

  const send = async (real: boolean) => {
    if (idError) {
      setError(idError);
      return;
    }
    const id = newId.trim();
    setError("");
    setStep({ kind: "busy", real });
    let reqId: string;
    try {
      reqId = await sendPromote(record, id, !real);
    } catch (e) {
      // 請求沒寫進去：什麼都沒發生。
      if (!alive.current) return;
      setError(`${T.sendError}${reasonOf(e)}`);
      setStep({ kind: "form" });
      return;
    }
    // 正式送出後這個塊可能被重畫掉（那套換成官方的那筆、頁面每幾秒重讀）：結果一律交給上層顯示，不因為塊不在就不說。
    const req = await waitFor(reqId, () => !real && !alive.current);
    if (real) {
      if (req?.state === "done") {
        onDone(T.done(req.result), true);
      } else {
        const text = req ? `${T.rejected}${req.reason || T.unknownReject}` : T.pending;
        if (alive.current) setError(text);
        else onDone(text, false);
      }
      if (alive.current) setStep({ kind: req?.state === "done" ? "closed" : "form" });
      return;
    }
    if (!alive.current) return;
    if (!req) {
      setError(T.timeout);
      setStep({ kind: "form" });
      return;
    }
    if (req.state === "rejected") {
      setError(`${T.rejected}${req.reason || T.unknownReject}`);
      setStep({ kind: "form" });
      return;
    }
    setStep({ kind: "preview", id, result: req.result, detail: req.detail });
  };

  if (step.kind === "closed") {
    return (
      <Button
        variant="secondary"
        className="self-start"
        data-testid="edit-promote-open"
        onClick={() => setStep({ kind: "form" })}
      >
        {T.open}
      </Button>
    );
  }
  const preview = step.kind === "preview" ? promotePreview(record, step.result, step.detail) : null;
  // 預演後又改了新編號：那次預演不算，要再預演一次才能確認。
  const stale = step.kind === "preview" && step.id !== newId.trim();
  return (
    <div
      data-testid="edit-promote"
      data-record={record}
      className="flex w-full min-w-0 flex-col gap-2 rounded-xl border border-border bg-surface-2 p-3"
    >
      <p className="m-0 text-sm font-semibold">{T.title}</p>
      <p className="m-0 text-xs text-muted">{T.hint}</p>
      <label className="flex flex-col gap-1 text-sm">
        <span>{T.newId}</span>
        <input
          data-testid="edit-promote-id"
          value={newId}
          disabled={busy}
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          onChange={(e) => {
            setNewId(e.target.value);
            setError("");
          }}
          className="min-h-11 rounded-lg border border-border bg-bg px-3 text-base text-fg"
        />
        <span className="text-xs text-muted">{T.newIdHint}</span>
      </label>
      {error ? (
        <p
          role="alert"
          data-testid="edit-promote-error"
          className="m-0 text-sm font-medium text-danger"
        >
          {error}
        </p>
      ) : null}
      {preview && !stale ? (
        <div data-testid="edit-promote-preview" className="flex flex-col gap-1 text-sm">
          <ul className="m-0 flex list-disc flex-col gap-1 pl-5">
            {promotePreviewLines(preview).map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
          {step.kind === "preview" && step.detail ? (
            <details className="text-xs text-muted">
              <summary className="min-h-11 cursor-pointer content-center">{T.detail}</summary>
              <pre
                data-testid="edit-promote-detail"
                className="whitespace-pre-wrap break-all font-sans"
              >
                {step.detail}
              </pre>
            </details>
          ) : null}
          <p
            role="alert"
            data-testid="edit-promote-warn"
            className="m-0 rounded-lg bg-surface p-2 text-sm font-semibold text-danger"
          >
            {T.warn}
          </p>
        </div>
      ) : null}
      {stale ? <p className="m-0 text-sm text-muted">{T.changed}</p> : null}
      {busy ? (
        <p role="status" className="m-0 text-sm text-muted">
          {step.real ? T.sending : T.previewing}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        {preview && !stale ? (
          <Button
            variant="danger"
            data-testid="edit-promote-confirm"
            disabled={busy}
            onClick={() => void send(true)}
          >
            {T.confirm}
          </Button>
        ) : (
          <Button
            variant="secondary"
            data-testid="edit-promote-preview-btn"
            disabled={busy}
            onClick={() => void send(false)}
          >
            {T.preview}
          </Button>
        )}
        <Button
          variant="ghost"
          disabled={busy}
          onClick={() => {
            setError("");
            setStep({ kind: "closed" });
          }}
        >
          {T.cancel}
        </Button>
      </div>
    </div>
  );
}
