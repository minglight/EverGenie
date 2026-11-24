import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

export default defineConfig({
  // 官方套件的頁面在 web/ 外面，找不到 web/node_modules；測它們用到 SDK 的檔案（例 closet 的 store.spec）時要指給 vite 看。
  resolve: {
    alias: {
      "@evergenie/sdk-web": fileURLToPath(
        new URL("./packages/sdk-web/src/index.ts", import.meta.url),
      ),
    },
  },
  // 官方套件在 web/ 外面（../packages/official），要讓 vite 讀得到。
  server: { fs: { allow: [".."] } },
  test: {
    environment: "jsdom",
    // 並行數上限（T2 第 1 條）：EVERGENIE_CPU 是 Makefile 設的全機上限，但 vitest 再多開也不會更省——
    // 主要成本是每個測試檔各建一個 jsdom（實測 2–4 個 worker 約 112 CPU 秒、7 個約 142、預設的核心數減一更多），
    // 所以最多 4 個。
    maxWorkers: Math.min(Number(process.env.EVERGENIE_CPU) || 4, 4),
    include: [
      "{apps,packages}/**/*.spec.{ts,tsx}",
      // 官方套件頁面裡的純函式測試（P6-3b）。
      "../packages/official/*/pages/**/*.spec.ts",
    ],
    exclude: ["**/node_modules/**", "**/e2e/**"],
    // 設計 token 的測試要讀 tokens.css 的原文（`?raw`）；其他 CSS 照舊不處理（WEB-70）。
    css: { include: [/tokens\.css/] },
  },
});
