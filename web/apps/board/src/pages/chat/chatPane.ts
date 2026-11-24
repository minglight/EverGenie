import { createStore, useStore } from "../../data/store";

// 桌機（≥ 1280）討論串旁邊顯示什麼：角色欄（預設）或對話清單（REQ-156）。
// 存 localStorage（方便類的資料，FEN-5 允許）；讀寫都包 try/catch，讀不到就是預設。

export const CHAT_PANE_KEY = "eg.chatPane.v1";
export type ChatPane = "persona" | "list";

export function parsePane(raw: string | null): ChatPane {
  return raw === "list" ? "list" : "persona";
}

function read(): ChatPane {
  try {
    return parsePane(localStorage.getItem(CHAT_PANE_KEY));
  } catch {
    return "persona";
  }
}

export const chatPaneStore = createStore<ChatPane>(read());

/** 從 localStorage 重新讀一次（其他分頁改了；測試也用它）。 */
export function reloadChatPane() {
  const next = read();
  if (next !== chatPaneStore.get()) chatPaneStore.set(next);
}

if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key === CHAT_PANE_KEY || e.key === null) reloadChatPane();
  });
}

export function useChatPane(): { pane: ChatPane; setPane: (next: ChatPane) => void } {
  const pane = useStore(chatPaneStore);
  const setPane = (next: ChatPane) => {
    chatPaneStore.set(next);
    try {
      localStorage.setItem(CHAT_PANE_KEY, next);
    } catch {
      // 寫不進去只是少了「記住」，這次還是照樣生效。
    }
  };
  return { pane, setPane };
}
