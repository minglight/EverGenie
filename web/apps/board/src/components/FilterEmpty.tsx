import { useAgentFilter, usePickedNames } from "../data/agentFilter";
import { t } from "../i18n/zh-TW";

// 篩選之後是 0：說是「這幾位」在這裡沒有東西，不顯示原本「這裡是空的」那句（會以為資料不見）。
export function FilterEmpty({ variant = "here" }: { variant?: "here" | "today" }) {
  const { picked, clear } = useAgentFilter();
  const names = usePickedNames(picked);
  return (
    <div
      role="status"
      data-testid="filter-empty"
      className="flex flex-col items-center gap-1 rounded-[18px] border border-dashed border-border-strong px-4 py-[22px] text-center"
    >
      <div className="text-[15px] font-semibold">
        {t(variant === "today" ? "filter.empty.today" : "filter.empty.here", { names })}
      </div>
      <button
        type="button"
        onClick={clear}
        className="flex min-h-11 items-center px-3 text-sm font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
      >
        {t("filter.showAll")}
      </button>
    </div>
  );
}
