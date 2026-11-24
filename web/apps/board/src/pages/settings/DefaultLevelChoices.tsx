import { api, type PermissionLevel } from "@evergenie/sdk-web";
import { cn } from "@evergenie/ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check } from "lucide-react";
import { useState } from "react";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { wizardChoices } from "./levels";
import { systemTrustQuery } from "./queries";

// 整個家的預設權限：三個白話選項（逐字照命令列精靈，REQ-158），點了馬上存。
// 2026-10-28 從設定精靈搬到「權限」分頁（REQ-238：精靈只問職位與角色，權限照預設）。
export function DefaultLevelChoices({ current }: { current: PermissionLevel | undefined }) {
  const client = useQueryClient();
  const [error, setError] = useState("");
  const choose = useMutation({
    mutationFn: (level: PermissionLevel) =>
      api.system.setPermissionLevels({ default_level: level }),
    onMutate: () => setError(""),
    onError: (err) => setError(t("settings.setup.saveFailed", { reason: reasonOf(err) })),
    onSettled: () => client.invalidateQueries({ queryKey: systemTrustQuery.queryKey }),
  });
  // 點了先顯示剛選的，伺服器回來再以伺服器的為準。
  const level = choose.isPending ? choose.variables : current;
  return (
    <>
      <fieldset
        data-testid="trust-default-choices"
        className="m-0 flex flex-col gap-2 border-0 p-0"
      >
        <legend className="sr-only">{t("settings.setup.choice.group")}</legend>
        {wizardChoices.map((c) => {
          const on = level === c.level;
          return (
            <button
              key={c.level}
              type="button"
              aria-pressed={on}
              data-level={c.level}
              onClick={() => choose.mutate(c.level)}
              className={cn(
                "flex min-h-11 items-start gap-3 rounded-xl border px-3.5 py-3 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
                on ? "border-accent bg-accent-soft" : "border-border bg-bg hover:bg-hover",
              )}
            >
              <span
                aria-hidden="true"
                className={cn(
                  "mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full border",
                  on ? "border-accent bg-accent text-accent-fg" : "border-border-strong",
                )}
              >
                {on ? <Check className="size-3.5" strokeWidth={3} /> : null}
              </span>
              <span className="min-w-0">
                <span className="block text-[15px] leading-snug font-semibold">{t(c.title)}</span>
                <span className="mt-0.5 block text-[13px] leading-snug text-muted">
                  {t(c.explain)}
                </span>
              </span>
            </button>
          );
        })}
      </fieldset>
      {error ? (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
    </>
  );
}
