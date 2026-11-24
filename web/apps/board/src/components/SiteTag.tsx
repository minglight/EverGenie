import { cn } from "@evergenie/ui";
import { ChevronDown } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import { type Site, type SiteRef, useSites } from "../data/sites";
import { t } from "../i18n/zh-TW";

// 看板連多台（S3，計畫 2026-11-01-board-multi-home 設計決定 5、9）：合併的畫面上標出「這是哪台」。
// 顏色＝[[web.sites]] 設的 color；沒設（主伺服器一定沒設）照電腦代號挑一個預設色（tokens.css 的 --site-1…6，亮暗各一套）。
// 只有一台時這些都不畫（畫面跟 S3 之前一樣），由呼叫端用 useMultiSite() 判斷（清單每筆用 ItemSiteTag）。
// S3-7b 加：新東西選建在哪台（SitePickChip）。某台用不了的提示在 SiteNotice.tsx、詳情頁找台在 SiteGate.tsx。

/** 預設色有幾個（tokens.css 的 --site-1 … --site-6）。 */
export const SITE_TONES = 6;

/** 照電腦代號（沒讀到就用台的 key／網址）挑預設色的編號 1…SITE_TONES；同一台在每個看板上顏色都一樣。 */
export function siteToneIndex(site: Pick<Site, "machine" | "key" | "base">): number {
  const seed = site.machine || site.key || site.base;
  let h = 0;
  for (const ch of seed) h = (h * 31 + (ch.codePointAt(0) ?? 0)) >>> 0;
  return (h % SITE_TONES) + 1;
}

/** 這台的顏色（CSS 值）：設定的 #rgb／#rrggbb，或預設色的 token。 */
export function siteColor(site: Pick<Site, "color" | "machine" | "key" | "base">): string {
  return site.color || `var(--site-${siteToneIndex(site)})`;
}

/** 放在元素上的 CSS 變數 `--site-color`（inline style 的 CSS 變數是 token 規則的例外，跟秘書顏色一樣）。 */
export function siteVars(site: Pick<Site, "color" | "machine" | "key" | "base">): CSSProperties {
  return { "--site-color": siteColor(site) } as CSSProperties;
}

/**
 * 台名：get_mode 的 label；主伺服器還沒讀到台名時叫「這台」（S3-7b）；別台從沒連上過（不知道台名）就用網址的主機名。
 */
export function siteName(site: Pick<Site, "label" | "base"> & { primary?: boolean }): string {
  if (site.label) return site.label;
  if (site.primary) return t("site.primary");
  try {
    return new URL(site.base).host;
  } catch {
    return site.base;
  }
}

/** 看板是不是連了不只一台（只有一台時合併的畫面都不畫台名）。 */
export function useMultiSite(): boolean {
  return useSites().length > 1;
}

/** 台名的小標籤：彩色圓點＋台名（＋detail，例如「正式 · 版本 x」）。 */
export function SiteTag({
  site,
  detail,
  className,
}: {
  site: Site;
  detail?: string;
  className?: string;
}) {
  const name = siteName(site);
  const text = detail ? `${name} · ${detail}` : name;
  return (
    <span
      data-testid="site-tag"
      data-site={site.key}
      title={text}
      style={siteVars(site)}
      className={cn(
        // 台名很長（沒連上過時是整個網址的主機名）也不能把旁邊的秘書名擠扁：自己最多 10rem、放不下先縮自己（DEV-156 U-23）。
        "inline-flex h-5 min-w-0 max-w-[min(100%,10rem)] shrink items-center gap-1 rounded-full bg-hover px-2 text-[11px] leading-none font-semibold text-muted",
        className,
      )}
    >
      <span aria-hidden="true" className="size-2 shrink-0 rounded-full bg-[var(--site-color)]" />
      <span className="truncate">{text}</span>
    </span>
  );
}

/** 「正式 · 版本 1.2.3」：對話頂端的台名後面那一段（版本讀不到就不寫）。 */
export function siteDetail(site: Pick<Site, "env" | "version">): string {
  const env =
    site.env === "dev" ? t("siteTag.env.dev") : site.env === "prod" ? t("siteTag.env.prod") : "";
  const version = site.version ? t("siteTag.version", { v: site.version }) : "";
  return [env, version].filter(Boolean).join(" · ");
}

/** 頭像外框用那台的顏色（多台時）；`on` 是 false 就原樣回傳。 */
export function SiteRing({
  site,
  on,
  children,
  className,
}: {
  site: Site;
  on: boolean;
  children: ReactNode;
  className?: string;
}) {
  if (!on) return <>{children}</>;
  return (
    <span
      data-site-ring={site.key}
      style={siteVars(site)}
      className={cn(
        "inline-flex shrink-0 rounded-full shadow-[0_0_0_2px_var(--bg),0_0_0_4px_var(--site-color)]",
        className,
      )}
    >
      {children}
    </span>
  );
}

/**
 * 合併清單（今天、訊息…，S3-7a）每筆的台名：只拿到台的 key（SiteRef）時用，自己找台、只有多台時才畫。
 * 長清單已經有 Site 的就直接用 SiteTag（加 useMultiSite 判斷），不必每列各查一次台清單。
 */
export function SiteRefTag({ site, className }: { site: SiteRef; className?: string }) {
  const sites = useSites();
  if (sites.length <= 1) return null;
  const found = sites.find((s) => s.key === site.key);
  return found ? <SiteTag site={found} className={className} /> : null;
}

/**
 * 清單每一筆標它在哪台（S3-7b 的任務、待辦、Goal、權限申請）：只有一台時不畫，畫面跟 S3 之前一樣。
 */
export function ItemSiteTag({ site, className }: { site: Site; className?: string }) {
  const multi = useMultiSite();
  if (!multi) return null;
  return <SiteTag site={site} className={cn("shrink-0", className)} />;
}

/**
 * 「建在哪台」的膠囊（只有一台時不畫）：顯示目前要建在哪台，點開可以改。
 * 只列能用的台（主伺服器＋ok 的別台）；選了別台不會退回主伺服器。
 */
export function SitePickChip({
  sites,
  value,
  onChange,
  className,
}: {
  sites: readonly Site[];
  value: string;
  onChange: (key: string) => void;
  className?: string;
}) {
  const all = useSites();
  if (all.length <= 1) return null;
  // 目標台可能是現在用不了的那台（秘書在那台，useSiteTarget 不退回主伺服器）：照樣顯示它，後面加狀態。
  const current = sites.find((s) => s.key === value) ?? all.find((s) => s.key === value);
  const usable = sites.some((s) => s.key === value);
  const color = current ? siteColor(current) : "var(--border)";
  return (
    <label
      data-testid="site-pick"
      data-site={value}
      className={cn(
        "relative inline-flex min-h-7 items-center gap-1 rounded-full border bg-surface py-0.5 pr-1.5 pl-2 text-[13px] leading-snug text-fg focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-focus",
        className,
      )}
      style={{ borderColor: color }}
    >
      <span aria-hidden="true" className="size-1.5 rounded-full" style={{ background: color }} />
      <span className="text-muted">{t("site.create.at")}</span>
      <span className="font-semibold">{current ? siteName(current) : ""}</span>
      {current && !usable ? (
        <span className="text-muted">
          {current.state === "checking" ? t("site.create.checking") : t("site.create.unavailable")}
        </span>
      ) : null}
      <ChevronDown className="size-3.5 text-muted" aria-hidden="true" />
      {/* 透明的原生選單蓋在膠囊上：點哪裡都能開，鍵盤與讀屏照原生選單走。 */}
      <select
        aria-label={t("site.create.label")}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="absolute inset-0 -my-2 cursor-pointer opacity-0"
      >
        {current && !usable ? (
          <option value={current.key} disabled>
            {siteName(current)}
          </option>
        ) : null}
        {sites.map((s) => (
          <option key={s.key} value={s.key}>
            {siteName(s)}
          </option>
        ))}
      </select>
    </label>
  );
}
