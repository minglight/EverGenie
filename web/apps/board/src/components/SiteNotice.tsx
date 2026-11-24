import { cn } from "@evergenie/ui";
import { CircleAlert } from "lucide-react";
import { type Site, siteUsable, useSites } from "../data/sites";
import { t } from "../i18n/zh-TW";
import { siteName } from "./SiteTag";

/**
 * 那台不能用時要講的一句話；能用（主伺服器、別台 ok）回 null。
 * 只看 `Site.state` 與伺服器給的 `Site.reason`（沒設 owner_login、不是主人的裝置、正式／開發不同…）；
 * 不認得的狀態一律寫「現在不能用」（S5 約定：不替新狀態另做畫面）。
 */
export function siteNoticeText(
  site: Pick<Site, "primary" | "state" | "reason" | "label" | "base" | "version">,
): string | null {
  if (siteUsable(site)) return null;
  const name = siteName(site);
  switch (site.state) {
    case "checking":
      return t("site.notice.checking", { name });
    case "down":
      return t("site.notice.down", { name });
    case "denied":
      return site.reason
        ? t("site.notice.deniedWhy", { name, reason: site.reason })
        : t("site.notice.denied", { name });
    case "old":
      return site.version
        ? t("site.notice.old", { name, version: site.version })
        : t("site.notice.oldNoVersion", { name });
    default:
      return t("site.notice.unavailable", { name });
  }
}

/**
 * 某台不能用時，在那台的區塊放一行灰色提示（不是警告色；S3-9，WEB-80）。
 * 合併清單的每一段（聊天的討論串分組、打開那台的對話）、設定頁的每一台都用它：
 * `<SiteNotice site={site} />`——能用的台什麼都不畫，所以可以無條件放。
 */
export function SiteNotice({ site, className }: { site: Site; className?: string }) {
  const text = siteNoticeText(site);
  if (!text) return null;
  return (
    <p
      role="status"
      data-testid="site-notice"
      data-site={site.key}
      data-state={site.state}
      className={cn(
        "flex items-start gap-2 rounded-xl bg-surface-2 px-3.5 py-2.5 text-[13px] leading-snug text-muted",
        className,
      )}
    >
      <CircleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
      <span className="min-w-0 flex-1 break-words">{text}</span>
    </p>
  );
}

/**
 * 合併清單（今天、訊息，S3-7a；任務、待辦、Goal、權限申請，S3-7b）上方：別台不能用的各一行 SiteNotice，其他台照常。
 * `failed`＝連得上、但這一頁的資料讀失敗的台（台的 key），多一行「那台的{what}讀不到」。
 * `only`＝只看這一台（詳情頁打開別台的一筆時用）。
 */
export function SiteProblems({
  what,
  failed = [],
  only,
  className,
}: {
  what: string;
  failed?: readonly string[];
  only?: string;
  className?: string;
}) {
  const sites = useSites();
  const rows = sites.filter(
    (s) =>
      !s.primary &&
      (only === undefined || s.key === only) &&
      (siteNoticeText(s) !== null || failed.includes(s.key)),
  );
  if (rows.length === 0) return null;
  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      {rows.map((s) =>
        siteNoticeText(s) !== null ? (
          <SiteNotice key={s.key} site={s} />
        ) : (
          <p
            key={s.key}
            role="status"
            data-testid="site-notice"
            data-site={s.key}
            data-state="failed"
            className="flex items-start gap-2 rounded-xl bg-surface-2 px-3.5 py-2.5 text-[13px] leading-snug text-muted"
          >
            <CircleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
            <span className="min-w-0 flex-1 break-words">
              {t("sitePart.failed", { site: siteName(s), what })}
            </span>
          </p>
        ),
      )}
    </div>
  );
}
