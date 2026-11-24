// 套件頁面 import 的 `@evergenie/host` 的型別（tsconfig.packages.json 用）。
// 執行時由 runtime.ts 的 `host` 物件提供，兩邊要一致。
import type { api } from "@evergenie/sdk-web";

export { Slot } from "./Slot";
export declare const client: typeof api;
/** 換頁走看板的前端路由（上一頁回得去，FEN-29）；轉址這類看完就失效的頁面傳 `{ replace: true }`。 */
export declare function navigate(href: string, opts?: { replace?: boolean }): void;
