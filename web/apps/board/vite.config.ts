import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// 輸出進 git（使用者不用裝 Node，D-11）；只由 `make web` 產生。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../../../internal/base/app/board/dist",
    emptyOutDir: true,
  },
});
