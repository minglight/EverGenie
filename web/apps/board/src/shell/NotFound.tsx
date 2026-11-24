import { buttonVariants, cn } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Link, Navigate, useLocation } from "@tanstack/react-router";
import { pagesQuery } from "../data/queries";
import { t } from "../i18n/zh-TW";
import { useIsDesktop } from "../lib/media";

// 找不到的網址：白話說明＋回今天（REQ-108；舊看板的連結也會落在這裡）。
// 套件在 package.toml 寫的 route（例 /water）是別名：先轉到它的正式網址 /p/<套件>/<頁面>（DEV-91）。
export function NotFoundPage() {
  // 手機的頁首已經是這個標題，內文不再重複一次（避免兩個 h1）。
  const desktop = useIsDesktop();
  const pathname = useLocation({ select: (l) => l.pathname });
  const pages = useQuery(pagesQuery());
  const here = pathname.replace(/\/+$/, "");
  const alias = here ? pages.data?.find((p) => p.alias === here && p.route) : undefined;
  if (alias?.route) return <Navigate to={alias.route} replace />;
  return (
    <div className="flex min-h-full flex-col items-center justify-center gap-3 px-6 py-16 text-center">
      <p className="font-mono text-5xl font-bold text-border-strong" aria-hidden="true">
        404
      </p>
      {desktop ? <h1 className="text-xl font-bold">{t("notFound.title")}</h1> : null}
      <p className="max-w-sm text-sm text-muted">{t("notFound.hint")}</p>
      <Link to="/" replace className={cn(buttonVariants({ variant: "primary" }), "mt-2")}>
        {t("notFound.home")}
      </Link>
    </div>
  );
}
