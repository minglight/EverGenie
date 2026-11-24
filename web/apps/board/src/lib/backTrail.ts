// 走過的網址（第幾筆歷史 → 網址）：「上一頁」靠它跳過跟現在同一個網址的那幾筆。
// 刪除、找不到之後用 replace 回清單時，歷史會變成「清單、清單」；不跳過的話那一按畫面不會動（FEN-29）。
// 只記在記憶體：重新整理後認不得的筆當成不同，退一步就好。
let trail: (string | undefined)[] = [];

export function recordTrail(index: number, href: string) {
  trail[index] = href;
}

/** 要退幾筆才會看到不一樣的網址；再往前都是同一個網址（退了畫面也不會變）回 null。 */
export function stepsBack(index: number, href: string): number | null {
  for (let at = index - 1; at >= 0; at--) {
    if (trail[at] !== href) return index - at;
  }
  return null;
}

export function resetTrail() {
  trail = [];
}
