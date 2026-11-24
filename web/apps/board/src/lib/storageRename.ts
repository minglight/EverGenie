// 改名（DEV-158）：瀏覽器裡存的鍵從「hg.」開頭換成「eg.」開頭。主人已經存的偏好、草稿要帶過去，不要讓他重設：
// 開看板時把舊鍵搬一次（新鍵已經有值就以新的為準），搬好才刪舊的。看板與套件頁面同一個網域，
// 套件頁面存的（closet、fitness、habits 的鍵）也一起搬。存取被擋（無痕模式、關掉網站資料）就不搬，照常開。
const OLD_PREFIX = "hg."; // 讀舊名（legacy）：改名前的鍵
const NEW_PREFIX = "eg.";

/** 把一個 Storage 裡「hg.」開頭的鍵搬到「eg.」開頭；回搬了幾個。 */
export function renameStoredKeys(store: Storage): number {
  let moved = 0;
  const old: string[] = [];
  try {
    for (let i = 0; i < store.length; i++) {
      const key = store.key(i);
      if (key?.startsWith(OLD_PREFIX)) old.push(key);
    }
  } catch {
    return 0;
  }
  for (const key of old) {
    try {
      const value = store.getItem(key);
      const next = NEW_PREFIX + key.slice(OLD_PREFIX.length);
      if (value !== null && store.getItem(next) === null) {
        store.setItem(next, value);
        moved++;
      }
      store.removeItem(key);
    } catch {
      // 寫不進去（滿了、被擋）：舊的留著，下次開再搬。
    }
  }
  return moved;
}

/** 開看板時跑一次：localStorage 與 sessionStorage 都搬。 */
export function renameBrowserStorage(): void {
  for (const pick of [() => window.localStorage, () => window.sessionStorage]) {
    let store: Storage | undefined;
    try {
      store = pick();
    } catch {
      store = undefined;
    }
    if (store) renameStoredKeys(store);
  }
}
