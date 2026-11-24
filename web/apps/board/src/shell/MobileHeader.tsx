import { cn } from "@evergenie/ui";
import { ChevronLeft, Search } from "lucide-react";
import { BackLink, useBackTarget } from "../components/BackLink";
import { useOwnPageTitle } from "../components/PageTitle";
import { useAgentFilter } from "../data/agentFilter";
import { t } from "../i18n/zh-TW";
import { useNow } from "../lib/now";
import { greetingText, todaySubtitle } from "../pages/overview/greeting";
import { mergedWaitingTotal, useOverviews } from "../pages/overview/merged";
import { HomeLabel, useHome } from "./HomeMark";

export interface MobileHeaderProps {
  title: string;
  /** "today"：標題換成早安／午安／晚安，副標是日期與等你處理的件數。 */
  kind?: "today";
  onSearch: () => void;
}

// 手機頁首：高 60；左邊上一頁（今天頁沒有，FEN-29）與大標題，右邊 44×44 的搜尋鈕（打開指令面板）。
export function MobileHeader({ title, kind, onSearch }: MobileHeaderProps) {
  const now = useNow();
  const hasBack = useBackTarget() !== null;
  const { userName } = useHome();
  const Title = useOwnPageTitle() ? "p" : "h1";
  const { picked } = useAgentFilter();
  // 看多台時件數是各台相加（S3-7a），跟今天頁的大字同一份。
  const { parts } = useOverviews(kind === "today");
  const heading = kind === "today" ? greetingText(now, userName) : title;
  const subtitle = kind === "today" ? todaySubtitle(now, mergedWaitingTotal(parts, picked)) : null;
  return (
    <header
      className={cn(
        "flex h-[60px] shrink-0 items-center justify-between pt-2 pr-2",
        hasBack ? "pl-1" : "pl-5",
      )}
    >
      <BackLink className="flex size-11 shrink-0 items-center justify-center rounded-full text-fg focus-visible:outline-2 focus-visible:outline-focus">
        <ChevronLeft className="size-6" strokeWidth={1.8} aria-hidden="true" />
      </BackLink>
      <div className="mr-auto flex min-w-0 flex-col">
        <div className="flex min-w-0 items-center gap-2">
          {/* 內文自己有第一層標題時（詳情頁的那一筆），這裡退成一般文字：一頁一個 h1（DEV-156 U-31）。 */}
          <Title className="truncate text-2xl leading-tight font-bold tracking-[-0.01em]">
            {heading}
          </Title>
          <HomeLabel />
        </div>
        {subtitle ? <p className="truncate text-[13px] text-muted">{subtitle}</p> : null}
      </div>
      <button
        type="button"
        aria-label={t("app.search")}
        onClick={onSearch}
        className="flex size-11 shrink-0 items-center justify-center rounded-full text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      >
        <Search className="size-[22px]" strokeWidth={1.8} aria-hidden="true" />
      </button>
    </header>
  );
}
