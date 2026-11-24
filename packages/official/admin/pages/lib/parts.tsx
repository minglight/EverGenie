// 開發工坊頁面共用的小零件：載入 hook、卡片、一列欄位、重新檢查鈕、開單鈕（先在畫面內確認）。
import { Button, Spinner } from "@evergenie/ui";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import {
  fill,
  nameAfterCJK,
  type Opened,
  runningTask,
  T,
  taskHref,
  trackOpened,
  waitRequest,
} from "./release";
import { type Desk, loadRequest, openTask, reasonOf, sendRefresh } from "./store";

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
          if (live) setError(reasonOf(e));
        });
    void run();
    const timer = pollMs > 0 ? setInterval(run, pollMs) : undefined;
    return () => {
      live = false;
      if (timer) clearInterval(timer);
    };
  }, [tick, pollMs]);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  return { data, error, reload };
}

export function Card({
  title,
  testId,
  children,
}: {
  title: string;
  testId?: string;
  children: ReactNode;
}) {
  return (
    <section
      data-testid={testId}
      className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4"
    >
      <h2 className="font-semibold">{title}</h2>
      {children}
    </section>
  );
}

export function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-sm">
      <span className="text-muted">{label}</span>
      {/* 值短的時候 justify-between 把它推到右邊、跟標籤同一行；換到下一行時是那行唯一一個，靠左；
          多行的值也整段靠左，不一行行靠右對齊（ADM-32）。 */}
      <span data-testid="admin-row-value" className="min-w-0 break-words text-left">
        {children}
      </span>
    </div>
  );
}

export function LoadError({ error }: { error: string }) {
  return error ? (
    <p role="alert" className="text-sm text-danger">
      {T.loadError}
      {error}
    </p>
  ) : null;
}

/** 重新檢查：寫一張重算請求，等程式回填 done 再叫 onDone 重載；被拒就顯示原因。 */
export function RefreshButton({
  target,
  onDone,
}: {
  target: "all" | "release" | "devhome";
  onDone: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const run = async () => {
    setBusy(true);
    setError("");
    try {
      const id = await sendRefresh(target);
      const res = await waitRequest(
        () => loadRequest(id),
        () => new Promise((r) => setTimeout(r, 1000)),
      );
      if (res.end === "rejected") setError(T.refreshRejected + res.reason);
      if (res.end === "timeout") setError(T.refreshTimeout);
      onDone();
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col gap-1">
      <Button
        className="self-start"
        disabled={busy}
        onClick={() => void run()}
        data-testid="admin-refresh"
      >
        {busy ? <Spinner className="size-4" /> : null}
        {busy ? T.refreshing : T.refresh}
      </Button>
      {error ? (
        <p role="alert" className="text-[13px] text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

const linkClass =
  "inline-flex min-h-11 items-center self-start rounded-lg bg-accent px-4 text-base font-medium text-accent-fg hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";

/**
 * 開單鈕：按了先在畫面內確認，確定才開一張給 admin 秘書的任務單（不新增遠端執行的通道）。
 * 同一種（標題開頭 prefix）還沒結束的單已經有了就不開，改成連到那張。
 */
export function TaskButton({
  desk,
  prefix,
  label,
  confirm,
  yes,
  title,
  body,
  disabled = false,
  onOpened,
  testId,
}: {
  desk: Desk | undefined;
  prefix: string;
  label: string;
  confirm: string;
  yes: string;
  title: string;
  body: () => string;
  disabled?: boolean;
  onOpened: () => void;
  testId?: string;
}) {
  const [ask, setAsk] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [opened, setOpened] = useState<Opened | null>(null);
  // 清單每次重讀都對一次：剛開的單列到之後交給清單，單結束了按鈕才會回來（ADM-30）。
  useEffect(() => {
    if (desk) setOpened((o) => trackOpened(o, desk.open));
  }, [desk]);
  if (desk === undefined) return null;
  if (!desk.name) return <p className="text-[13px] text-muted">{T.noAgent}</p>;
  const running = runningTask(desk.open, prefix) ?? (opened && !opened.seen ? opened : undefined);
  if (running) {
    return (
      <div className="flex flex-col gap-1.5" data-testid={testId ? `${testId}-running` : undefined}>
        <p className="text-[13px] text-muted">
          {fill(T.running, { id: running.id ?? "", title: running.title ?? "" })}
        </p>
        <a href={taskHref(running.id ?? "")} className={linkClass}>
          {T.see}
        </a>
        {error ? (
          <p role="alert" className="text-[13px] text-danger">
            {error}
          </p>
        ) : null}
      </div>
    );
  }
  const create = async () => {
    setBusy(true);
    setError("");
    try {
      const { id, notStarted } = await openTask(title, body());
      setOpened({ id, title, seen: false });
      setAsk(false);
      onOpened();
      if (notStarted) setError(fill(T.notStarted, { reason: notStarted }));
    } catch (e) {
      setError(fill(T.failed, { reason: reasonOf(e) }));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col gap-2">
      {ask ? (
        <div role="alert" className="flex flex-col gap-2 rounded-xl bg-attention-soft p-3">
          <p className="text-sm text-attention-fg">
            {fill(confirm, { name: nameAfterCJK(desk.name) })}
          </p>
          <div className="flex gap-2">
            <Button variant="primary" disabled={busy} onClick={() => void create()}>
              {busy ? <Spinner className="size-4 text-accent-fg" /> : null}
              {yes}
            </Button>
            <Button disabled={busy} onClick={() => setAsk(false)}>
              {T.confirmNo}
            </Button>
          </div>
        </div>
      ) : (
        <Button
          variant="primary"
          className="self-start"
          disabled={disabled}
          onClick={() => setAsk(true)}
          data-testid={testId}
        >
          {label}
        </Button>
      )}
      {error ? (
        <p role="alert" className="text-[13px] text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}
