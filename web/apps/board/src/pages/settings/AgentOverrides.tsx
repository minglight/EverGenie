import { Segmented } from "@evergenie/ui";
import { useState } from "react";
import { useAgents } from "../../data/agents";
import { type TextKey, t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import {
  type AgentMode,
  agentModeOf,
  agentModesFor,
  agentOverrideCount,
  categoryIds,
  withAgentMode,
} from "./prefs";
import { SettingGroup, SettingRow } from "./SettingRow";
import type { AutosavedPrefs } from "./useAutosavedPrefs";

const inputClass =
  "h-11 w-full rounded-[10px] border border-border bg-bg px-3 text-base text-fg focus-visible:outline-2 focus-visible:outline-focus";

// 完整模式：單一秘書另設。選一位秘書，每個分類可以「跟全域一樣」或另外指定送法（REQ-25）；
// 「要你同意」最低只能到「只放看板」（REQ-26，這裡不擺「關閉」，核心也會擋）。
export function AgentOverrides({
  auto,
}: {
  auto: AutosavedPrefs & { prefs: NonNullable<AutosavedPrefs["prefs"]> };
}) {
  const { all: list } = useAgents();
  const desktop = useIsDesktop();
  const [agent, setAgent] = useState("");
  const { prefs } = auto;
  const name = list.find((a) => a.id === agent)?.name ?? agent;

  return (
    <SettingGroup title={t("settings.notify.agents")}>
      <SettingRow title={t("settings.notify.agents.pick")} hint={t("settings.notify.agents.hint")}>
        <select
          aria-label={t("settings.notify.agents.pick")}
          data-testid="agent-override-pick"
          value={agent}
          onChange={(e) => setAgent(e.target.value)}
          className={inputClass}
        >
          <option value="">{t("settings.notify.agents.choose")}</option>
          {list.map((a) => {
            const n = agentOverrideCount(prefs, a.id ?? "");
            return (
              <option key={a.id} value={a.id}>
                {a.name || a.id}
                {n > 0 ? t("settings.notify.agents.count", { n }) : ""}
              </option>
            );
          })}
        </select>
      </SettingRow>
      {agent === "" ? (
        <p className="px-4 py-3.5 text-[13px] text-muted">{t("settings.notify.agents.none")}</p>
      ) : (
        categoryIds.map((category) => {
          const catName = t(`settings.notify.cat.${category}` as TextKey);
          return (
            <SettingRow
              key={category}
              title={catName}
              error={auto.errors[`agent:${agent}:${category}`]}
              wide
            >
              <Segmented<AgentMode>
                options={agentModesFor(category).map((m) => ({
                  value: m,
                  label: t(
                    (m === "same"
                      ? "settings.notify.agents.same"
                      : `settings.notify.mode.${m}`) as TextKey,
                  ),
                }))}
                value={agentModeOf(prefs, agent, category)}
                onChange={(next) =>
                  auto.edit(`agent:${agent}:${category}`, (p) =>
                    withAgentMode(p, agent, category, next),
                  )
                }
                ariaLabel={t("settings.notify.agents.mode", { who: name, name: catName })}
                compact={desktop}
                wrap
                className="md:min-w-[280px]"
              />
            </SettingRow>
          );
        })
      )}
    </SettingGroup>
  );
}
