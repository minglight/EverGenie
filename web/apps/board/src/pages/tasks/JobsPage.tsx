import { TasksSection } from "../../components/TasksSection";
import { t } from "../../i18n/zh-TW";
import { JobsPanel } from "./JobsPanel";

// 任務單 → 排程（/tasks/jobs，REQ-18）：排程也是秘書在做的事，只是有時間表，所以跟任務單放一起。
export function JobsPage() {
  return (
    <TasksSection tab="jobs">
      <p className="px-1 text-[13px] leading-snug text-muted">{t("jobs.intro")}</p>
      <JobsPanel />
    </TasksSection>
  );
}
