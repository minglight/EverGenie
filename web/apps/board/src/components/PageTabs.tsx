import { cn } from "@evergenie/ui";
import { Link } from "@tanstack/react-router";
import { type ReactNode, useEffect, useRef } from "react";

export interface PageTab {
  key: string;
  /** 網址（伺服器給的字串也可以，所以這裡放寬 Router 的型別檢查）。 */
  to: string;
  label: string;
  current: boolean;
  /** 網址查詢（例：角色頁的 `?tab=`）；不給就照 `to`。 */
  search?: Record<string, unknown>;
  /** true：換分頁不多疊一筆歷史（角色頁的分頁，上一頁直接回清單）。 */
  replace?: boolean;
}

/** 分頁的樣子；不是網址的分頁（例：健康頁窄螢幕的「聊天」）也用它，看起來才是同一排。 */
export function tabClass(current: boolean): string {
  return cn(
    // 不折行、不縮：分頁多（角色頁加了套件的分頁）放不下時，是那一排自己橫向捲（DEV-156）。
    "flex min-h-11 flex-none items-center border-b-2 px-3 text-[15px] font-semibold whitespace-nowrap transition-colors focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus",
    current ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg",
  );
}

// 頁內的底線分頁：每個分頁各自一個網址，是導覽不是篩選，所以畫成底線分頁，
// 跟頁面裡的分段按鈕（Segmented）分得出層級（任務單｜Goal｜排程、健康的飲食｜運動｜身體）。
// children 接在分頁後面（同一排的非網址分頁）；trailing 靠右（例：「項目」開關）。
// 分頁不折行；放不下時這一排自己橫向捲、目前那個分頁捲進畫面。
export function PageTabs({
  label,
  tabs,
  onPick,
  children,
  trailing,
}: {
  label: string;
  tabs: PageTab[];
  /** 點了任何一個網址分頁（含目前這頁）。 */
  onPick?: () => void;
  children?: ReactNode;
  trailing?: ReactNode;
}) {
  // 目前那個分頁被捲到外面時（直接開到後面的分頁）捲進畫面；只動這一排，不動頁面。
  const ref = useRef<HTMLElement>(null);
  const current = tabs.find((x) => x.current)?.key;
  // biome-ignore lint/correctness/useExhaustiveDependencies: 換了目前的分頁要重新對一次
  useEffect(() => {
    const el = ref.current;
    const on = el?.querySelector<HTMLElement>('[aria-current="page"]');
    if (!el || !on) return;
    const start = on.offsetLeft;
    const end = start + on.offsetWidth;
    if (start < el.scrollLeft) el.scrollLeft = Math.max(0, start - 8);
    else if (end > el.scrollLeft + el.clientWidth) el.scrollLeft = end - el.clientWidth + 8;
  }, [current]);
  return (
    <nav
      ref={ref}
      aria-label={label}
      data-testid="page-tabs"
      // 放不下就這一排自己橫向捲，頁面不捲（手機上五個分頁時字不會折成兩行）。
      className="hg-scroll relative flex min-w-0 items-end gap-1 overflow-x-auto overflow-y-hidden border-b border-border"
    >
      {tabs.map((x) => (
        <Link
          key={x.key}
          to={x.to as "/"}
          search={x.search as never}
          replace={x.replace}
          // Link 預設把子路徑也算「目前」；這裡由呼叫的人決定哪一個是目前。
          activeOptions={{ exact: true, includeSearch: false }}
          aria-current={x.current ? "page" : undefined}
          onClick={onPick}
          className={tabClass(x.current)}
        >
          {x.label}
        </Link>
      ))}
      {children}
      {trailing ? (
        <div className="mb-0.5 ml-auto flex flex-none items-center">{trailing}</div>
      ) : null}
    </nav>
  );
}
