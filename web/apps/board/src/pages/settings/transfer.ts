import type { PersonaCard, PersonasImportResponse } from "@evergenie/sdk-web";

// 角色的匯出與匯入（DEV-116 S3、WEB-151～155）：畫面的純函式。真正的規則（收什麼、蓋不蓋、驗什麼）都在伺服器，
// 這裡只挑能匯出的卡、整理預覽的數字（FEN-N3）。

/** 能匯出的卡：官方位置與「我的」；套件帶來的角色跟著套件走、被官方蓋住的我的卡匯出時會拿到官方那張，都不列。 */
export function exportable(cards: readonly PersonaCard[]): PersonaCard[] {
  return cards.filter((c) => c.slug && c.source !== "package" && !c.shadowed);
}

/** 勾選切換：有就拿掉、沒有就加上（保持原本的順序）。 */
export function toggle(picked: readonly string[], slug: string): string[] {
  return picked.includes(slug) ? picked.filter((s) => s !== slug) : [...picked, slug];
}

export interface ImportCounts {
  added: number;
  replaced: number;
  same: number;
  recordsAdded: number;
  recordsReplaced: number;
  recordsSame: number;
  /** 你改過、這次不換的（檔＋紀錄）。 */
  kept: number;
  skipped: number;
}

export function importCounts(res: PersonasImportResponse): ImportCounts {
  return {
    added: res.added?.length ?? 0,
    replaced: res.replaced?.length ?? 0,
    same: res.unchanged ?? 0,
    recordsAdded: res.records_added?.length ?? 0,
    recordsReplaced: res.records_replaced?.length ?? 0,
    recordsSame: res.records_unchanged ?? 0,
    kept: (res.kept?.length ?? 0) + (res.records_kept?.length ?? 0),
    skipped: res.records_skipped?.length ?? 0,
  };
}

/** 預覽裡有「你改過的」才給「蓋掉我改過的」勾選（force）。 */
export function hasEdits(res: PersonasImportResponse): boolean {
  return importCounts(res).kept > 0;
}

/** 你改過、這次不換的清單：檔案在前、紀錄在後。 */
export function keptItems(res: PersonasImportResponse): string[] {
  return [...(res.kept ?? []), ...(res.records_kept ?? [])];
}

const GB = 1024 * 1024 * 1024;

/** 檔案大小的白話（例：「1.2 MB」「2 GB」）。 */
export function sizeText(bytes: number): string {
  if (bytes >= GB) return `${bytes % GB === 0 ? bytes / GB : (bytes / GB).toFixed(1)} GB`;
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} bytes`;
}

/** 叫瀏覽器下載伺服器做好的檔（同源網址，伺服器回 attachment；手機的 Safari 也會跳下載）。 */
export function startDownload(href: string, filename: string) {
  const a = document.createElement("a");
  a.href = href;
  a.download = filename;
  a.rel = "noopener";
  a.style.display = "none";
  document.body.appendChild(a);
  a.click();
  a.remove();
}
