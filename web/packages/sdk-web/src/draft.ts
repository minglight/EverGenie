// 聊天輸入框的草稿 key（看板 ThreadComposer 讀同一個 key）。放在 sdk 讓套件頁面也能幫主人起個頭（DEV-142）。
export const chatDraftKey = (session: string) => `eg.draft.chat.${session}`;

/** 在某位秘書的聊天輸入框先放一句開頭；那串已經有草稿就不蓋掉。存不了（私密模式等）就算了。 */
export function seedChatDraft(session: string, text: string): void {
  try {
    const key = chatDraftKey(session);
    if ((window.localStorage.getItem(key) ?? "") === "") window.localStorage.setItem(key, text);
  } catch {
    // 存不了就算了，聊天照樣打得開。
  }
}
