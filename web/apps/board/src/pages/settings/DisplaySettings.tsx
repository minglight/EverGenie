import { api, type ChatDisplay } from "@evergenie/sdk-web";
import { Segmented } from "@evergenie/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { chatDisplayQuery } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import { reasonOf } from "../../lib/reason";
import { SettingGroup, SettingRow } from "./SettingRow";

// 對話的顯示方式（WEB-160）：預設不露工具細節，工程師顯示；存在家裡（home.toml chat_display），換裝置不會跑掉。
export function DisplaySettings() {
  const client = useQueryClient();
  const { data } = useQuery(chatDisplayQuery());
  const desktop = useIsDesktop();
  const [error, setError] = useState("");
  const change = useMutation({
    mutationFn: (next: ChatDisplay) => api.system.setChatDisplay({ chat_display: next }),
    onMutate: () => setError(""),
    onSuccess: (res, next) =>
      client.setQueryData(chatDisplayQuery().queryKey, res.chat_display ?? next),
    onError: (err) => setError(t("settings.chatDisplay.failed", { reason: reasonOf(err) })),
  });
  // 點了先顯示剛選的，伺服器回來再以伺服器的為準。
  const shown = change.isPending ? change.variables : (data ?? "default");
  return (
    <SettingGroup>
      <SettingRow
        title={t("settings.chatDisplay.title")}
        hint={t("settings.chatDisplay.hint")}
        error={error}
      >
        <Segmented
          options={[
            { value: "default" as ChatDisplay, label: t("settings.chatDisplay.default") },
            { value: "engineer" as ChatDisplay, label: t("settings.chatDisplay.engineer") },
          ]}
          value={shown}
          onChange={(next) => {
            if (next !== shown) change.mutate(next);
          }}
          ariaLabel={t("settings.chatDisplay.aria")}
          compact={desktop}
        />
      </SettingRow>
    </SettingGroup>
  );
}
