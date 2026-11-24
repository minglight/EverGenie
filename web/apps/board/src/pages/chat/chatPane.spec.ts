import { afterEach, describe, expect, test } from "vitest";
import { CHAT_PANE_KEY, chatPaneStore, parsePane, reloadChatPane } from "./chatPane";

afterEach(() => {
  localStorage.clear();
  reloadChatPane();
});

describe("chatPane", () => {
  // Guards: REQ-156
  test("只有「list」認得，其他（壞值、沒有）都是角色欄", () => {
    expect(parsePane("list")).toBe("list");
    expect(parsePane("persona")).toBe("persona");
    expect(parsePane(null)).toBe("persona");
    expect(parsePane("{壞的")).toBe("persona");
  });

  // Guards: REQ-156
  test("其他分頁改了（storage 事件）就跟著變，清掉就回到預設", () => {
    localStorage.setItem(CHAT_PANE_KEY, "list");
    window.dispatchEvent(new StorageEvent("storage", { key: CHAT_PANE_KEY }));
    expect(chatPaneStore.get()).toBe("list");
    localStorage.removeItem(CHAT_PANE_KEY);
    window.dispatchEvent(new StorageEvent("storage", { key: CHAT_PANE_KEY }));
    expect(chatPaneStore.get()).toBe("persona");
  });
});
