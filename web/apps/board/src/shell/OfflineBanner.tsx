import { CloudOff } from "lucide-react";
import { useOffline } from "../data/connection";
import { t } from "../i18n/zh-TW";

// 連不上看板服務時頁首一條淡色橫條（DEV-156 U-21）：畫面上的東西是剛才抓到的，動作可能送不出去。
// 連回來自動消失（串流重連成功會順便重抓資料）；斷一下就接回來的不出現，正常時不會閃。
export function OfflineBanner() {
  const offline = useOffline();
  if (!offline) return null;
  return (
    <div
      role="status"
      data-testid="offline-banner"
      className="flex shrink-0 items-center gap-2 border-b border-border bg-surface-2 px-4 py-1.5 text-[13px] text-muted"
    >
      <CloudOff className="size-4 shrink-0" aria-hidden="true" />
      <span className="min-w-0">{t("shell.offline")}</span>
    </div>
  );
}
