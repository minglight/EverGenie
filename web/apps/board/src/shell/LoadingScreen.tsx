import { Spinner } from "@evergenie/ui";
import { t } from "../i18n/zh-TW";
import { GenieMark } from "./GenieMark";

// 選單還沒拿到之前，畫面只有這個：不畫選單、也不畫頁面（REQ-105、FEN-N1）。
// 最多停 MENU_GRACE_MS；再久、或讀失敗，外殼改用基本選單照常畫出來（WEB-175，Shell.tsx）。
export function LoadingScreen() {
  return (
    <main
      className="grid min-h-dvh place-items-center bg-bg text-fg"
      aria-busy="true"
      data-testid="boot-loading"
    >
      <div className="flex flex-col items-center gap-3 text-muted">
        <GenieMark className="size-12" />
        <Spinner />
        <p role="status" className="text-sm">
          {t("app.loading")}
        </p>
      </div>
    </main>
  );
}
