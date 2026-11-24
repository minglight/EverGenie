/** 桌機「今天」清單的單鍵捷徑（J／K 上下、A 主要動作、R 次要動作）。 */
export type TodayKey = "primary" | "secondary" | "down" | "up";

type KeyLike = {
  key: string;
  repeat?: boolean;
  metaKey?: boolean;
  ctrlKey?: boolean;
  altKey?: boolean;
};

/**
 * 把按鍵翻成「今天」清單的動作；不是捷徑回 null。
 * 按住 A／R 不放時瀏覽器會連發（repeat）：動作做完選取會跳到下一件，連發會一路核准／拒絕好幾件，所以只認第一下（WEB-123）。
 * J／K 連發照常移動。
 */
export function todayKeyAction(e: KeyLike): TodayKey | null {
  if (e.metaKey || e.ctrlKey || e.altKey) return null;
  const k = e.key;
  if (k === "a" || k === "A") return e.repeat ? null : "primary";
  if (k === "r" || k === "R") return e.repeat ? null : "secondary";
  if (k === "j" || k === "ArrowDown") return "down";
  if (k === "k" || k === "ArrowUp") return "up";
  return null;
}

/**
 * 焦點不在清單時（用滑鼠在右欄按過東西，或按完那顆按鈕消失、焦點掉回頁面本身）也能用 J／K／A／R（WEB-129）：
 * 只認字母鍵（↑↓ 留給右欄自己捲），焦點在可以打字的地方不攔。
 */
export function todayPaneKeyAction(e: KeyLike & { target: EventTarget | null }): TodayKey | null {
  if (e.key.startsWith("Arrow")) return null;
  if (isTypingTarget(e.target)) return null;
  return todayKeyAction(e);
}

/** 焦點在可以打字的地方：輸入框、多行輸入、下拉、可編輯區。 */
export function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as Element | null;
  if (!el || typeof el.closest !== "function") return false;
  return (
    el.closest("input, textarea, select, [contenteditable]:not([contenteditable='false'])") !== null
  );
}
