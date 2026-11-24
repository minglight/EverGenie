import type { Job } from "@evergenie/sdk-web";
import { cn } from "@evergenie/ui";
import { CircleAlert } from "lucide-react";
import { toneText } from "../../lib/statusTone";
import { lastRunView } from "./jobs";

/**
 * 排程最近一次的那一行（清單每列、詳情的「最近一次」）：從沒跑過就什麼都不畫。
 * 沒跑成是紅字加驚嘆號，原因最多兩行（手機 390px 不撐高整列）；詳情頁 full 時不截。
 */
export function JobLastRun({ job, now, full }: { job: Job; now: Date; full?: boolean }) {
  const view = lastRunView(job, now);
  if (!view) return null;
  return (
    <span
      data-testid="job-last-run"
      data-tone={view.tone}
      className={cn(
        "flex items-start gap-1 break-words",
        full ? "text-sm" : "text-xs",
        toneText[view.tone],
        view.tone === "trouble" && "font-medium",
      )}
    >
      {view.tone === "trouble" ? (
        <CircleAlert className="mt-px size-3.5 shrink-0" aria-hidden="true" />
      ) : null}
      <span className={cn("min-w-0", !full && "line-clamp-2")}>{view.text}</span>
    </span>
  );
}
