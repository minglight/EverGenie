import type { SiteRef } from "./sites";

// 別台的一筆能不能連到它的詳情頁（S3-7a）：詳情頁要認得 ?home= 才會從那台讀；還不認得的頁，
// 連過去會打開主伺服器同編號的那一筆（還可能畫出驗收鈕，寫到錯的家，FEN-28）。
// 現在認得的：訊息（S3-7a）、聊天（S3-6）、任務單、待辦、Goal（S3-7b，詳情頁用 SiteGate 照 ?home= 找台）。
const READS_HOME = new Set<string>([
  "/messages/$id",
  "/chat/$session",
  "/tasks/$id",
  "/reminders/$id",
  "/goals/$id",
]);

/** 主伺服器的一律可以連；別台的只有那個詳情頁認得 ?home= 才連。 */
export function canLinkDetail(site: Pick<SiteRef, "key">, to: string): boolean {
  return site.key === "" || READS_HOME.has(to);
}
