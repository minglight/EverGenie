import { t } from "../../i18n/zh-TW";

export type AccountRestartPhase = "offer" | "working" | "done";

// 設定 → 職位與秘書：換帳號成功之後，那位秘書卡上的「重開她的對話」（DEV-106）。
// 已經開著的對話還在用舊帳號，重開才會換；重開會中斷她正在做的事、換了帳號她會從頭開始，所以先把話說在按鈕前面。
export function AccountRestart({
  phase,
  name,
  onRestart,
}: {
  phase: AccountRestartPhase;
  name: string;
  onRestart: () => void;
}) {
  return (
    <div
      data-testid="agent-restart"
      className="flex items-center justify-between gap-2 rounded-xl bg-surface-2 py-1 pr-1 pl-3.5"
    >
      <span role="status" className="min-w-0 py-2 text-[13px] leading-snug text-muted">
        {phase === "done"
          ? t("chat.restart.done", { name })
          : t("settings.agents.restart.note", { name })}
      </span>
      {phase === "done" ? null : (
        <button
          type="button"
          onClick={onRestart}
          disabled={phase === "working"}
          className="flex min-h-11 shrink-0 items-center rounded-lg bg-accent px-3 text-[14px] font-semibold text-accent-fg focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-60"
        >
          {phase === "working" ? t("chat.restart.working") : t("settings.agents.restart.button")}
        </button>
      )}
    </div>
  );
}
