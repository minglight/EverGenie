import { ApiError, type TaskStatus as Status, type Task } from "@evergenie/sdk-web";
import { cn, showToast } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { refreshAll } from "../data/live";
import { apiFor, PRIMARY, type SiteRef } from "../data/sites";
import { t } from "../i18n/zh-TW";
import { statusKey } from "../lib/taskText";

/** 主人能直接改到的狀態：等你要有問題或要秘書送驗收，不給手動選。 */
const OPEN: Status[] = ["idea", "doing", "waiting_external"];
/** 結單：一按就結束、不能再改，所以要按兩下確認。 */
const CLOSE: Status[] = ["done", "cancelled"];

/**
 * 主人手動改任務狀態（TSK-19）：沒結束的單可以改成待做／進行中／等外部，或直接結單（完成、不做了）。
 * 完成要結果與驗證方式（資料規格），沒寫過的就記成主人手動結單、無法驗證。
 */
export function TaskStatus({ task, site = PRIMARY }: { task: Task; site?: SiteRef }) {
  const client = useQueryClient();
  // 送到那張單所在的台（S3-7b）；主伺服器就是原本的 api。
  const api = apiFor(site);
  const id = task.id ?? "";
  const [busy, setBusy] = useState(false);
  const [armed, setArmed] = useState<Status | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  const run = async (to: Status) => {
    setBusy(true);
    try {
      if (CLOSE.includes(to)) {
        await api.tasks.close({
          id,
          status: to,
          result: task.result || (to === "done" ? t("task.manual.result") : ""),
          verify: to === "done" && task.type !== "ask" ? task.verify || "unverifiable" : undefined,
          verify_how:
            to === "done" && task.type !== "ask"
              ? task.verify_how || t("task.manual.verifyHow")
              : undefined,
        });
      } else {
        await api.tasks.update({ id, status: to });
      }
      showToast(t("task.status.changed", { status: t(statusKey[to]) }));
      await refreshAll(client, site);
    } catch (err) {
      showToast(
        t("task.status.changeFailed", {
          reason: err instanceof ApiError ? err.message : String(err),
        }),
      );
    } finally {
      setBusy(false);
    }
  };

  const press = (to: Status) => {
    clearTimeout(timer.current);
    if (CLOSE.includes(to) && armed !== to) {
      setArmed(to);
      timer.current = setTimeout(() => setArmed(null), 3000);
      return;
    }
    setArmed(null);
    void run(to);
  };

  const current = task.status ?? "doing";
  const chip =
    "inline-flex min-h-9 items-center rounded-full px-3.5 text-[13px] font-medium transition-colors focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-60";
  return (
    <section className="flex flex-col gap-1.5" aria-label={t("task.status.change")}>
      <h2 className="text-sm font-semibold">{t("task.status.change")}</h2>
      <div className="flex flex-wrap gap-2">
        {[...OPEN, ...CLOSE].map((to) => {
          const on = to === current;
          const closing = CLOSE.includes(to);
          return (
            <button
              key={to}
              type="button"
              disabled={busy || on}
              aria-pressed={on}
              aria-label={
                armed === to ? undefined : t("task.status.to", { status: t(statusKey[to]) })
              }
              onClick={() => press(to)}
              className={cn(
                chip,
                on
                  ? "bg-accent text-accent-fg"
                  : armed === to
                    ? to === "done"
                      ? "bg-ok-soft font-semibold text-ok"
                      : "bg-danger-soft font-semibold text-danger"
                    : closing
                      ? "border border-border bg-surface text-fg hover:bg-hover"
                      : "bg-surface-2 text-fg hover:bg-hover",
              )}
            >
              {armed === to
                ? t("task.status.confirm", { status: t(statusKey[to]) })
                : t(statusKey[to])}
            </button>
          );
        })}
      </div>
    </section>
  );
}
