import { type Api, ApiError } from "@evergenie/sdk-web";
import { cn, showToast } from "@evergenie/ui";
import { CircleStop } from "lucide-react";
import { useState } from "react";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";

// 她在做事時，輸入框上面一列「停下她」（SES-62、WEB-87，2026-11-02 稽核：手機上沒辦法叫她停）：跟在她畫面上按停止一樣，
// 送 session.command interrupt；她這一輪會以「被中斷」結束，狀態回到待命。在要你同意時不出現（用「拒絕」）。
export function StopBar({
  api,
  session,
  agentName,
}: {
  api: Api;
  session: string;
  agentName: string;
}) {
  const desktop = useIsDesktop();
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const stop = async () => {
    setSending(true);
    setError("");
    try {
      await api.session.command({ session, command: "interrupt" });
      // 手機放上面：下面是輸入框與「/」選單，放下面會蓋住（WEB-114）。
      showToast(t("chat.stop.sent", { name: agentName }), { top: !desktop });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setSending(false);
    }
  };
  return (
    <div data-testid="stop-bar" className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2 rounded-xl bg-surface-2 py-1 pr-1 pl-3.5">
        <span className="min-w-0 truncate text-[13px] text-muted">
          {t("chat.stop.working", { name: agentName })}
        </span>
        <button
          type="button"
          onClick={() => void stop()}
          disabled={sending}
          className={cn(
            "flex min-h-11 shrink-0 items-center gap-1.5 rounded-lg px-3 text-[14px] font-medium text-danger focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-60",
          )}
        >
          <CircleStop className="size-4" aria-hidden="true" />
          {t("chat.stop.button")}
        </button>
      </div>
      {error ? (
        <p role="alert" className="px-1 text-[13px] text-danger">
          {t("chat.stop.failed", { reason: error })}
        </p>
      ) : null}
    </div>
  );
}
