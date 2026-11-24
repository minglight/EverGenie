// 使用者訊息裡的附件行（Composer 組的）：`![原檔名](/share/x)` 或 `[原檔名](/share/x)`，整行只有這個。
const ATTACHMENT_LINE = /^!?\[[^\]]*\]\(\/share\/[^)\s]+\)$/;

/** 把訊息拆成文字和附件行；附件行交給 Markdown 畫成縮圖或下載 chip，文字照原樣顯示。 */
export function splitAttachments(message: string): { text: string; attachments: string[] } {
  const text: string[] = [];
  const attachments: string[] = [];
  for (const line of message.split("\n")) {
    if (ATTACHMENT_LINE.test(line.trim())) attachments.push(line.trim());
    else text.push(line);
  }
  return { text: text.join("\n").trim(), attachments };
}
