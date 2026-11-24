import { type TextKey, t } from "../i18n/zh-TW";

/**
 * 狀態字：看板認得的照表翻成白話；不認得的（別台版本比較新、回了新的狀態，S3-7b）照原字顯示，不留空白。
 * 顏色那邊由 Pill 對不認得的 tone 畫灰（muted）。
 */
export function knownText(map: Readonly<Record<string, TextKey>>, value: string): string {
  const key = Object.hasOwn(map, value) ? map[value] : undefined;
  return key ? t(key) : value;
}
