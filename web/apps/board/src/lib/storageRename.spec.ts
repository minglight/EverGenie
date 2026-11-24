import { afterEach, describe, expect, it } from "vitest";
import { renameBrowserStorage, renameStoredKeys } from "./storageRename";

describe("瀏覽器存的鍵改名（hg. → eg.）", () => {
  afterEach(() => {
    localStorage.clear();
    sessionStorage.clear();
  });

  it("舊鍵搬到新鍵、舊的刪掉；別的鍵不動", () => {
    localStorage.setItem("hg.theme", "dark");
    localStorage.setItem("hg.draft.chat.code", "寫到一半");
    localStorage.setItem("other.key", "x");
    expect(renameStoredKeys(localStorage)).toBe(2);
    expect(localStorage.getItem("eg.theme")).toBe("dark");
    expect(localStorage.getItem("eg.draft.chat.code")).toBe("寫到一半");
    expect(localStorage.getItem("hg.theme")).toBeNull();
    expect(localStorage.getItem("hg.draft.chat.code")).toBeNull();
    expect(localStorage.getItem("other.key")).toBe("x");
  });

  it("新鍵已經有值就以新的為準，舊的照樣收掉；再跑一次什麼都不做", () => {
    localStorage.setItem("eg.chatPane.v1", "list");
    localStorage.setItem("hg.chatPane.v1", "thread");
    expect(renameStoredKeys(localStorage)).toBe(0);
    expect(localStorage.getItem("eg.chatPane.v1")).toBe("list");
    expect(localStorage.getItem("hg.chatPane.v1")).toBeNull();
    expect(renameStoredKeys(localStorage)).toBe(0);
  });

  it("開看板時 localStorage 與 sessionStorage 都搬", () => {
    localStorage.setItem("hg.agentFilter.v2", '["code"]');
    sessionStorage.setItem("hg.client", "c-1");
    renameBrowserStorage();
    expect(localStorage.getItem("eg.agentFilter.v2")).toBe('["code"]');
    expect(sessionStorage.getItem("eg.client")).toBe("c-1");
  });

  it("寫不進去時舊的留著，下次再搬", () => {
    const data = new Map<string, string>([["hg.theme", "dark"]]);
    const full = {
      get length() {
        return data.size;
      },
      key: (i: number) => [...data.keys()][i] ?? null,
      getItem: (k: string) => data.get(k) ?? null,
      setItem: () => {
        throw new Error("QuotaExceededError");
      },
      removeItem: (k: string) => void data.delete(k),
      clear: () => data.clear(),
    } as Storage;
    expect(renameStoredKeys(full)).toBe(0);
    expect(data.get("hg.theme")).toBe("dark");
  });
});
