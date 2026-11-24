import { ApiError } from "@evergenie/sdk-web";
import { t } from "../i18n/zh-TW";

/** 伺服器的白話錯誤直接顯示；網路、瀏覽器自己丟的（英文、內部）一律換成一句白話（UXC-8）。 */
export function reasonOf(err: unknown): string {
  return err instanceof ApiError && err.message ? err.message : t("settings.error.generic");
}
