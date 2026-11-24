import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ActionButton } from "../components/ActionButton";
import { closeFunnel, phoneQuery } from "../data/phone";
import { t } from "../i18n/zh-TW";
import { reasonOf } from "../lib/reason";

// 看板被 Tailscale Funnel 開到整個網路時（DEV-153）：不管在哪一頁都看得到的紅色警告。
// 一顆「關掉對外公開」（關掉是變安全的動作，手機、別台開的看板也按得動，SET-12）加說明的連結。沒開、讀不到就不畫。
export function FunnelBanner() {
  const client = useQueryClient();
  const { data } = useQuery(phoneQuery);
  const close = useMutation({ mutationFn: () => closeFunnel(client) });
  if (!data?.funnel) return null;
  return (
    <div
      role="alert"
      data-testid="funnel-banner"
      className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b border-danger bg-danger-soft px-4 py-2 text-[14px]"
    >
      <p className="min-w-0 flex-1 font-semibold text-danger">
        {t("settings.phone.funnel.title")}
        {close.isError
          ? `（${t("settings.phone.funnel.failed", { reason: reasonOf(close.error) })}）`
          : ""}
      </p>
      <ActionButton
        tone="danger"
        data-testid="funnel-banner-close"
        busy={close.isPending}
        onClick={() => close.mutate()}
      >
        {t("settings.phone.funnel.close")}
      </ActionButton>
      <Link
        to="/settings/phone"
        className="flex min-h-11 items-center font-medium text-fg underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      >
        {t("settings.phone.funnel.how")}
      </Link>
    </div>
  );
}
