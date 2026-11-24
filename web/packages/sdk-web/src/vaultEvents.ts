// 記錄變了的通知，給套件頁面用（PKG-49）：看板已經開著一條 bus 串流（vault.changed），收到就在瀏覽器裡轉一次，
// 套件頁面訂這個就好，不用自己再開串流（每頁多開一條會吃掉瀏覽器同一個網站的連線上限）。
// 例：秘書在旁邊的聊天裡幫你記了一餐（`evergenie data add nutrition.meal`），飲食頁馬上重讀，不用重新整理。

const EVENT = "evergenie:vault-changed";

/** 種類填這個＝「不知道漏了哪些，全部重讀」（看板的串流斷線重連時發）。 */
export const ALL_KINDS = "*";

export interface VaultChange {
  /** 完整的種類名稱，例如 `nutrition.meal`。 */
  kind: string;
  id?: string;
}

/** 看板收到 vault.changed 時呼叫（`data/live.ts`）。沒有 window（測試、伺服器）就什麼都不做。 */
export function emitVaultChanged(change: VaultChange): void {
  if (typeof window === "undefined" || !change.kind) return;
  window.dispatchEvent(new CustomEvent<VaultChange>(EVENT, { detail: change }));
}

/**
 * 訂某些種類的變化。prefix 是種類開頭（例如 `"nutrition."` 收這個套件全部的種類）；回傳取消訂閱的函式。
 * 同一個動作常常連發好幾筆（一次改好幾個記錄），呼叫端自己決定要不要去抖。
 */
export function onVaultChanged(prefix: string, fn: (change: VaultChange) => void): () => void {
  if (typeof window === "undefined") return () => {};
  const listener = (e: Event) => {
    const change = (e as CustomEvent<VaultChange>).detail;
    if (!change || typeof change.kind !== "string") return;
    // ALL_KINDS：看板的串流斷過又接上，斷掉時可能漏了變化，大家都重讀一次。
    if (change.kind === ALL_KINDS || change.kind.startsWith(prefix)) fn(change);
  };
  window.addEventListener(EVENT, listener);
  return () => window.removeEventListener(EVENT, listener);
}

/**
 * `onVaultChanged` 加去抖：一陣變化（例如秘書一次改好幾筆）只叫一次 fn，最後一筆之後等 debounceMs。
 * 套件頁面的載入 hook 用它重讀：`useEffect(() => onLiveChange("nutrition.", reload), [reload])`。
 */
export function onLiveChange(prefix: string, fn: () => void, debounceMs = 300): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const off = onVaultChanged(prefix, () => {
    clearTimeout(timer);
    timer = setTimeout(fn, debounceMs);
  });
  return () => {
    clearTimeout(timer);
    off();
  };
}
