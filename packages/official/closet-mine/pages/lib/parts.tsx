// 「上傳衣服與角色」頁面共用的小零件：定時重讀的 hook、送出請求並等結果的 hook、縮圖。
import { useCallback, useEffect, useRef, useState } from "react";
import { type Outcome, POLL_MS, requestOutcomeText, type SendFields, T, TIMEOUT_MS } from "./mine";
import { loadRequest, reasonOf, sendRequest } from "./store";

/** 一塊卡片的外框（表單、刪掉的清單）。 */
export const card = "flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4";

export interface Loaded<D> {
  data: D | undefined;
  error: string;
  reload: () => void;
}

/** 載一次、之後可以 reload；pollMs > 0 時定時再載一次。 */
export function useLoad<D>(fn: () => Promise<D>, pollMs = 0): Loaded<D> {
  const [data, setData] = useState<D>();
  const [error, setError] = useState("");
  const [tick, setTick] = useState(0);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  useEffect(() => {
    let live = true;
    const run = () =>
      fnRef
        .current()
        .then((d) => {
          if (!live) return;
          setError("");
          setData(d);
        })
        .catch((e: unknown) => {
          if (!live) return;
          console.warn("讀取失敗", e);
          setError(reasonOf(e));
        });
    void run();
    const timer = pollMs > 0 ? setInterval(run, pollMs) : undefined;
    return () => {
      live = false;
      if (timer) clearInterval(timer);
    };
  }, [tick, pollMs]);
  // 重讀時先清掉錯誤：畫面回到「載入中」，再失敗才又顯示，不會按了「再試一次」像沒反應。
  const reload = useCallback(() => {
    setError("");
    setTick((n) => n + 1);
  }, []);
  return { data, error, reload };
}

/** 送出的選項。at／doneAt 決定結果顯示在哪一行（`ResultLine` 的 at），讓回饋出現在按的那顆鈕旁邊（UXC-9）。 */
export interface SendOpts {
  /** 做完要說的話（沒給說「好了，衣櫃與展示間幾秒內會更新」）。 */
  doneText?: string;
  /** 處理中與失敗顯示在哪（空字串＝分頁最上面那行）。 */
  at?: string;
  /** 做完顯示在哪（沒給同 at）：按的那一列做完就不見時（刪掉）改到還在的地方。 */
  doneAt?: string;
}

export interface Sender {
  busy: boolean;
  /** 最近一次的結果或錯誤；null＝還沒送過。 */
  outcome: Outcome | { kind: "error"; text: string } | null;
  /** outcome 顯示在哪一行（SendOpts 的 at／doneAt）。 */
  at: string;
  /** 送出：上傳（有檔才傳）→ 寫請求 → 每 500 毫秒讀一次，最多 10 秒；回最後的結果（錯誤回 error）。 */
  send: (
    file: File | null,
    fields: Omit<SendFields, "upload">,
    opts?: SendOpts,
  ) => Promise<Outcome | { kind: "error"; text: string }>;
  /** 本機檢查沒過：直接顯示原因（在 at 那一行），不送出。 */
  fail: (text: string, at?: string) => void;
}

/** 送出請求並等結果；done 時呼叫 onDone（頁面重讀 closet.person）。 */
export function useSender(onDone: () => void): Sender {
  const [busy, setBusy] = useState(false);
  const [outcome, setOutcome] = useState<Sender["outcome"]>(null);
  const [at, setAt] = useState("");
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const send: Sender["send"] = async (file, fields, opts = {}) => {
    const place = opts.at ?? "";
    setBusy(true);
    setAt(place);
    setOutcome({ kind: "waiting" });
    let final: Outcome | { kind: "error"; text: string };
    try {
      const id = await sendRequest(file, fields);
      const started = Date.now();
      for (;;) {
        await new Promise((r) => setTimeout(r, POLL_MS));
        if (!alive.current) return { kind: "waiting" };
        const next = requestOutcomeText(
          await loadRequest(id),
          Date.now() - started >= TIMEOUT_MS,
          opts.doneText,
        );
        if (next.kind !== "waiting") {
          final = next;
          break;
        }
      }
    } catch (e) {
      final = { kind: "error", text: reasonOf(e) };
    }
    if (alive.current) {
      setBusy(false);
      setAt(final.kind === "done" ? (opts.doneAt ?? place) : place);
      setOutcome(final);
      if (final.kind === "done") onDone();
    }
    return final;
  };
  const fail: Sender["fail"] = (text, place = "") => {
    setAt(place);
    setOutcome({ kind: "error", text });
  };
  return { busy, outcome, at, send, fail };
}

/** 送出的結果：只在 sender 這次指定的那一行（at）出現，讓回饋在按的鈕旁邊（UXC-9）；空字串＝分頁最上面那行。 */
export function ResultLine({ sender, at = "" }: { sender: Sender; at?: string }) {
  const o = sender.outcome;
  if (!o || sender.at !== at) return null;
  if (o.kind === "waiting") {
    return (
      <p role="status" data-testid="mine-result" data-kind="waiting" className="text-sm text-muted">
        {T.sending}
      </p>
    );
  }
  const bad = o.kind === "rejected" || o.kind === "error";
  return (
    <p
      role={bad ? "alert" : "status"}
      data-testid="mine-result"
      data-kind={o.kind}
      className={bad ? "text-sm font-medium text-danger" : "text-sm font-medium text-ok"}
    >
      {o.kind === "error" ? `${T.sendError}${o.text}` : o.text}
    </p>
  );
}

/** 縮圖：沒有網址或載入失敗畫灰色人形。 */
export function Thumb({ url, testId = "mine-thumb" }: { url: string; testId?: string }) {
  const [failed, setFailed] = useState("");
  const show = url !== "" && failed !== url;
  return (
    <div className="relative aspect-square w-full overflow-hidden rounded-xl bg-surface-2">
      {show ? (
        <img
          data-testid={testId}
          src={url}
          alt=""
          loading="lazy"
          onError={() => setFailed(url)}
          className="absolute inset-0 size-full object-cover object-top"
        />
      ) : (
        <svg
          viewBox="0 0 100 100"
          aria-hidden="true"
          className="absolute inset-0 size-full text-border-strong"
          fill="currentColor"
        >
          <circle cx="50" cy="34" r="14" />
          <path d="M22 92c0-18 12-30 28-30s28 12 28 30z" />
        </svg>
      )}
    </div>
  );
}
