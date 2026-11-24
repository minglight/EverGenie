import { api, type PermissionLevel } from "@evergenie/sdk-web";
import { Avatar, Segmented } from "@evergenie/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { useAgents } from "../../data/agents";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import { reasonOf } from "../../lib/reason";
import { DefaultLevelChoices } from "./DefaultLevelChoices";
import { INHERIT, levelName, levelShort, ownLevel } from "./levels";
import { systemTrustQuery } from "./queries";
import { SettingGroup } from "./SettingRow";

type Choice = PermissionLevel | "inherit";

// 設定 → 權限 上半：整個家的預設等級（三個白話選項，2026-10-28 從設定精靈搬來），加每位秘書自己的等級（REQ-110）。改了馬上送，
// 「跟預設」＝拿掉單獨設定（level 送空字串）。秘書下次開對話才生效。
export function TrustLevels() {
  const query = useQuery(systemTrustQuery);
  const client = useQueryClient();
  const desktop = useIsDesktop();
  const { view } = useAgents();
  // 送出到回來之前先顯示剛選的；失敗就還原。
  const [pending, setPending] = useState<Record<string, Choice>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});

  const save = useMutation({
    mutationFn: async ({ agent, choice }: { agent: string; choice: Choice }) =>
      api.system.setPermissionLevels({
        levels: [{ agent, level: choice === "inherit" ? INHERIT : choice }],
      }),
    onMutate: ({ agent, choice }) => {
      setPending((p) => ({ ...p, [agent]: choice }));
      setErrors((e) => ({ ...e, [agent]: "" }));
    },
    onError: (err, { agent }) => {
      setErrors((e) => ({
        ...e,
        [agent]: t("settings.trust.levels.failed", { reason: reasonOf(err) }),
      }));
    },
    onSettled: async (_data, _err, { agent }) => {
      await client.invalidateQueries({ queryKey: systemTrustQuery.queryKey });
      setPending((p) => {
        const { [agent]: _drop, ...rest } = p;
        return rest;
      });
    },
  });

  if (query.data === undefined) {
    return query.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void query.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const { default_level: defaultLevel, agents = [] } = query.data;
  const options: { value: Choice; label: string }[] = [
    ...(["full", "standard", "readonly"] as const).map((l) => ({
      value: l as Choice,
      label: t(levelShort[l]),
    })),
    { value: "inherit", label: t("settings.level.short.inherit") },
  ];

  return (
    <SettingGroup title={t("settings.trust.levels")}>
      <div className="flex flex-col gap-2.5 px-4 py-3.5">
        <p className="text-[14px] leading-snug" data-testid="trust-default-level">
          {t("settings.trust.levels.default", {
            level: defaultLevel ? t(levelName[defaultLevel]) : "—",
          })}
        </p>
        <DefaultLevelChoices current={defaultLevel} />
      </div>
      {agents.length === 0 ? (
        <p className="px-4 py-3.5 text-[13px] text-muted">{t("settings.trust.levels.empty")}</p>
      ) : (
        agents.map((a) => {
          const id = a.agent ?? "";
          const v = view(id);
          const value = pending[id] ?? ownLevel(a.source, a.level);
          return (
            <div
              key={id}
              data-testid="trust-level-row"
              className="flex flex-col gap-2.5 px-4 py-3.5 md:flex-row md:items-center md:justify-between"
            >
              <div className="flex min-w-0 items-center gap-3">
                <Avatar
                  name={v.name}
                  color={v.color}
                  colorDark={v.colorDark}
                  src={v.avatarUrl}
                  size={32}
                />
                <p className="min-w-0 truncate text-[15px] font-semibold">
                  {v.name === t("agent.unknown") ? id : v.name}
                </p>
              </div>
              <div className="min-w-0 md:w-[320px] md:shrink-0">
                <Segmented
                  options={options}
                  value={value}
                  onChange={(choice) => save.mutate({ agent: id, choice })}
                  ariaLabel={t("settings.trust.levels.agent", {
                    name: v.name === t("agent.unknown") ? id : v.name,
                  })}
                  compact={desktop}
                  wrap
                />
                {errors[id] ? (
                  <p role="alert" className="mt-2 text-xs leading-snug text-danger">
                    {errors[id]}
                  </p>
                ) : null}
              </div>
            </div>
          );
        })
      )}
      <p className="px-4 py-3 text-[13px] text-muted">{t("settings.trust.levels.footer")}</p>
    </SettingGroup>
  );
}
