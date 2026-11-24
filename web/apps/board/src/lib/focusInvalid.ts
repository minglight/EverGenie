/**
 * 表單送出被擋（必填沒填）時，把焦點移到第一個標了 `aria-invalid` 的欄位並捲進畫面。
 * 手機上的抽屜表單會捲動：錯誤寫在最上面那欄、人在最下面按「建立」時看不到，像是按了沒反應（GOL-14）。
 * 要在錯誤畫出來之後呼叫（例如 `requestAnimationFrame` 裡）。找到回 true。
 */
export function focusFirstInvalid(root: ParentNode | null | undefined): boolean {
  const el = root?.querySelector<HTMLElement>('[aria-invalid="true"]');
  if (!el) return false;
  el.focus({ preventScroll: true });
  el.scrollIntoView?.({ block: "center" });
  return true;
}
