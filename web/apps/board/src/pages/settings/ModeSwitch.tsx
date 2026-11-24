import type { HomeMode } from "@evergenie/sdk-web";
import { Segmented } from "@evergenie/ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import { reasonOf } from "../../lib/reason";
import { switchMode, useMode } from "./mode";
import { notifyPrefsQuery } from "./queries";
import { SettingGroup, SettingRow } from "./SettingRow";

// 簡單／完整（D-9）：通知分頁最上面的分段按鈕，跟設定精靈的「介面」那一步是同一個值。
// 簡單切到完整時，核心會把預設組展開成明確的逐類設定（switchMode）。
export function ModeSwitch() {
  const client = useQueryClient();
  const { mode } = useMode();
  const desktop = useIsDesktop();
  const [error, setError] = useState("");
  const change = useMutation({
    mutationFn: (next: HomeMode) => switchMode(client, next),
    onMutate: () => setError(""),
    onError: (err) => setError(t("settings.mode.failed", { reason: reasonOf(err) })),
    onSettled: () => client.invalidateQueries({ queryKey: notifyPrefsQuery.queryKey }),
  });
  // 點了先顯示剛選的，伺服器回來再以伺服器的為準。
  const shown = change.isPending ? change.variables : (mode ?? "simple");
  return (
    <SettingGroup>
      <SettingRow title={t("settings.mode.title")} hint={t("settings.mode.hint")} error={error}>
        <Segmented
          options={[
            { value: "simple" as HomeMode, label: t("settings.mode.simple") },
            { value: "full" as HomeMode, label: t("settings.mode.full") },
          ]}
          value={shown}
          onChange={(next) => {
            if (next !== shown) change.mutate(next);
          }}
          ariaLabel={t("settings.mode.aria")}
          compact={desktop}
        />
      </SettingRow>
    </SettingGroup>
  );
}
