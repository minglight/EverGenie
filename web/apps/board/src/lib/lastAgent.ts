// 「上一次開任務單給誰」：開單時沒指定、也沒選秘書，就帶上一位（作者 2026-10-28）。
// 存 localStorage 只是方便；讀寫失敗就當沒有。
const KEY = "eg.lastTaskAgent";

export function readLastTaskAgent(): string {
  try {
    return window.localStorage.getItem(KEY) ?? "";
  } catch {
    return "";
  }
}

export function writeLastTaskAgent(id: string) {
  try {
    if (id) window.localStorage.setItem(KEY, id);
  } catch {
    // 存不了就算了。
  }
}

/** 上一位還在名冊裡（而且啟用）才採用；不在了就當沒有。 */
export function lastTaskAgentIn(enabledIds: readonly string[]): string {
  const id = readLastTaskAgent();
  return id && enabledIds.includes(id) ? id : "";
}
