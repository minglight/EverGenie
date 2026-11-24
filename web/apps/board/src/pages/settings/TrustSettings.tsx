import type { PermissionPolicy } from "@evergenie/sdk-web";
import { useQuery } from "@tanstack/react-query";
import { Pill } from "../../components/Pill";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { useAgents } from "../../data/agents";
import { t } from "../../i18n/zh-TW";
import { trustQuery } from "./queries";
import { SettingGroup } from "./SettingRow";
import { TrustLevels } from "./TrustLevels";

function ActionChips({ actions, empty }: { actions: string[]; empty: string }) {
  if (actions.length === 0) return <p className="text-[13px] text-muted">{empty}</p>;
  return (
    <ul className="flex flex-wrap gap-1.5">
      {actions.map((a) => (
        <li
          key={a}
          className="rounded-md bg-surface-2 px-2 py-0.5 font-mono text-[13px] break-all text-fg"
        >
          {a}
        </li>
      ))}
    </ul>
  );
}

function Repos({ policy }: { policy: PermissionPolicy }) {
  const { view } = useAgents();
  const repos = policy.repos ?? [];
  return (
    <SettingGroup title={t("settings.trust.repos")}>
      {repos.length === 0 ? (
        <p className="px-4 py-3.5 text-[13px] text-muted">{t("settings.trust.repos.empty")}</p>
      ) : (
        repos.map((r) => (
          <div key={r.path} className="flex flex-col gap-2 px-4 py-3.5" data-testid="trust-repo">
            <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5">
              <p className="min-w-0 font-mono text-[14px] font-semibold break-all">{r.path}</p>
              <Pill tone={r.push ? "attention" : "muted"}>
                {t(r.push ? "settings.trust.push.yes" : "settings.trust.push.no")}
              </Pill>
            </div>
            {r.remote ? (
              <p className="text-[13px] text-muted">
                {t("settings.trust.remote")}：
                <span className="font-mono break-all">{r.remote}</span>
              </p>
            ) : null}
            <div className="flex flex-wrap items-center gap-1.5 text-[13px]">
              <span className="text-muted">{t("settings.trust.writers")}：</span>
              {(r.writers ?? []).length === 0 ? (
                <span className="text-muted">{t("settings.trust.writers.empty")}</span>
              ) : (
                (r.writers ?? []).map((id) => (
                  <Pill key={id} tone="accent">
                    {view(id).name === t("agent.unknown") ? id : view(id).name}
                  </Pill>
                ))
              )}
            </div>
          </div>
        ))
      )}
    </SettingGroup>
  );
}

// 設定 → 權限（唯讀）：可寫的資料夾（誰能寫用人名）、核准的動作、不開放的動作。
export function TrustSettings() {
  const query = useQuery(trustQuery);
  if (query.data === undefined) {
    return query.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void query.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const policy = query.data;
  return (
    <div className="flex flex-col gap-5">
      <TrustLevels />
      <Repos policy={policy} />
      <SettingGroup title={t("settings.trust.allow")}>
        <div className="flex flex-col gap-2 px-4 py-3.5">
          <p className="text-[13px] text-muted">{t("settings.trust.allow.hint")}</p>
          <ActionChips actions={policy.allow ?? []} empty={t("settings.trust.allow.empty")} />
        </div>
      </SettingGroup>
      <SettingGroup title={t("settings.trust.deny")}>
        <div className="flex flex-col gap-2 px-4 py-3.5">
          <p className="text-[13px] text-muted">{t("settings.trust.deny.hint")}</p>
          <ActionChips actions={policy.deny ?? []} empty={t("settings.trust.deny.empty")} />
        </div>
      </SettingGroup>
      <p className="px-1 text-[13px] leading-relaxed text-muted">{t("settings.trust.footer")}</p>
    </div>
  );
}
