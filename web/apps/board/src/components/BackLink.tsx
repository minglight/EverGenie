import { cn } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Link, useLocation, useRouter } from "@tanstack/react-router";
import { ChevronLeft } from "lucide-react";
import { type MouseEvent, type ReactNode, useEffect } from "react";
import { rosterQuery } from "../data/queries";
import { t } from "../i18n/zh-TW";
import { type BackTarget, backTarget } from "../lib/backTarget";
import { recordTrail, stepsBack } from "../lib/backTrail";
import { useIsDesktop } from "../lib/media";

const hrefOf = (l: { pathname: string; searchStr: string }) => `${l.pathname}${l.searchStr}`;

/** 外殼掛一次：記下每筆歷史是哪個網址（上一頁用來跳過同一個網址的筆）。 */
export function useBackTrail() {
  const location = useLocation();
  const index = location.state.__TSR_index;
  const href = hrefOf(location);
  useEffect(() => recordTrail(index, href), [index, href]);
}

/**
 * 這個畫面的上一層；null＝不畫上一頁：今天頁，或還沒有秘書時的設定精靈
 * （今天頁會立刻把人轉回精靈，上一頁在這裡等於原地繞圈）。
 */
export function useBackTarget(): BackTarget | null {
  const { pathname, search } = useLocation();
  const roster = useQuery(rosterQuery());
  const desktop = useIsDesktop();
  if (pathname.replace(/\/+$/, "") === "/settings/setup" && roster.data?.length === 0) return null;
  return backTarget(pathname, search as Record<string, unknown>, { settingsHome: !desktop });
}

/**
 * 上一頁（FEN-29）：有站內歷史就真的退（跳過跟現在同一個網址的筆）；退到頭才往上一層，而且用 replace 換掉現在這筆。
 * 永遠不疊新的歷史，所以不會「A → B → 上一頁 → A → 瀏覽器上一頁 → B」繞圈。
 */
export function BackLink({ className, children }: { className?: string; children: ReactNode }) {
  const router = useRouter();
  const location = useLocation();
  const target = useBackTarget();
  if (!target) return null;
  const onClick = (e: MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const steps = stepsBack(location.state.__TSR_index, hrefOf(location));
    if (steps === null) return;
    e.preventDefault();
    router.history.go(-steps);
  };
  return (
    <Link
      to={target.to as "/"}
      search={target.search as never}
      replace
      aria-label={t("app.back")}
      data-testid="back-link"
      onClick={onClick}
      className={className}
    >
      {children}
    </Link>
  );
}

/** 一條上一頁：桌機內容區頂端（在捲動區外，一直看得到）；手機討論串當掉時也用它。沒有上一頁就不佔位。 */
export function BackBar() {
  const target = useBackTarget();
  if (!target) return null;
  return (
    <div className="flex h-11 shrink-0 items-center border-b border-border px-2">
      <BackLink
        className={cn(
          "inline-flex min-h-11 items-center gap-0.5 rounded-lg px-1 text-sm font-medium text-accent",
          "focus-visible:outline-2 focus-visible:outline-focus",
        )}
      >
        <ChevronLeft className="size-4" aria-hidden="true" />
        {t("app.back")}
      </BackLink>
    </div>
  );
}
