import type { Package, Tier } from "@evergenie/sdk-web";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { homeQuery, invalidatePackageViews, packageListQuery } from "../../data/queries";
import { type TextKey, t } from "../../i18n/zh-TW";
import { FolderInstall } from "./FolderInstall";
import { InstallGoDialog } from "./InstallGoDialog";
import { hiddenDevOnly, OptionalPackages, optionalPackages } from "./OptionalPackages";
import { PackageExtensions } from "./PackageExtensions";
import { PackageOutdated } from "./PackageOutdated";
import { PackageRow } from "./PackageRow";
import { PackageUrlInstall } from "./PackageUrlInstall";
import { SettingGroup } from "./SettingRow";

const tiers: { tier: Tier; title: TextKey }[] = [
  { tier: "official", title: "settings.packages.tier.official" },
  { tier: "installed", title: "settings.packages.tier.installed" },
  { tier: "mine", title: "settings.packages.tier.mine" },
];

// 設定 → 套件：選用的進階套件（REQ-176，2026-10-28 從設定精靈搬來）、清單（三層）、補裝 Go、從資料夾與網址安裝、擴充點（REQ-93、94、171）。
export function PackagesSettings() {
  const query = useQuery(packageListQuery());
  const home = useQuery(homeQuery());
  const client = useQueryClient();
  const [goOpen, setGoOpen] = useState(false);

  if (query.data === undefined) {
    return query.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void query.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const dev = home.data?.env === "dev";
  const packages: Package[] = (query.data.packages ?? []).filter((p) => !hiddenDevOnly(p, dev));
  const warnings = query.data.warnings ?? [];
  const waitingGo = packages.filter((p) => p.remedy === "install_go").length;
  const names = Object.fromEntries(packages.map((p) => [p.id ?? "", p.name || p.id || ""]));
  // 官方的選用套件只在最上面「進階套件」列一次（開關），不在官方清單重複。
  const optional = optionalPackages(packages).filter((p) => p.tier === "official");
  const listed = packages.filter((p) => !optional.includes(p));

  return (
    <div className="flex flex-col gap-5">
      {/* 安裝的表單在清單最下面：手機上要捲過全部套件才看得到，頁首放一個入口直接跳過去（PKG-54）。 */}
      <button
        type="button"
        data-testid="packages-install-jump"
        className="inline-flex min-h-11 items-center self-start px-1 text-[14px] font-medium text-accent underline"
        onClick={() =>
          document
            .getElementById("packages-install")
            ?.scrollIntoView({ behavior: "smooth", block: "start" })
        }
      >
        {t("settings.packages.installJump")}
      </button>
      {warnings.length > 0 ? (
        <div
          role="status"
          data-testid="package-warnings"
          className="flex flex-col gap-1 rounded-xl bg-attention-soft px-4 py-3 text-[13px] leading-snug text-attention-fg"
        >
          <p className="font-semibold">{t("settings.packages.warnings")}</p>
          {warnings.map((w) => (
            <p key={w} className="break-words">
              {w}
            </p>
          ))}
        </div>
      ) : null}
      {waitingGo > 0 ? (
        <div
          data-testid="install-go-banner"
          className="flex flex-col gap-3 rounded-xl bg-attention-soft px-4 py-3 text-attention-fg sm:flex-row sm:items-center sm:justify-between"
        >
          <p className="text-[14px] leading-snug font-semibold">
            {t("settings.packages.goBanner", { n: waitingGo })}
          </p>
          <ActionButton tone="primary" onClick={() => setGoOpen(true)}>
            {t("settings.packages.goInstall")}
          </ActionButton>
        </div>
      ) : null}
      {optional.length > 0 ? (
        <section data-testid="packages-optional" className="flex flex-col gap-2">
          <h2 className="px-1 text-[13px] font-semibold text-muted">
            {t("settings.packages.optional")}
          </h2>
          <p className="px-1 text-[13px] leading-snug text-muted">
            {t("settings.packages.optional.hint")}
          </p>
          <div className="rounded-[18px] border border-border bg-surface px-4 py-3.5 shadow-[var(--shadow-card)]">
            <OptionalPackages packages={optional} />
          </div>
        </section>
      ) : null}
      <PackageOutdated />
      {packages.length === 0 ? (
        <p className="px-1 text-[13px] text-muted">{t("settings.packages.empty")}</p>
      ) : (
        tiers.map(({ tier, title }) => {
          const list = listed.filter((p) => p.tier === tier);
          return list.length === 0 ? null : (
            <SettingGroup key={tier} title={t(title)}>
              {list.map((p) => (
                <PackageRow key={p.id} pkg={p} />
              ))}
            </SettingGroup>
          );
        })
      )}
      <div
        id="packages-install"
        data-testid="packages-install"
        className="flex scroll-mt-4 flex-col gap-3"
      >
        <FolderInstall />
        <PackageUrlInstall />
      </div>
      <PackageExtensions names={names} />
      <InstallGoDialog
        open={goOpen}
        count={waitingGo}
        onClose={() => setGoOpen(false)}
        onInstalled={() => void invalidatePackageViews(client)}
      />
    </div>
  );
}
