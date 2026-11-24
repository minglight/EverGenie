import { api } from "@evergenie/sdk-web";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Minus, Plus } from "lucide-react";
import { useState } from "react";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { workMaxQuery } from "./queries";
import { SettingGroup, SettingRow } from "./SettingRow";

const stepClass =
  "flex size-11 items-center justify-center rounded-full border border-border bg-bg text-fg hover:bg-hover disabled:opacity-40 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";

/**
 * 設定 → 職位與秘書 → 同時進行的對話（WEB-186、SES-73）：settings/sessions.toml 的 work_max。
 * 一按就存（跟其他設定一樣不用按儲存）；先顯示剛按的，伺服器回來以伺服器為準，被拒絕就退回並寫原因。
 * 預設值、範圍都由伺服器給（只在 internal/base/session 定一次）。簡單模式也看得到。
 */
export function WorkMaxSetting() {
  const client = useQueryClient();
  const q = useQuery(workMaxQuery);
  const [error, setError] = useState("");
  const change = useMutation({
    mutationFn: (next: number) => api.system.setWorkMax({ work_max: next }),
    onMutate: () => setError(""),
    onError: (err) => setError(t("settings.workMax.failed", { reason: reasonOf(err) })),
    onSettled: () => client.invalidateQueries({ queryKey: workMaxQuery.queryKey }),
  });
  const body = (() => {
    const { work_max, default: dflt, min, max } = q.data ?? {};
    // 數字都由伺服器給（不在畫面另寫一份預設）；缺了就當讀不到。
    if (work_max === undefined || dflt === undefined || min === undefined || max === undefined) {
      return q.isError || q.data ? (
        <LoadFailed what={t("settings.loadFailed")} onRetry={() => void q.refetch()} />
      ) : (
        <LoadingBlock />
      );
    }
    const shown = change.isPending ? change.variables : work_max;
    const isDefault = shown === dflt;
    const step = (d: number) => {
      // 手改檔案寫了範圍外的值：往回拉到範圍內。
      const next = Math.min(Math.max(shown + d, min), max);
      if (next !== shown) change.mutate(next);
    };
    return (
      <SettingRow
        title={t("settings.workMax.title")}
        hint={t("settings.workMax.hint", { n: dflt })}
        error={error}
        extra={
          <p className="mt-2 text-[13px] leading-snug text-muted">
            {t("settings.workMax.range", { min, max })}
          </p>
        }
      >
        <div className="flex items-center gap-3" data-testid="work-max">
          <button
            type="button"
            className={stepClass}
            aria-label={t("settings.workMax.less")}
            disabled={shown <= min}
            onClick={() => step(-1)}
          >
            <Minus className="size-5" aria-hidden="true" />
          </button>
          <output
            data-testid="work-max-value"
            aria-live="polite"
            aria-label={t("settings.workMax.aria", { n: shown })}
            className="min-w-[4.5rem] text-center text-[17px] font-semibold tabular-nums"
          >
            {t("settings.workMax.value", { n: shown })}
            {isDefault ? (
              <span className="block text-[12px] font-normal text-muted">
                {t("settings.workMax.default")}
              </span>
            ) : null}
          </output>
          <button
            type="button"
            className={stepClass}
            aria-label={t("settings.workMax.more")}
            disabled={shown >= max}
            onClick={() => step(1)}
          >
            <Plus className="size-5" aria-hidden="true" />
          </button>
        </div>
      </SettingRow>
    );
  })();
  return <SettingGroup title={t("settings.workMax.group")}>{body}</SettingGroup>;
}
