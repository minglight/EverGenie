import { useEffect, useMemo } from "react";
import { useChatAgents, useGoToAgent } from "./chatAgents";

// 聊天頁（桌機）的 ⌥1–⌥9：直接跳到第 N 位秘書的討論串，打字中也可以用。
// 標題欄右邊原本那排頭像（AgentRail）2026-11-02 拿掉（作者：篩選改成一次一位之後就不需要了）；換人用篩選或標題的人名鈕。

/** 快捷鍵最多幾位（⌥1–⌥9）。 */
export const HOTKEY_AGENTS = 9;

/** ⌥＋數字鍵 → 第幾位（0 起算）；其他按法回 null。用 code 判斷，Mac 的 ⌥1 打出來的字是「¡」。 */
export function hotkeyIndex(e: {
  altKey: boolean;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  code: string;
}): number | null {
  if (!e.altKey || e.metaKey || e.ctrlKey || e.shiftKey) return null;
  const m = /^Digit([1-9])$/.exec(e.code);
  return m ? Number(m[1]) - 1 : null;
}

/** 啟用中的秘書（每台照名冊順序，主伺服器在前），第 N 位對到 ⌥N；跟「換一位秘書」同一個目的地（REQ-200）。 */
export function useAgentHotkeys(enabled: boolean) {
  const { agents: all } = useChatAgents();
  const agents = useMemo(() => all.filter((a) => a.enabled).slice(0, HOTKEY_AGENTS), [all]);
  const go = useGoToAgent();
  useEffect(() => {
    if (!enabled) return;
    const onKey = (e: KeyboardEvent) => {
      const at = hotkeyIndex(e);
      if (at === null) return;
      const agent = agents[at];
      if (!agent) return;
      e.preventDefault();
      go(agent);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [enabled, agents, go]);
}
