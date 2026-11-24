import type { Tier } from "@evergenie/sdk-web";
import { t } from "../i18n/zh-TW";

/** 分享來源網址的短寫：去掉協定、帳號、結尾的 .git 與斜線（`https://github.com/friend/pond.git` → `github.com/friend/pond`）。 */
export function shortSource(url: string): string {
  return url
    .trim()
    .replace(/^[a-z+]+:\/\//i, "")
    .replace(/^[^@/]+@/, "")
    .replace(/^([^/:]+):(?!\d)/, "$1/")
    .replace(/\.git$/, "")
    .replace(/\/+$/, "");
}

/** 套件層級的標籤字（SEC-302 S4、REQ-350）：官方不標；自己的「我的」；分享的有來源就「分享自 <來源>」，沒有就「分享的」。 */
export function tierLabel(tier: Tier | undefined, source?: string): string {
  if (tier === "mine") return t("tier.mine");
  if (tier === "installed") {
    const from = source ? shortSource(source) : "";
    return from ? t("tier.sharedFrom", { source: from }) : t("tier.shared");
  }
  return "";
}

/** 頁面標題與選單編輯器旁的小標籤；官方的不畫。 */
export function TierBadge({ tier, source }: { tier: Tier | undefined; source?: string }) {
  const label = tierLabel(tier, source);
  if (!label) return null;
  return (
    <span
      data-testid="tier-badge"
      data-tier={tier}
      title={source || undefined}
      className="inline-flex max-w-full shrink-0 items-center truncate rounded-full border border-border px-2 py-0.5 text-xs font-medium text-muted"
    >
      {label}
    </span>
  );
}
