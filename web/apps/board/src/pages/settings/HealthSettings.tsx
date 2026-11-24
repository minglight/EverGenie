import { Button, cn } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { CircleAlert, CircleCheck } from "lucide-react";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { t } from "../../i18n/zh-TW";
import { DoctorFull } from "./DoctorFull";
import { healthQuery } from "./queries";
import { ReportProblem } from "./ReportProblem";
import { SettingGroup } from "./SettingRow";
import { UpdateCard } from "./UpdateCard";

// 設定 → 系統健康：最上面是「版本與更新」（UpdateCard），下面是 web.get_health 的每一項（看板、核心服務、推播、各位常駐秘書），不正常的標紅；
// 再下面是按了才跑的「完整檢查」（DoctorFull）與「回報問題」（ReportProblem），DEV-153。
export function HealthSettings() {
  return (
    <div className="flex flex-col gap-4">
      <UpdateCard />
      <HealthItems />
      <DoctorFull />
      <ReportProblem />
    </div>
  );
}

function HealthItems() {
  const query = useQuery(healthQuery);
  if (query.data === undefined) {
    return query.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void query.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const items = query.data;
  return (
    <div className="flex flex-col gap-4">
      <SettingGroup>
        {items.length === 0 ? (
          <p className="px-4 py-3.5 text-[13px] text-muted">{t("settings.health.empty")}</p>
        ) : (
          items.map((item) => {
            const Icon = item.ok ? CircleCheck : CircleAlert;
            return (
              <div
                key={item.name}
                data-testid="health-item"
                data-ok={item.ok}
                className="flex items-start gap-3 px-4 py-3.5"
              >
                <Icon
                  className={cn("mt-0.5 size-5 shrink-0", item.ok ? "text-ok" : "text-danger")}
                  strokeWidth={1.9}
                  aria-label={t(item.ok ? "settings.health.ok" : "settings.health.bad")}
                />
                <div className="min-w-0 flex-1">
                  <p className="text-[15px] leading-snug font-semibold">{item.name}</p>
                  {item.detail ? (
                    <p
                      className={cn(
                        "mt-0.5 text-[13px] leading-snug",
                        item.ok ? "text-muted" : "font-medium text-danger",
                      )}
                    >
                      {item.detail}
                    </p>
                  ) : null}
                  {item.ok ? null : (
                    <p className="mt-0.5 text-[13px] leading-snug text-muted">
                      {t("settings.health.fixHint")}
                    </p>
                  )}
                </div>
              </div>
            );
          })
        )}
      </SettingGroup>
      <div className="flex flex-col items-start gap-2">
        <Button onClick={() => void query.refetch()} disabled={query.isFetching}>
          {t("settings.health.recheck")}
        </Button>
        <p data-testid="health-doctor-hint" className="text-[12px] leading-snug text-muted">
          {t("settings.health.doctorHint")}
        </p>
      </div>
    </div>
  );
}
