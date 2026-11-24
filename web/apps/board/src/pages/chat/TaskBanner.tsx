import { cn } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";
import { RowCode } from "../../components/RowCode";
import { taskQuery } from "../../data/queries";
import { homeSearch, PRIMARY, type SiteRef } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { isDelivery, statusText } from "../../lib/taskText";

const cls =
  "flex shrink-0 items-start gap-3 border-b border-border bg-surface-2 px-4 py-2 hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus";

interface Row {
  status: string;
  /** 要你動手（要你驗收、要你回答）＝橘色，其他灰（UXC-12，DEV-156 U-10）。 */
  waiting: boolean;
  title: string;
  note: string;
}

// 工作 session（對應一張任務單）的討論串頂端：單號、標題、目前狀態；要你驗收或決定時帶一行秘書留的話。
// 點整列到任務單。
// 別台的工作 session：單在那台，讀那台（S3-6）；連結帶 ?home= 打開那台的單（S3-7b）。
export function TaskBanner({ id, site = PRIMARY }: { id: string; site?: SiteRef }) {
  const { data: task } = useQuery(taskQuery(id, site));
  if (!task) return null;
  const note = isDelivery(task) ? task.result : task.status === "waiting_user" ? task.ask : "";
  const row = {
    status: statusText(task),
    waiting: task.status === "waiting_user",
    title: task.title ?? "",
    note: note ?? "",
  };
  return (
    <Link
      to="/tasks/$id"
      params={{ id }}
      search={homeSearch(site)}
      data-testid="task-banner"
      data-site={site.key || undefined}
      aria-label={t("chat.task.open", { id })}
      className={cls}
    >
      <BannerBody id={id} row={row} />
    </Link>
  );
}

function BannerBody({ id, row }: { id: string; row: Row }) {
  return (
    <>
      <span
        data-testid="task-banner-status"
        className={cn(
          "mt-0.5 shrink-0 rounded-md px-2 py-0.5 text-[11px] font-semibold",
          row.waiting ? "bg-attention-soft text-attention-fg" : "bg-surface text-muted",
        )}
      >
        {row.status}
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5 text-sm">
        <span className="truncate">
          <RowCode id={id} className="font-normal" />
          {row.title}
        </span>
        {row.note ? <span className="line-clamp-2 text-xs text-muted">{row.note}</span> : null}
      </span>
      <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted" aria-hidden="true" />
    </>
  );
}
