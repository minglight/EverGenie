import type { DoctorCheck } from "@evergenie/sdk-web";
import { api } from "@evergenie/sdk-web";
import { cn } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { CircleAlert, CircleCheck, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { useShowCodes } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { SettingGroup } from "./SettingRow";

const groupKey = {
  config: "settings.doctor.group.config",
  sessions: "settings.doctor.group.sessions",
  packages: "settings.doctor.group.packages",
  permissions: "settings.doctor.group.permissions",
  schedule: "settings.doctor.group.schedule",
  data: "settings.doctor.group.data",
  push: "settings.doctor.group.push",
} as const;

function groupName(group: string | undefined): string {
  const key = groupKey[(group ?? "") as keyof typeof groupKey];
  return key ? t(key) : t("settings.doctor.group.other");
}

const rank = (c: DoctorCheck) => (c.severity === "error" ? 0 : c.severity === "warn" ? 1 : 2);

/** 有問題的排前面（要處理 → 注意 → 正常），同一種照原本的順序。 */
export function sortChecks(checks: DoctorCheck[]): DoctorCheck[] {
  return checks
    .map((c, i) => ({ c, i }))
    .sort((a, b) => rank(a.c) - rank(b.c) || a.i - b.i)
    .map((x) => x.c);
}

// 設定 → 系統健康的「完整檢查」（DEV-153，WEB-191）：按了才跑 system.doctor（要幾秒），有問題的排前面；
// 正常的收成一行、要看再展開。下一步（fix）多半是指令，只在完整模式顯示；簡單模式統一一句「跟任何一位秘書說」。
export function DoctorFull() {
  const [asked, setAsked] = useState(false);
  const [showOk, setShowOk] = useState(false);
  const showFix = useShowCodes();
  const query = useQuery({
    queryKey: ["doctor-full"],
    queryFn: async () => (await api.system.doctor({})).checks ?? [],
    enabled: asked,
    staleTime: 0,
    retry: false,
  });
  const run = () => {
    if (asked) void query.refetch();
    else setAsked(true);
  };
  const checks = query.data === undefined ? undefined : sortChecks(query.data);
  const problems = checks?.filter((c) => rank(c) < 2) ?? [];
  const fine = checks?.filter((c) => rank(c) === 2) ?? [];

  return (
    <SettingGroup title={t("settings.doctor.title")}>
      <div data-testid="doctor-full" className="flex flex-col items-start gap-3 px-4 py-3.5">
        <p className="text-[13px] leading-snug text-muted">{t("settings.doctor.intro")}</p>
        <ActionButton
          tone="outline"
          busy={query.isFetching}
          disabled={query.isFetching}
          data-testid="doctor-run"
          onClick={run}
        >
          {t(checks ? "settings.doctor.again" : "settings.doctor.run")}
        </ActionButton>
        {query.isError && !query.isFetching ? (
          <p role="alert" className="text-[13px] leading-snug text-danger">
            {t("settings.doctor.failed")}
          </p>
        ) : null}
      </div>
      {checks ? (
        <div data-testid="doctor-result" className="flex flex-col">
          <p
            data-testid="doctor-summary"
            className={cn(
              "px-4 py-3 text-[14px] leading-snug font-semibold",
              problems.length > 0 ? "text-fg" : "text-ok",
            )}
          >
            {problems.length > 0
              ? t("settings.doctor.summary.problems", { n: problems.length, ok: fine.length })
              : t("settings.doctor.summary.allOk", { ok: fine.length })}
          </p>
          {problems.map((c) => (
            <CheckRow key={`${c.id}:${c.detail}`} check={c} showFix={showFix} />
          ))}
          {problems.length > 0 ? (
            <p className="px-4 pb-3 text-[13px] leading-snug text-muted">
              {t("settings.health.fixHint")}
            </p>
          ) : null}
          {fine.length > 0 ? (
            <div className="border-t border-border">
              <button
                type="button"
                aria-expanded={showOk}
                data-testid="doctor-ok-toggle"
                onClick={() => setShowOk((v) => !v)}
                className="flex min-h-11 w-full items-center px-4 text-left text-[13px] font-medium text-muted"
              >
                {t(showOk ? "settings.doctor.hideOk" : "settings.doctor.showOk", {
                  n: fine.length,
                })}
              </button>
              {showOk
                ? fine.map((c) => (
                    <CheckRow key={`${c.id}:${c.detail}`} check={c} showFix={false} />
                  ))
                : null}
            </div>
          ) : null}
        </div>
      ) : null}
    </SettingGroup>
  );
}

function CheckRow({ check, showFix }: { check: DoctorCheck; showFix: boolean }) {
  const tone = check.severity === "error" ? "error" : check.severity === "warn" ? "warn" : "ok";
  const Icon = tone === "error" ? CircleAlert : tone === "warn" ? TriangleAlert : CircleCheck;
  const label =
    tone === "error"
      ? t("settings.doctor.sev.error")
      : tone === "warn"
        ? t("settings.doctor.sev.warn")
        : t("settings.health.ok");
  return (
    <div
      data-testid="doctor-check"
      data-severity={tone}
      className="flex items-start gap-3 px-4 py-2.5"
    >
      <Icon
        className={cn(
          "mt-0.5 size-5 shrink-0",
          tone === "error" ? "text-danger" : tone === "warn" ? "text-muted" : "text-ok",
        )}
        strokeWidth={1.9}
        aria-label={label}
      />
      <div className="min-w-0 flex-1">
        <p className="text-[12px] leading-snug text-muted">
          {groupName(check.group)}
          {tone === "ok" ? "" : `・${label}`}
        </p>
        <p
          className={cn(
            "text-[14px] leading-snug break-words",
            tone === "error" ? "font-medium text-danger" : "text-fg",
          )}
        >
          {check.detail}
        </p>
        {showFix && check.fix ? (
          <p className="mt-0.5 text-[12px] leading-snug break-words text-muted">
            {t("settings.doctor.fix", { fix: check.fix })}
          </p>
        ) : null}
      </div>
    </div>
  );
}
