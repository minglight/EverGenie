import { useEffect } from "react";
import { createStore, useStore } from "./store";

// 每頁自己算好「這一頁目前檢視裡，每位秘書有幾件」，桌機側欄的篩選段跟著顯示同一份（補充契約 S8 第 6、8 條）。
// 不在這五頁時沒有人登記（null），側欄就不顯示件數。

export interface PageCounts {
  counts: Record<string, number>;
  /** 「全部」下面的 N 件：這一頁目前檢視的總筆數（不受篩選影響）。 */
  total: number;
}

const store = createStore<PageCounts | null>(null);

function same(a: PageCounts | null, b: PageCounts | null): boolean {
  if (a === b) return true;
  if (!a || !b || a.total !== b.total) return false;
  const keys = new Set([...Object.keys(a.counts), ...Object.keys(b.counts)]);
  for (const k of keys) if ((a.counts[k] ?? 0) !== (b.counts[k] ?? 0)) return false;
  return true;
}

/** 側欄讀的：目前這一頁登記的件數；沒有就是 null。 */
export function usePageCounts(): PageCounts | null {
  return useStore(store);
}

/** 頁面登記自己的件數（傳 null＝這個畫面現在不需要，例如討論串本身）；離開頁面時清掉。 */
export function usePublishPageCounts(value: PageCounts | null): void {
  const key = value ? JSON.stringify([value.total, Object.entries(value.counts).sort()]) : "";
  // biome-ignore lint/correctness/useExhaustiveDependencies: 用 key 比內容，value 每次 render 都是新物件
  useEffect(() => {
    if (!same(store.get(), value)) store.set(value);
  }, [key]);
  useEffect(() => () => store.set(null), []);
}
