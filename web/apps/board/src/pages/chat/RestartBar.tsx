import type { Api, Session } from "@evergenie/sdk-web";
import { cn } from "@evergenie/ui";
import { RotateCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { t } from "../../i18n/zh-TW";
import { parseIso } from "../../lib/format";
import { reasonOf } from "../../lib/reason";
import { restartSession } from "../../lib/restartSession";

// 標題欄的鈕只負責打開這一列；這一列說明會發生什麼、等主人按「重開」才做（她正在做的事會中斷，不可逆，FEN-21）。
// 重開的做法在 lib/restartSession.ts（DEV-106）。結果照核心回的狀態寫：啟動中、排隊中、額度還沒恢復。
export const RESTART_BAR_ID = "restart-bar";

/** 重開之後核心回的狀態，寫成一句白話（不一律說「已重開」：排隊或額度用完時她沒有馬上起來）。 */
export function restartResult(name: string, s: Session | undefined): string {
  // queued_at 沒有值時伺服器給的是 Go 的零值時間（0001-01-01…），不是空字串：用 parseIso 才不會每次都說排隊。
  if (parseIso(s?.queued_at)) return t("chat.restart.done.queued", { name });
  if (s?.state === "limited") return t("chat.restart.done.limited", { name });
  return t("chat.restart.done", { name });
}

export function RestartBar({
  api,
  session,
  agentName,
  onClose,
  onDone,
  onBusy,
}: {
  api: Api;
  session: Session;
  agentName: string;
  onClose: () => void;
  /** 重開結束（成功或失敗）：外面重抓她現在的樣子。 */
  onDone: () => void;
  /** 重開進行中（true）／結束（false）：外面在進行中不收這一列，免得再按一次重複重開。 */
  onBusy?: (busy: boolean) => void;
}) {
  const [phase, setPhase] = useState<"ask" | "working" | "done">("ask");
  const [result, setResult] = useState("");
  const [error, setError] = useState("");
  const closeRef = useRef<HTMLButtonElement>(null);
  // 成功後「重開」鈕不見，焦點移到「知道了」，不掉到 body（a11y）。
  useEffect(() => {
    if (phase === "done") closeRef.current?.focus();
  }, [phase]);
  const restart = async () => {
    setPhase("working");
    setError("");
    onBusy?.(true);
    try {
      const s = await restartSession(api, session);
      setResult(restartResult(agentName, s));
      setPhase("done");
    } catch (err) {
      // 關掉了、開不起來：她現在是停著的（核心把她留在 stopped），可以再按一次。
      setError(t("chat.restart.failed", { reason: reasonOf(err) }));
      setPhase("ask");
    } finally {
      onBusy?.(false);
      onDone();
    }
  };
  return (
    <section
      id={RESTART_BAR_ID}
      data-testid="restart-bar"
      aria-label={t("chat.restart.title", { name: agentName })}
      className="flex shrink-0 flex-col gap-1 border-b border-border bg-surface-2 px-3 py-2"
    >
      {phase === "done" ? (
        <div className="flex items-center justify-between gap-2">
          <p role="status" className="min-w-0 text-[13px] text-muted">
            {result}
          </p>
          <button
            ref={closeRef}
            type="button"
            onClick={onClose}
            className="flex min-h-11 shrink-0 items-center rounded-lg px-3 text-[14px] font-medium text-accent focus-visible:outline-2 focus-visible:outline-focus"
          >
            {t("chat.restart.close")}
          </button>
        </div>
      ) : (
        <>
          <p className="text-[14px] font-medium">{t("chat.restart.title", { name: agentName })}</p>
          <p className="text-[13px] text-muted">{t("chat.restart.hint")}</p>
          <div className="flex items-center justify-end gap-1">
            <button
              type="button"
              onClick={onClose}
              disabled={phase === "working"}
              className="flex min-h-11 items-center rounded-lg px-3 text-[14px] font-medium text-muted focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-60"
            >
              {t("ui.cancel")}
            </button>
            <button
              type="button"
              onClick={() => void restart()}
              disabled={phase === "working"}
              className={cn(
                "flex min-h-11 items-center gap-1.5 rounded-lg bg-accent px-4 text-[14px] font-semibold text-accent-fg focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-60",
              )}
            >
              <RotateCw
                className={cn("size-4", phase === "working" && "motion-safe:animate-spin")}
                aria-hidden="true"
              />
              {phase === "working" ? t("chat.restart.working") : t("chat.restart.confirm")}
            </button>
          </div>
        </>
      )}
      {error ? (
        <p role="alert" className="text-[13px] text-danger">
          {error}
        </p>
      ) : null}
    </section>
  );
}
