import { useRouter } from "@tanstack/react-router";
import { useEffect } from "react";
import { isBoardPath } from "../lib/boardPath";
import { setHostNavigator } from "../pkg/runtime";

interface ClickLike {
  defaultPrevented: boolean;
  button: number;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}

interface AnchorLike {
  href: string;
  target: string;
  hasAttribute(name: string): boolean;
}

/** 這個點擊要不要改走前端路由：要就回路徑，不要（新分頁、外站、下載、連到目前網址）回 null。 */
export function internalHref(e: ClickLike, a: AnchorLike, here: Location | URL): string | null {
  if (e.defaultPrevented || e.button !== 0) return null;
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return null;
  if ((a.target && a.target !== "_self") || a.hasAttribute("download") || !a.href) return null;
  const url = new URL(a.href, here.href);
  if (url.origin !== here.origin || !isBoardPath(url.pathname)) return null;
  // 連到目前網址（含同頁錨點）交給瀏覽器：不多疊一筆一樣的歷史。
  if (url.pathname === here.pathname && url.search === here.search) return null;
  return `${url.pathname}${url.search}${url.hash}`;
}

/**
 * 內容區（含抽屜、對話框，它們 portal 在 main 外面）裡的一般連結（套件頁的 <a href>、訊息裡的連結）走前端路由，不整頁重載：
 * 重載會讓站內歷史歸零，「上一頁」就退不回剛才那頁（FEN-29）。
 * 掛在 document：React 的 Link 已經 preventDefault，這裡看到就跳過。
 */
export function useInternalLinks() {
  const router = useRouter();
  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      const a = (e.target as Element | null)?.closest?.("main a[href], [role=dialog] a[href]");
      if (!(a instanceof HTMLAnchorElement)) return;
      const href = internalHref(e, a, window.location);
      if (href === null) return;
      e.preventDefault();
      router.history.push(href);
    };
    document.addEventListener("click", onClick);
    // 套件頁的 navigate（`@evergenie/host`）也走同一個前端路由。
    setHostNavigator((href, replace) =>
      replace ? router.history.replace(href) : router.history.push(href),
    );
    return () => {
      document.removeEventListener("click", onClick);
      setHostNavigator(null);
    };
  }, [router]);
}
