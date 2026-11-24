import type { Session } from "@evergenie/sdk-web";
import { t } from "../../i18n/zh-TW";
import { effortWord } from "../../lib/modelChoices";

// 討論串頂端的「現在用的模型 · 思考力度」（WEB-84、D-46）：看 session 的 model、model_label、effort。
// 名字用引擎給的（model_label），沒給就顯示 model 原字；力度照引擎的字（跟「/effort」選單一樣，xhigh 寫成 extra）。
// 都不知道就不畫（不猜）。

export interface ModelLine {
  /** 標題欄那一小段字，例如「X 9 · 思考 medium」。 */
  text: string;
  /** 滑過、讀屏軟體看的完整說明（含原始代號）。 */
  title: string;
}

export function modelLine(
  session: Pick<Session, "model" | "model_label" | "effort"> | null | undefined,
): ModelLine | null {
  const model = session?.model?.trim() ?? "";
  const label = session?.model_label?.trim() || model;
  const effort = effortWord(session?.effort?.trim() ?? "");
  if (!label && !effort) return null;
  const text = [label, effort ? t("chat.model.effort", { level: effort }) : ""]
    .filter(Boolean)
    .join(" · ");
  const parts = [
    label
      ? t("chat.model.title.model", { model: label === model ? model : `${label}（${model}）` })
      : "",
    effort ? t("chat.model.title.effort", { level: effort }) : "",
  ].filter(Boolean);
  return { text, title: `${parts.join("，")}。${t("chat.model.title.change")}` };
}
