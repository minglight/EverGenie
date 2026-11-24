import type { PageInfo } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import { slotPages } from "../../pkg/Slot";
import {
  currentPersonaTab,
  INTRO_TAB,
  itemSearch,
  personaTabs,
  SLOT_PERSONA_TAB,
  tabSearch,
} from "./personaTabs";

// 一位角色一頁（DEV-143）：介紹在前、套件掛在 core.persona.tab 的分頁照 slot 的順序；分頁記在 ?tab=。
describe("角色頁的分頁", () => {
  const pages = [
    { id: "demo.tab", title: "衣服" },
    { id: "studio.voice", title: "" },
    { title: "沒有 id 的不列" },
  ];

  test("TestPersonaTabsIntroFirstThenPackages", () => {
    expect(personaTabs("介紹", pages)).toEqual([
      { key: INTRO_TAB, label: "介紹" },
      { key: "demo.tab", label: "衣服" },
      // 套件沒給標題就寫頁面 id，不留空白。
      { key: "studio.voice", label: "studio.voice" },
    ]);
  });

  test("TestPersonaTabFromUrl", () => {
    const tabs = personaTabs("介紹", pages);
    expect(currentPersonaTab(tabs, undefined, true)).toBe(INTRO_TAB);
    expect(currentPersonaTab(tabs, "intro", true)).toBe(INTRO_TAB);
    expect(currentPersonaTab(tabs, "demo.tab", true)).toBe("demo.tab");
    // 套件關掉了（對不到）：回介紹，不留空白頁。
    expect(currentPersonaTab(tabs, "gone.tab", true)).toBe(INTRO_TAB);
    // 套件頁清單還沒讀到：先載入中（null），不先閃一下介紹。
    expect(currentPersonaTab(personaTabs("介紹", []), "demo.tab", false)).toBeNull();
    expect(tabSearch(INTRO_TAB)).toEqual({});
    expect(tabSearch("demo.tab")).toEqual({ tab: "demo.tab" });
  });

  // 套件分頁裡打開哪一項記在 ?item=（重新整理回同一個畫面）；關掉就拿掉；介紹分頁沒有 item；換分頁（tabSearch）不帶 item。
  test("TestPersonaTabItemInUrl", () => {
    expect(itemSearch("demo.tab", "helper-my-1")).toEqual({ tab: "demo.tab", item: "helper-my-1" });
    expect(itemSearch("demo.tab", "")).toEqual({ tab: "demo.tab" });
    expect(itemSearch(undefined, "x")).toEqual({});
    expect(itemSearch(INTRO_TAB, "x")).toEqual({});
    expect(tabSearch("demo.tab")).not.toHaveProperty("item");
  });

  // 分頁「API 支援才顯示」（DEV-160 S4、MNU-11）：能力的判斷在核心（Go TestSlotPageShowWhenCapability），能力沒有時那頁不在 get_pages；
  // 看板只照拿到的清單畫、自己不判斷是哪種家：清單沒有那頁就沒有那個分頁，網址指著它也回介紹；清單有了，分頁就在。
  test("TestPersonaTabsOnlyFromListedPages", () => {
    const clothes: PageInfo = {
      id: "guest.clothes",
      title: "衣服",
      slot: SLOT_PERSONA_TAB,
      bundle_url: "/pkg-bundles/a.js",
      chat: false,
    };
    const edit: PageInfo = {
      id: "guest.edit",
      title: "修改",
      slot: SLOT_PERSONA_TAB,
      bundle_url: "/pkg-bundles/b.js",
      chat: false,
    };
    const without = personaTabs("介紹", slotPages([clothes], SLOT_PERSONA_TAB));
    expect(without.map((x) => x.label)).toEqual(["介紹", "衣服"]);
    expect(currentPersonaTab(without, "guest.edit", true)).toBe(INTRO_TAB);

    const withCap = personaTabs("介紹", slotPages([edit, clothes], SLOT_PERSONA_TAB));
    expect(withCap.map((x) => x.label)).toEqual(["介紹", "衣服", "修改"]);
    expect(currentPersonaTab(withCap, "guest.edit", true)).toBe("guest.edit");
  });
});
