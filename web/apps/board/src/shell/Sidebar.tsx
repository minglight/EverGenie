import type { MenuEntry } from "@evergenie/sdk-web";
import { Avatar, Badge, cn, Kbd } from "@evergenie/ui";
import { ChevronDown, ChevronRight, CircleAlert, Moon, Search, Sun } from "lucide-react";
import { useState } from "react";
import { siteNoticeText } from "../components/SiteNotice";
import { siteName } from "../components/SiteTag";
import { useAgentFilter } from "../data/agentFilter";
import {
  type FilterPerson,
  filterChipLabel,
  mergeFilterSites,
  personCount,
  useFilterSites,
} from "../data/filterSites";
import { usePageCounts } from "../data/pageCounts";
import { t } from "../i18n/zh-TW";
import { iconFor } from "../lib/icons";
import { REPLY_BADGES } from "../lib/statusTone";
import { useTheme } from "../lib/theme";
import { type FlatItem, flattenTree, isActiveItem, isActiveRoute } from "./flatten";
import { GenieMark } from "./GenieMark";
import { HomeLabel, useHome } from "./HomeMark";
import { NavLink } from "./NavLink";

const COLLAPSED_KEY = "eg.nav.collapsed";
const SETTINGS_ID = "core.settings";

function readCollapsed(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? "[]");
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
}

function writeCollapsed(ids: string[]) {
  try {
    localStorage.setItem(COLLAPSED_KEY, JSON.stringify(ids));
  } catch {
    // 寫不進去只是少了「記住」。
  }
}

export interface SidebarProps {
  tree: MenuEntry[];
  pathname: string;
  onSearch: () => void;
}

function NavItem({ item, pathname }: { item: FlatItem; pathname: string }) {
  const Icon = iconFor(item.icon);
  const current = isActiveItem(item, pathname);
  return (
    <NavLink
      route={item.route}
      aria-current={current ? "page" : undefined}
      className={cn(
        "flex h-9 items-center gap-2.5 rounded-[9px] px-2.5 text-sm hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
        current
          ? "bg-surface font-semibold text-fg shadow-[var(--shadow-card)]"
          : "font-medium text-muted",
      )}
    >
      <Icon
        className={cn("size-4 shrink-0", current && "text-accent")}
        strokeWidth={1.8}
        aria-hidden="true"
      />
      <span className="flex-1 truncate">{item.title}</span>
      {item.badge > 0 ? (
        <Badge
          tone={REPLY_BADGES.has(item.id) ? "attention" : "muted"}
          className="h-5 min-w-5 px-1.5"
        >
          {item.badge}
        </Badge>
      ) : null}
    </NavLink>
  );
}

// 桌機側欄的「只看這位」（REQ-155）：頭像格，點一下切換，跟手機的小卡列用同一份狀態；
// 件數是「目前這一頁」的（不在五頁之一就不顯示）。選單本身的徽章不受篩選影響。
// 連多台時兩台同一個職位合成一個頭像（作者 2026-11-02），點了兩台的她都看得到；不能用的台在頭像下面放一行短標記（整句在頁面內文講一次，DEV-156 U-23）。
function AgentFilterSection() {
  const sections = useFilterSites();
  const { picked, isOn, toggle, clear } = useAgentFilter();
  const page = usePageCounts();
  const multi = sections.length > 1;
  const { people, down } = mergeFilterSites(sections);
  if (people.length === 0) return null;
  const filtered = picked.length > 0;
  const grid = (agents: FilterPerson[]) => (
    <div className="grid grid-cols-3 gap-x-0.5 gap-y-1 px-0.5">
      {agents.map((agent) => {
        const on = isOn(agent.key);
        const n = page ? personCount(agent, page.counts) : 0;
        const base = filterChipLabel(
          agent,
          multi ? agent.sites.map(siteName).join(t("filter.names.sep")) : "",
        );
        const label = on ? `${base}${t("filter.chip.on")}` : base;
        return (
          <button
            key={agent.key}
            type="button"
            aria-pressed={on}
            data-count={n}
            data-agent={agent.key}
            data-agents={agent.keys.join(" ")}
            aria-label={label}
            title={label}
            onClick={() => toggle(agent.key)}
            className="relative flex min-h-[62px] flex-col items-center gap-1 rounded-[10px] py-1.5 text-fg hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
          >
            <span
              className={cn(
                "block size-10 rounded-full transition-[opacity,box-shadow]",
                on && "shadow-[0_0_0_2px_var(--side),0_0_0_4px_var(--accent)]",
                filtered && !on && "opacity-45",
              )}
            >
              <Avatar
                name={agent.name}
                color={agent.color}
                colorDark={agent.colorDark}
                src={agent.avatarUrl}
                size={40}
                pulse={agent.working}
              />
            </span>
            {n > 0 ? (
              <Badge
                tone="attention"
                className="absolute top-[3px] right-3 h-[17px] min-w-[17px] border-2 border-side px-1 text-[10px]"
              >
                {n}
              </Badge>
            ) : null}
            <span
              className={cn(
                "max-w-full truncate px-0.5 text-[11px]",
                on ? "font-bold text-fg" : "font-medium text-muted",
              )}
            >
              {agent.name}
            </span>
          </button>
        );
      })}
    </div>
  );
  return (
    <section aria-label={t("filter.label")} className="flex flex-col gap-2">
      <div className="flex items-center justify-between px-2">
        <span className="text-xs font-semibold text-muted">{t("filter.side.title")}</span>
        {filtered ? (
          <button
            type="button"
            onClick={clear}
            className="rounded-md py-1 text-xs font-semibold text-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
          >
            {t("filter.showAll")}
          </button>
        ) : null}
      </div>
      {people.length > 0 ? grid(people) : null}
      {multi
        ? down.map((site) =>
            siteNoticeText(site) !== null ? (
              // 那台連不上：這裡只放一行短標記，整句說明在頁面內文講一次（SiteProblems；DEV-156 U-23，跟手機的秘書列同一個做法）。
              <p
                key={site.key || "primary"}
                role="note"
                title={siteNoticeText(site) ?? undefined}
                aria-label={siteNoticeText(site) ?? undefined}
                data-testid="filter-site-notice"
                data-site={site.key}
                data-state={site.state}
                className="mx-0.5 flex items-center gap-1.5 rounded-lg bg-surface-2 px-2.5 py-2 text-xs text-muted"
              >
                <CircleAlert aria-hidden="true" className="size-4 shrink-0" />
                {t(site.state === "checking" ? "filter.site.checking" : "filter.site.down")}
              </p>
            ) : null,
          )
        : null}
    </section>
  );
}

// 桌機（≥ 768px）的預設選單：左側欄 240px，群組可收合，下半部是主題切換與設定。
export function Sidebar({ tree, pathname, onSearch }: SidebarProps) {
  const [collapsed, setCollapsed] = useState(readCollapsed);
  const { theme, toggle } = useTheme();
  const { userName } = useHome();
  const settings = flattenTree(tree).find((i) => i.id === SETTINGS_ID);

  const toggleGroup = (id: string) => {
    const next = collapsed.includes(id) ? collapsed.filter((x) => x !== id) : [...collapsed, id];
    setCollapsed(next);
    writeCollapsed(next);
  };

  return (
    <aside className="box-border flex w-60 shrink-0 flex-col gap-3.5 overflow-y-auto border-r border-border bg-side px-2.5 py-3.5">
      <div className="flex min-w-0 items-center gap-2.5 px-2 py-0.5">
        <GenieMark className="size-7 shrink-0" />
        <span className="truncate font-display text-[17px] font-bold tracking-[0.02em]">
          {userName ? t("shell.brand", { name: userName }) : t("shell.brand.plain")}
        </span>
      </div>
      <HomeLabel className="-mt-2 ml-[46px] self-start" />
      <button
        type="button"
        onClick={onSearch}
        className="flex h-[38px] items-center gap-2 rounded-[10px] border border-border bg-surface px-2.5 text-left text-[13px] text-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      >
        <Search className="size-4" strokeWidth={1.8} aria-hidden="true" />
        <span className="flex-1">{t("shell.searchOrCommand")}</span>
        <Kbd>⌘K</Kbd>
      </button>
      <AgentFilterSection />
      <nav aria-label={t("app.nav")} className="flex flex-col gap-0.5">
        {tree.map((entry) => {
          if (entry.children && entry.children.length > 0) {
            const id = entry.id ?? "";
            const open = !collapsed.includes(id);
            const Chevron = open ? ChevronDown : ChevronRight;
            const items = flattenTree([entry]).filter((i) => i.id !== SETTINGS_ID);
            if (items.length === 0) return null;
            return (
              <div key={id} className="flex flex-col gap-0.5">
                <button
                  type="button"
                  aria-expanded={open}
                  aria-label={t(open ? "shell.group.collapse" : "shell.group.expand", {
                    name: entry.title ?? "",
                  })}
                  onClick={() => toggleGroup(id)}
                  className="flex items-center justify-between rounded-md px-2.5 pt-1.5 pb-1 text-xs font-semibold text-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
                >
                  {entry.title}
                  <Chevron className="size-3.5" aria-hidden="true" />
                </button>
                {open
                  ? items.map((item) => <NavItem key={item.id} item={item} pathname={pathname} />)
                  : null}
              </div>
            );
          }
          const [item] = flattenTree([entry]);
          if (!item || item.id === SETTINGS_ID) return null;
          return <NavItem key={item.id} item={item} pathname={pathname} />;
        })}
      </nav>
      <div className="mt-auto flex flex-col gap-0.5">
        <button
          type="button"
          onClick={toggle}
          className="flex h-9 items-center gap-2.5 rounded-[9px] px-2.5 text-left text-sm text-muted hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        >
          {theme === "dark" ? (
            <Sun className="size-4" strokeWidth={1.8} aria-hidden="true" />
          ) : (
            <Moon className="size-4" strokeWidth={1.8} aria-hidden="true" />
          )}
          <span>{t(theme === "dark" ? "shell.theme.toLight" : "shell.theme.toDark")}</span>
        </button>
        {settings ? (
          <NavLink
            route={settings.route}
            aria-current={isActiveRoute(settings.route, pathname) ? "page" : undefined}
            className={cn(
              "flex h-9 items-center gap-2.5 rounded-[9px] px-2.5 text-sm hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
              isActiveRoute(settings.route, pathname)
                ? "bg-surface font-semibold text-fg"
                : "text-muted",
            )}
          >
            {(() => {
              const Icon = iconFor(settings.icon);
              return <Icon className="size-4" strokeWidth={1.8} aria-hidden="true" />;
            })()}
            <span>{settings.title}</span>
          </NavLink>
        ) : null}
      </div>
    </aside>
  );
}
