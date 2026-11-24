import { Avatar, Badge, cn } from "@evergenie/ui";
import { CircleAlert } from "lucide-react";
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
import { useIsWide } from "../lib/media";
import { SiteNotice, siteNoticeText } from "./SiteNotice";
import { siteName } from "./SiteTag";

export interface AgentFilterBarProps {
  className?: string;
}

const SKELETONS = [0, 1, 2, 3, 4];

const ring = "shadow-[0_0_0_2px_var(--bg),0_0_0_4px_var(--accent)]";

// 外框高度寫死（REQ-155，F2）：整列 88.5px＝框線 1 + 上內距 4 + 下內距 10 + 按鈕 73.5。
// 按鈕＝上下內距各 4 + 44 的頭像 + 5 的間距 + 16.5 的字（11px × html 行高 1.5）。
// 名冊、件數還沒到、換頁時分隔線都在同一個高度，不會跳；字級或頭像大小變了要一起改這個數字
// （e2e 的 TestTabSwitchNoLayoutShift 會量實際高度）。
const BAR_HEIGHT = "h-[88.5px]";

const pill =
  "flex w-14 shrink-0 flex-col items-center gap-[5px] py-1 text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";

// 手機（< 1024px）頁標題下面的秘書小卡：橫向捲動、一次選一位，五頁共用同一個選擇（REQ-155）。
// 由外殼（Shell）在五個頁面的上方畫一次，換分頁時是同一個 DOM 節點，不重掛。
// ≥ 1024px 改由側欄的「只看這位」負責，這一列不畫。件數讀 usePageCounts（跟桌機側欄同一份）。
// 連多台時兩台同一個職位合成一張小卡（作者 2026-11-02），點了兩台的她都看得到；不能用的台在小卡後面一格灰字（SiteNotice，高度跟小卡一樣、外框不跳）。
export function AgentFilterBar({ className }: AgentFilterBarProps) {
  const wide = useIsWide();
  const sections = useFilterSites();
  const { picked, isOn, toggle, clear } = useAgentFilter();
  const pageCounts = usePageCounts();
  if (wide) return null;
  const counts = pageCounts?.counts ?? {};
  const total = pageCounts?.total;
  const filtered = picked.length > 0;
  const multi = sections.length > 1;
  const { people, down } = mergeFilterSites(sections);
  const empty = people.length === 0;
  const chips = (agents: FilterPerson[]) =>
    agents.map((agent) => {
      const on = isOn(agent.key);
      const n = personCount(agent, counts);
      const base = filterChipLabel(
        agent,
        multi ? agent.sites.map(siteName).join(t("filter.names.sep")) : "",
      );
      return (
        <button
          key={agent.key}
          type="button"
          aria-pressed={on}
          data-count={n}
          data-agent={agent.key}
          data-agents={agent.keys.join(" ")}
          aria-label={on ? `${base}${t("filter.chip.on")}` : base}
          onClick={() => toggle(agent.key)}
          className={cn(pill, "relative")}
        >
          <span
            className={cn(
              "block size-11 rounded-full transition-[opacity,box-shadow]",
              on && ring,
              filtered && !on && "opacity-45",
            )}
          >
            <Avatar
              name={agent.name}
              color={agent.color}
              colorDark={agent.colorDark}
              src={agent.avatarUrl}
              size={44}
              pulse={agent.working}
            />
          </span>
          {n > 0 ? (
            <Badge tone="attention" className="absolute top-0 right-1 h-[18px] border-2 border-bg">
              {n}
            </Badge>
          ) : null}
          <span
            className={cn(
              "text-[11px] whitespace-nowrap",
              on ? "font-bold text-fg" : "font-medium text-muted",
            )}
          >
            {agent.name}
          </span>
        </button>
      );
    });
  return (
    <section
      aria-label={t("filter.label")}
      className={cn(
        "box-border shrink-0 border-b border-border bg-bg pt-1 pb-2.5",
        BAR_HEIGHT,
        className,
      )}
    >
      <div className="hg-scroll flex gap-1.5 overflow-x-auto px-4">
        <button type="button" aria-pressed={!filtered} onClick={clear} className={pill}>
          <span
            className={cn(
              "flex size-11 items-center justify-center rounded-full text-[13px] font-semibold transition-colors",
              filtered ? "bg-surface-2 text-muted" : cn("bg-accent-soft text-accent", ring),
            )}
          >
            {t("filter.all")}
          </span>
          <span className={cn("text-[11px]", filtered ? "text-muted" : "text-fg")}>
            {total === undefined ? t("filter.count.pending") : t("filter.count", { n: total })}
          </span>
        </button>
        {empty
          ? SKELETONS.map((k) => (
              <div key={k} aria-hidden="true" className={pill}>
                <span className="block size-11 rounded-full bg-surface-2" />
              </div>
            ))
          : null}
        {chips(people)}
        {multi && !empty
          ? down.map((site) =>
              siteNoticeText(site) !== null ? (
                // 那台連不上：只放一格小標記（跟頭像同一種格子），整句說明在頁面內文講一次（SiteProblems／SiteNotice），
                // 不在這裡再放一句被切掉的長字（DEV-156 U-23）。完整的話放在 title 與讀屏名稱。
                <div
                  key={site.key || "primary"}
                  role="note"
                  title={siteNoticeText(site) ?? undefined}
                  aria-label={siteNoticeText(site) ?? undefined}
                  data-testid="filter-site-notice"
                  data-site={site.key}
                  data-state={site.state}
                  className={cn(pill, "w-auto min-w-14")}
                >
                  <span className="flex size-11 items-center justify-center rounded-full bg-surface-2 text-muted">
                    <CircleAlert aria-hidden="true" className="size-5" />
                  </span>
                  <span className="text-[11px] font-medium whitespace-nowrap text-muted">
                    {t(site.state === "checking" ? "filter.site.checking" : "filter.site.down")}
                  </span>
                </div>
              ) : null,
            )
          : null}
      </div>
    </section>
  );
}
