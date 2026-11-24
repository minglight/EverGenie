import { describe, expect, it } from "vitest";
import { internalHref } from "./internalLinks";

const here = new URL("https://board.test/p/habits/list?x=1");
const click = {
  defaultPrevented: false,
  button: 0,
  metaKey: false,
  ctrlKey: false,
  shiftKey: false,
  altKey: false,
};
const a = (href: string, target = "", download = false) => ({
  href: new URL(href, here).href,
  target,
  hasAttribute: (name: string) => name === "download" && download,
});

describe("internalHref", () => {
  it("看板自己的頁面走前端路由，留著查詢與錨點", () => {
    expect(internalHref(click, a("/p/habits/detail?id=1"), here)).toBe("/p/habits/detail?id=1");
    expect(internalHref(click, a("/p/nutrition/trend#goal"), here)).toBe("/p/nutrition/trend#goal");
    expect(internalHref(click, a("/tasks/DEV-1"), here)).toBe("/tasks/DEV-1");
    expect(internalHref(click, a("/"), here)).toBe("/");
  });

  it("伺服器的檔案、外站、新分頁、下載照瀏覽器原本的做法", () => {
    expect(internalHref(click, a("/share/a.png"), here)).toBeNull();
    expect(internalHref(click, a("/persona-export/x.zip"), here)).toBeNull();
    expect(internalHref(click, a("https://example.com/tasks"), here)).toBeNull();
    expect(internalHref(click, a("/tasks", "_blank"), here)).toBeNull();
    expect(internalHref(click, a("/tasks", "", true), here)).toBeNull();
  });

  it("修飾鍵、別的按鍵、已經處理過的點擊不攔", () => {
    expect(internalHref({ ...click, metaKey: true }, a("/tasks"), here)).toBeNull();
    expect(internalHref({ ...click, button: 1 }, a("/tasks"), here)).toBeNull();
    expect(internalHref({ ...click, defaultPrevented: true }, a("/tasks"), here)).toBeNull();
  });

  it("連到目前網址交給瀏覽器（同頁錨點捲動；不多疊一筆）", () => {
    expect(internalHref(click, a("/p/habits/list?x=1#top"), here)).toBeNull();
    expect(internalHref(click, a("/p/habits/list?x=1"), here)).toBeNull();
  });
});
