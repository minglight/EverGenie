import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { type Site, sitesConfigQuery, siteUsable, useHomeParam, useHomeSite } from "../data/sites";
import { t } from "../i18n/zh-TW";
import { PageTitle } from "./PageTitle";
import { siteNoticeText } from "./SiteNotice";
import { siteName } from "./SiteTag";
import { LoadingBlock } from "./StateViews";

// 詳情頁的「這筆在哪台」（S3-7b）：任務、待辦、Goal 的詳情照網址的 ?home= 找台，用不了的台講原因（文字跟 SiteNotice 同一套）。

/**
 * 詳情頁照網址的 `?home=` 找那台：還在試連畫轉圈；清單裡沒有這台說找不到；別台用不了說原因；
 * 能用才把台交給 children（主伺服器永遠能用，網址不帶 ?home 時跟 S3 之前一樣）。
 */
export function SiteGate({
  back,
  children,
}: {
  back: { to: "/tasks" | "/reminders" | "/goals"; label: string };
  children: (site: Site) => ReactNode;
}) {
  const { site, pending } = useHomeSite();
  const home = useHomeParam();
  // 台清單（web.get_sites）還沒回來前，網址的 ?home= 還對不到台：先轉圈，不閃「找不到這台」。
  const config = useQuery({ ...sitesConfigQuery, enabled: home !== "" });
  if (!site) {
    if (pending || (home !== "" && config.isPending)) return <LoadingBlock />;
    return (
      <GateMessage
        title={t("site.detail.unknown")}
        hint={t("site.detail.unknown.hint", { home })}
        back={back}
      />
    );
  }
  if (siteUsable(site)) return <>{children(site)}</>;
  if (site.state === "checking") return <LoadingBlock />;
  return (
    <GateMessage
      title={t("site.detail.unavailable", { name: siteName(site) })}
      hint={siteNoticeText(site) ?? ""}
      back={back}
    />
  );
}

function GateMessage({
  title,
  hint,
  back,
}: {
  title: string;
  hint: string;
  back: { to: "/tasks" | "/reminders" | "/goals"; label: string };
}) {
  return (
    <div
      data-testid="site-gate"
      className="flex flex-col items-center gap-1.5 px-6 py-16 text-center"
    >
      <PageTitle className="text-[17px] font-semibold">{title}</PageTitle>
      <p className="text-[13px] text-muted">{hint}</p>
      <Link
        to={back.to}
        replace
        className="mt-3 flex min-h-11 items-center rounded-xl bg-accent px-5 text-[15px] font-semibold text-accent-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      >
        {back.label}
      </Link>
    </div>
  );
}
