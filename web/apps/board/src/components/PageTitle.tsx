import { type ComponentProps, useEffect, useSyncExternalStore } from "react";

// 每頁只有一個第一層標題（h1；WCAG 1.3.1，DEV-156 U-31）。
// 手機的頁首本來就是 h1（「待辦」「任務單」…）；內文自己有更準的標題時（詳情頁的那一筆、找不到的說明），
// 內文用 <PageTitle>（畫出來就是 h1），頁首就自動退成一般文字，不會一頁兩個 h1。
// 只在手機也會畫出來的內文標題才需要用它；只有桌機才畫的標題（桌機沒有頁首）照舊寫 <h1>。

let owners = 0;
const listeners = new Set<() => void>();

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** 現在畫面上有沒有內文自己的第一層標題（外殼的手機頁首用）。 */
export function useOwnPageTitle(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => owners > 0,
    () => false,
  );
}

/** 內文自己的第一層標題：用法跟 <h1> 一樣。 */
export function PageTitle(props: ComponentProps<"h1">) {
  useEffect(() => {
    owners += 1;
    for (const fn of listeners) fn();
    return () => {
      owners -= 1;
      for (const fn of listeners) fn();
    };
  }, []);
  return <h1 {...props} />;
}
