import { useEffect, useSyncExternalStore } from "react";

function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const mq = window.matchMedia(query);
      mq.addEventListener("change", onChange);
      return () => mq.removeEventListener("change", onChange);
    },
    () => window.matchMedia(query).matches,
    () => false,
  );
}

// 鍵盤怎麼處理（D-59）：不在 viewport 加 interactive-widget=resizes-content（iOS Safari 不認，Android 會把整個版面縮小、
// 底部選單跟著騎到鍵盤上，兩邊行為不同）；版面照常用 dvh，鍵盤打開時「一定要看得到」的東西改看 visualViewport：
// 高度上限用 CSS 變數 --hg-visible-h（useVisibleHeightVar 寫，沒寫就是 100dvh），貼頂的浮層加上 offsetTop。
function subscribeVisible(onChange: () => void) {
  const vv = window.visualViewport;
  vv?.addEventListener("resize", onChange);
  // iOS 打開鍵盤會把看得到的那塊往下捲（offsetTop 變），只發 scroll 不發 resize。
  vv?.addEventListener("scroll", onChange);
  window.addEventListener("resize", onChange);
  return () => {
    vv?.removeEventListener("resize", onChange);
    vv?.removeEventListener("scroll", onChange);
    window.removeEventListener("resize", onChange);
  };
}

const visibleHeight = () => Math.round(window.visualViewport?.height ?? window.innerHeight ?? 0);

/**
 * 畫面上真的看得到的高度（px）：手機鍵盤打開時變小（`visualViewport`；`dvh` 在 iOS 不會扣掉鍵盤）。
 * 伺服器端／量不到回 0。
 */
export const useVisibleHeight = () =>
  useSyncExternalStore(subscribeVisible, visibleHeight, () => 0);

/**
 * 看得到的那塊離版面頂端多遠（px，WEB-128）：iOS 打開鍵盤時會把畫面往上推，`position: fixed; top` 的浮層
 * 頂端會跑到看不到的地方，要加上這個值。量不到回 0。
 */
export const useVisibleTop = () =>
  useSyncExternalStore(
    subscribeVisible,
    () => Math.max(0, Math.round(window.visualViewport?.offsetTop ?? 0)),
    () => 0,
  );

/** CSS 變數名：看得到的高度（px）；沒寫的時候 index.css 給 100dvh。 */
export const VISIBLE_HEIGHT_VAR = "--hg-visible-h";

/**
 * 把看得到的高度寫進根元素的 CSS 變數（WEB-128），讓「最高 55% 畫面」這類上限在鍵盤打開時跟著變矮
 * （`max-h-[calc(var(--hg-visible-h)*0.55)]`）。外殼掛一次就好。
 */
export function useVisibleHeightVar() {
  const height = useVisibleHeight();
  useEffect(() => {
    const root = document.documentElement;
    if (height > 0) root.style.setProperty(VISIBLE_HEIGHT_VAR, `${height}px`);
    else root.style.removeProperty(VISIBLE_HEIGHT_VAR);
  }, [height]);
  useEffect(
    () => () => {
      document.documentElement.style.removeProperty(VISIBLE_HEIGHT_VAR);
    },
    [],
  );
}

/** ≥ 768px：側欄版面；< 768px：底部列版面。 */
export const useIsDesktop = () => useMedia("(min-width: 768px)");
/** ≥ 1024px：「今天」頁兩欄。 */
export const useIsWide = () => useMedia("(min-width: 1024px)");
/** ≥ 1280px：討論串旁邊可以放角色欄。 */
export const useIsXWide = () => useMedia("(min-width: 1280px)");
