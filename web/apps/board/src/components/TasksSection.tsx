import type { ReactNode } from "react";
import { t } from "../i18n/zh-TW";
import { useIsDesktop } from "../lib/media";
import { PageTabs } from "./PageTabs";

export type TasksTab = "tasks" | "goals" | "jobs";

const tabs = [
  { id: "tasks", to: "/tasks", label: "tasks.tab.tasks" },
  { id: "jobs", to: "/tasks/jobs", label: "tasks.tab.jobs" },
  { id: "goals", to: "/goals", label: "tasks.tab.goals" },
] as const;

// 頁內的分頁「任務單｜排程｜Goal」：各自一個網址（/tasks、/goals、/tasks/jobs），是導覽不是篩選，
// 所以畫成底線分頁，跟下面「未結／已結束」的分段按鈕（Segmented）分得出層級。
// Goal 一律在這裡、排最後（作者 2026-10-28：Goal 直接放在任務單，不再看簡單／完整模式，REQ-237；不放側欄選單）。
function TasksTabs({ tab }: { tab: TasksTab }) {
  return (
    <PageTabs
      label={t("tasks.tabs")}
      tabs={tabs.map((x) => ({ key: x.id, to: x.to, label: t(x.label), current: x.id === tab }))}
    />
  );
}

// 任務單、Goal、排程共用的外框：同一個寬度與留白、桌機的標題、頂端的頁內分頁，換分頁時位置不跳。
export function TasksSection({ tab, children }: { tab: TasksTab; children: ReactNode }) {
  const desktop = useIsDesktop();
  return (
    <div className="mx-auto flex max-w-[720px] flex-col gap-3.5 px-4 pt-3 pb-7">
      {desktop ? (
        <h1 className="pt-6 text-2xl font-bold tracking-[-0.01em]">{t("page.tasks")}</h1>
      ) : null}
      <TasksTabs tab={tab} />
      {children}
    </div>
  );
}
