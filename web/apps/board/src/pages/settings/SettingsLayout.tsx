import { cn } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { withSectionTitles } from "../../components/SectionTabs";
import { menuQuery } from "../../data/queries";
import { type TextKey, t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";

export type SettingsTab =
  | "notify"
  | "display"
  | "trust"
  | "devices"
  | "phone"
  | "menu"
  | "packages"
  | "agents"
  | "rules"
  | "machine"
  | "setup"
  | "health"
  | "log";

/** 設定分頁的分類（REQ-237）：常用的在上面，少用的收在「進階」，十三個分頁不用一個一個找（角色庫 2026-11-04 搬到「角色」頁，WEB-136）。 */
export const settingsCategories: { label: TextKey; tabs: SettingsTab[] }[] = [
  { label: "settings.cat.basic", tabs: ["setup", "notify", "display", "phone", "devices", "menu"] },
  { label: "settings.cat.agents", tabs: ["agents", "trust", "rules"] },
  { label: "settings.cat.advanced", tabs: ["packages", "machine", "health", "log"] },
];

/** 每個分頁的名稱；指令面板也用它讓人打「通知」找到「設定 · 通知」（WEB-101）。 */
export const tabLabel: Record<SettingsTab, TextKey> = {
  notify: "settings.tab.notify",
  display: "settings.tab.display",
  trust: "settings.tab.trust",
  devices: "settings.tab.devices",
  phone: "settings.tab.phone",
  menu: "settings.tab.menu",
  packages: "settings.tab.packages",
  agents: "settings.tab.agents",
  rules: "settings.tab.rules",
  machine: "settings.tab.machine",
  setup: "settings.tab.setup",
  health: "settings.tab.health",
  log: "settings.tab.log",
};

/** 收到設定的頁面（MNU-10）：一列一個入口；同一頁的項目只留一個。 */
export function useTucked() {
  const { data } = useQuery(menuQuery());
  const seen = new Set<string>();
  return withSectionTitles(data?.tucked ?? []).filter((it) => {
    if (!it.section) return true;
    if (seen.has(it.section)) return false;
    seen.add(it.section);
    return true;
  });
}

function TuckedLinks({ className, linkClass }: { className: string; linkClass: string }) {
  const items = useTucked();
  if (items.length === 0) return null;
  return (
    <nav aria-label={t("settings.cat.tucked")} data-testid="settings-tucked" className={className}>
      <p className="px-2.5 pb-1 text-[11px] font-semibold tracking-[0.06em] text-muted">
        {t("settings.cat.tucked")}
      </p>
      {items.map((it) => (
        <Link key={it.id} to={(it.route ?? "/") as "/"} className={linkClass}>
          {(it.section && it.section_title) || it.title}
        </Link>
      ))}
    </nav>
  );
}

export interface SettingsLayoutProps {
  tab: SettingsTab;
  /** 這個分頁一句話的說明。 */
  intro: string;
  /** 頁首右邊的狀態（自動儲存的「已儲存」）；沒有就不佔位。 */
  status?: ReactNode;
  children: ReactNode;
}

// 設定頁的外框：桌機左邊 220 寬的分頁清單、右邊內容最寬 640。
// 手機沒有分頁列（DEV-156 U-12）：設定首頁 /settings 是一張清單（SettingsHome），點一列進來就是這一頁，
// 頁首只寫這頁的名字（路由的 mobileTitle），上一頁回清單；內容從頁首下面直接開始。
export function SettingsLayout({ tab, intro, status, children }: SettingsLayoutProps) {
  const desktop = useIsDesktop();

  if (!desktop) {
    return (
      <div className="mx-auto flex max-w-[640px] flex-col gap-3 px-4 pt-1 pb-8">
        <div className="flex items-start justify-between gap-3 px-1">
          <p className="min-w-0 pt-0.5 text-[13px] leading-snug text-muted">{intro}</p>
          {status}
        </div>
        {children}
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-[940px] gap-8 px-8 pt-6 pb-10">
      <div className="flex w-[220px] shrink-0 flex-col gap-3">
        <h1 className="text-2xl font-bold tracking-[-0.01em]">{t("settings.title")}</h1>
        <nav aria-label={t("settings.tabs")} className="flex flex-col gap-4">
          {settingsCategories.map((cat) => (
            <div key={cat.label} className="flex flex-col gap-0.5">
              <p className="px-2.5 pb-1 text-[11px] font-semibold tracking-[0.06em] text-muted">
                {t(cat.label)}
              </p>
              {cat.tabs.map((id) => {
                const current = id === tab;
                return (
                  <Link
                    key={id}
                    to={`/settings/${id}` as "/settings/notify"}
                    aria-current={current ? "page" : undefined}
                    className={cn(
                      "flex h-9 items-center rounded-[9px] px-2.5 text-sm hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
                      current ? "bg-surface font-semibold text-fg" : "font-medium text-muted",
                    )}
                  >
                    {t(tabLabel[id])}
                  </Link>
                );
              })}
            </div>
          ))}
        </nav>
        <TuckedLinks
          className="flex flex-col gap-0.5"
          linkClass="flex h-9 items-center rounded-[9px] px-2.5 text-sm font-medium text-muted hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        />
      </div>
      <div className="flex min-w-0 max-w-[640px] flex-1 flex-col gap-4">
        <div className="flex min-h-9 items-end justify-between gap-4">
          <h2 className="font-display text-xl font-bold">{t(tabLabel[tab])}</h2>
          {status}
        </div>
        <p className="-mt-2 px-1 text-[13px] text-muted">{intro}</p>
        {children}
      </div>
    </div>
  );
}
