// 一定要第一個 import：改名前存的偏好、草稿（hg. 開頭的鍵）先搬到新鍵，後面的模組載入時才讀得到。
import "./lib/storageRenameBoot";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import { installStaleReload } from "./lib/staleReload";
import { applyStoredTheme } from "./lib/theme";
import { installSharedRuntime } from "./pkg/runtime";
import { router } from "./router";

const root = document.getElementById("root");
if (!root) {
  throw new Error("找不到 #root");
}

applyStoredTheme();
// 套件頁面用的共用模組（React、@evergenie/ui…）要在任何畫面 render、套件頁面載入之前設好。
installSharedRuntime();

// 正式版才註冊 service worker（App 推播、可安裝成 App）；開發時不註冊，免得快取干擾。
if (import.meta.env.PROD && "serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(() => {
      // 註冊失敗只是收不到推播，看板照常使用。
    });
  });
}

// 部署新版後，開著的舊分頁沒在打字就自己重新整理（WEB-172）。
if (import.meta.env.PROD) installStaleReload();

const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: true, retry: 1 } },
});

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
