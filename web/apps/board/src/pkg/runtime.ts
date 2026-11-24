import * as sdkWeb from "@evergenie/sdk-web";
import * as ui from "@evergenie/ui";
import * as React from "react";
import * as jsxRuntime from "react/jsx-runtime";
import * as ReactDOM from "react-dom";
import * as ReactDOMClient from "react-dom/client";
import { isBoardPath } from "../lib/boardPath";
import { Slot } from "./Slot";

type Go = (href: string, replace: boolean) => void;
let go: Go | null = null;

/** 外殼掛上前端路由（`shell/internalLinks.ts`）；沒掛（測試、還沒 render）時 navigate 退回瀏覽器換頁。 */
export function setHostNavigator(fn: Go | null) {
  go = fn;
}

/**
 * 套件換頁：走看板的前端路由，站內歷史不歸零（FEN-29）。看完就失效的頁面（轉址、找不到）傳 `{ replace: true }`。
 * 只有看板的頁面（`lib/boardPath`，跟站內連結同一份判斷）走前端路由；`/share/`、`/api/` 與外站整頁換。
 */
export function navigate(href: string, opts?: { replace?: boolean }) {
  const replace = opts?.replace === true;
  if (go && isBoardPath(href)) {
    go(href, replace);
    return;
  }
  if (replace) window.location.replace(href);
  else window.location.assign(href);
}

/** 套件頁面可以 import 的 `@evergenie/host`：外殼提供的 slot 元件、已經接好的 API client、換頁。 */
export const host = { Slot, client: sdkWeb.api, navigate };

/**
 * 外殼提供給套件頁面的共用模組（D-11）：核心編譯套件頁面時，不打包這些 import，
 * 改成讀 `globalThis.__hgShared[<名稱>]`。要在 render 之前呼叫（main.tsx）。
 */
export function installSharedRuntime() {
  (globalThis as Record<string, unknown>).__hgShared = {
    react: React,
    "react/jsx-runtime": jsxRuntime,
    "react-dom": ReactDOM,
    "react-dom/client": ReactDOMClient,
    "@evergenie/ui": ui,
    "@evergenie/sdk-web": sdkWeb,
    "@evergenie/host": host,
  };
}
