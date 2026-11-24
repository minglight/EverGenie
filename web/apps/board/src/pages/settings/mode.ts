import { api, type HomeMode } from "@evergenie/sdk-web";
import { type QueryClient, useQuery } from "@tanstack/react-query";
import { modeQuery } from "../../data/queries";
import { notifyPrefsQuery } from "./queries";

export interface ModeState {
  /** 還沒讀到是 undefined；畫面要等讀到再決定（不要先當成某一種閃一下）。 */
  mode: HomeMode | undefined;
  loaded: boolean;
}

// 通知的簡單／完整（2026-10-28 起只影響通知分頁）：外殼啟動時就讀 modeQuery()（Shell 呼叫一次），這裡共用同一份快取。
export function useMode(): ModeState {
  const { data } = useQuery(modeQuery());
  return { mode: data, loaded: data !== undefined };
}

// 切換模式。簡單切到完整時，先請核心把預設組展開成明確的逐類設定寫回（expand，notify-prefs.md「切換時展開」），
// 再改模式；展開失敗就不切。完整切到簡單不動通知偏好（逐類覆寫留著，簡單模式的通知頁會提醒）。
export async function switchMode(client: QueryClient, next: HomeMode): Promise<void> {
  if (next === "full") {
    const prefs = await client.fetchQuery({ ...notifyPrefsQuery, staleTime: 0 });
    if (prefs) {
      // 不送 tasks（[tasks] 原樣保留）和 presets（只讀）。
      const { tasks: _tasks, presets: _presets, ...rest } = prefs;
      const res = await api.notify.setPrefs({ prefs: rest, expand: true });
      if (res.prefs) client.setQueryData(notifyPrefsQuery.queryKey, res.prefs);
    }
  }
  await api.system.setMode({ mode: next });
  client.setQueryData(modeQuery().queryKey, next);
  await client.invalidateQueries({ queryKey: modeQuery().queryKey });
}
