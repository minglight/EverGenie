import { cn, showToast } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { refreshAll } from "../data/live";
import { apiFor, PRIMARY, type SiteRef } from "../data/sites";
import { t } from "../i18n/zh-TW";

/** 刪除任務單（封存，不刪檔）：兩段式確認，第一下變「確定刪除？」，3 秒內再按才刪。 */
export function DeleteTask({
  id,
  onDeleted,
  site = PRIMARY,
}: {
  id: string;
  onDeleted: () => void;
  /** 那張單在哪台（預設主伺服器）。 */
  site?: SiteRef;
}) {
  const client = useQueryClient();
  const [armed, setArmed] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  const press = async () => {
    clearTimeout(timer.current);
    if (!armed) {
      setArmed(true);
      timer.current = setTimeout(() => setArmed(false), 3000);
      return;
    }
    setArmed(false);
    try {
      await apiFor(site).tasks.archive({ ids: [id] });
      showToast(t("task.deleted", { id }));
      await refreshAll(client, site);
      onDeleted();
    } catch {
      showToast(t("task.delete.failed"));
    }
  };

  return (
    <button
      type="button"
      onClick={() => void press()}
      aria-label={armed ? t("messages.delete.confirm") : t("task.delete", { id })}
      className={cn(
        "inline-flex min-h-11 items-center gap-1.5 self-start rounded-full px-3.5 text-[13px] font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-focus",
        armed ? "bg-danger text-accent-fg" : "text-muted hover:bg-danger-soft hover:text-danger",
      )}
    >
      {armed ? (
        t("messages.delete.confirm")
      ) : (
        <>
          <Trash2 className="size-[18px]" strokeWidth={1.8} aria-hidden="true" />
          {t("messages.delete")}
        </>
      )}
    </button>
  );
}
