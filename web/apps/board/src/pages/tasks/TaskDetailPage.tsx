import { useParams } from "@tanstack/react-router";
import { NudgeCard } from "../../components/NudgeCard";
import { SiteGate } from "../../components/SiteGate";
import { TaskView } from "../../components/TaskView";
import { t } from "../../i18n/zh-TW";

export function TaskDetailPage() {
  const { id } = useParams({ strict: false }) as { id: string };
  return (
    <div className="pb-2">
      {/* ?home= 指的那台（沒寫＝主伺服器，跟 S3 之前一樣；S3-7b）。「還在等你」的提醒照網址 ?home 畫那台的（S3-8 useViewedHome）。 */}
      <SiteGate back={{ to: "/tasks", label: t("task.back") }}>
        {(site) => (
          <>
            <div className="mx-auto max-w-[720px] px-4 empty:hidden">
              <NudgeCard task={id} />
            </div>
            <TaskView key={`${site.key}/${id}`} id={id} site={site} />
          </>
        )}
      </SiteGate>
    </div>
  );
}
