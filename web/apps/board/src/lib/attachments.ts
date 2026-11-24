import { isShareImage } from "@evergenie/ui";

// 任務單與待辦附件（TSK-28、RMD-23）的純函式：單上存的是 `/share/<名字>` 連結（看板上傳、`evergenie share` 放進分享資料夾的檔）。

// 存檔時加的字首：看板上傳 `YYYYMMDD-HHMMSS-<4 碼十六進位>-`、`evergenie share` 是 ULID（26 碼）加 `-`。
const SHARE_PREFIX = /^(?:\d{8}-\d{6}-[0-9a-f]{4}|[0-9A-HJKMNP-TV-Z]{26})-/;

/** 附件給人看的名字：連結最後一段去掉存檔加的字首（原檔名）；解不開就照原字。 */
export function attachmentName(url: string): string {
  const last = url.split("/").pop() ?? url;
  let name = last;
  try {
    name = decodeURIComponent(last);
  } catch {
    // 不是合法的百分比編碼：照原字
  }
  const plain = name.replace(SHARE_PREFIX, "");
  return plain || name;
}

/** 照副檔名判斷是不是圖片（圖片直接顯示縮圖，其他畫成下載的 chip）；清單跟聊天、伺服器同一份（ui 的 isShareImage）。 */
export function isImageName(name: string): boolean {
  return isShareImage(name);
}

/** 剪貼簿或拖進來的檔案：沒有檔名（或是瀏覽器給的 image.png）的截圖改名成 paste-<時間>.png，名字才分得開。 */
export function namedFile(f: File, now = Date.now()): File {
  if (f.name && f.name !== "image.png") return f;
  const ext = (f.type.split("/")[1] ?? "png").replace(/[^a-z0-9]/gi, "") || "png";
  return new File([f], `paste-${now}.${ext}`, { type: f.type });
}
