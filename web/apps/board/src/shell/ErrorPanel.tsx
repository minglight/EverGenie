import { Button } from "@evergenie/ui";
import { t } from "../i18n/zh-TW";

// 頁面出錯時的白話說明（UXC-8）：不顯示堆疊、內部路徑或錯誤碼。
export function ErrorPanel({ message }: { message?: string }) {
  return (
    <div
      role="alert"
      className="flex min-h-full flex-col items-center justify-center gap-2 px-6 py-16 text-center"
    >
      <h1 className="text-xl font-bold">{t("app.error.title")}</h1>
      <p className="max-w-sm text-sm text-muted">{message ?? t("app.error.hint")}</p>
      <Button variant="primary" className="mt-3" onClick={() => window.location.reload()}>
        {t("app.error.reload")}
      </Button>
    </div>
  );
}
