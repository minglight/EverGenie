import { type ComponentType, useEffect, useState } from "react";

/** 套件頁面的 props（`/p/<套件>/<頁面>` 與 slot 都是）。 */
export interface PackagePageProps {
  pkg: string;
  page: string;
  /** `location.search` 字串（含開頭的 ?，沒有就是空字串）。 */
  search: string;
}

// biome-ignore lint/suspicious/noExplicitAny: 套件元件的 props 由載入的地方決定（頁面、slot、選單元件各不相同）
export type PackageComponent = ComponentType<any>;

const loaded = new Map<string, Promise<PackageComponent>>();
// 這個網址失敗過幾次：重試時網址加 ?retry=n，瀏覽器才會真的再抓（同一個網址載失敗，有的瀏覽器會記住失敗）。
const failures = new Map<string, number>();

/** 套件頁面的程式沒抓到（斷網、服務剛重開）：跟「畫面自己壞掉」分開，這種會自己再試。 */
export class BundleLoadError extends Error {
  constructor(cause: unknown) {
    super(cause instanceof Error ? cause.message : String(cause));
    this.name = "BundleLoadError";
  }
}

/** 載入編譯好的套件頁面，取 default export；同一個網址成功只載一次，失敗的不留著（下次呼叫重抓）。 */
export function loadBundle(url: string): Promise<PackageComponent> {
  let p = loaded.get(url);
  if (!p) {
    const tried = failures.get(url) ?? 0;
    const src = tried > 0 ? `${url}${url.includes("?") ? "&" : "?"}retry=${tried}` : url;
    p = import(/* @vite-ignore */ src).then(
      (mod: { default?: unknown }) => {
        if (typeof mod.default !== "function" && typeof mod.default !== "object") {
          throw new Error("頁面沒有 export default 的元件");
        }
        return mod.default as PackageComponent;
      },
      (err: unknown) => {
        throw new BundleLoadError(err);
      },
    );
    loaded.set(url, p);
    p.catch((err: unknown) => {
      if (err instanceof BundleLoadError) failures.set(url, tried + 1);
      if (loaded.get(url) === p) loaded.delete(url);
    });
  }
  return p;
}

export interface BundleState {
  Component?: PackageComponent;
  error?: Error;
}

/** 載入 url 的元件；還沒載好兩個欄位都是空，載失敗給 error。url 空白就什麼都不載。 */
export function useBundle(url: string | undefined): BundleState {
  const [state, setState] = useState<{ url: string } & BundleState>();
  useEffect(() => {
    if (!url) return;
    let live = true;
    loadBundle(url).then(
      (Component) => live && setState({ url, Component }),
      (err: unknown) =>
        live && setState({ url, error: err instanceof Error ? err : new Error(String(err)) }),
    );
    return () => {
      live = false;
    };
  }, [url]);
  return state && state.url === url ? state : {};
}
