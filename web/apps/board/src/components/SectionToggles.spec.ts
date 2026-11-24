import type { Package } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import { afterToggle, sectionPackages } from "./SectionToggles";

const pkg = (id: string, enabled: boolean, section: string, order: number): Package => ({
  id,
  name: id,
  enabled,
  menu: [{ id: "log", title: id, order, page: "log", section }],
});

describe("合併頁的項目開關", () => {
  // Guards: REQ-236
  test("列出這個 section 的套件（停用的也列，才開得回來），依選單順序；別的 section 不算", () => {
    const all = [
      pkg("body", false, "health", 21),
      pkg("habits", true, "", 30),
      pkg("fitness", true, "health", 20),
      pkg("nutrition", true, "health", 10),
      pkg("other", true, "money", 1),
    ];
    expect(sectionPackages(all, "health").map((p) => p.id)).toEqual([
      "nutrition",
      "fitness",
      "body",
    ]);
    expect(sectionPackages(all, "none")).toEqual([]);
  });

  // WEB-137：關掉正在看的套件、又沒有別的套件分頁時回到同 section 的核心頁（關掉衣櫃回「介紹」），再沒有回今天。
  test("關掉正在看的套件：換到還開著的套件分頁，沒有就回核心頁，再沒有回今天；其他情況不換頁", () => {
    const both = [pkg("wardrobe", true, "personas", 40), pkg("showroom", true, "personas", 41)];
    const base = { pkg: "wardrobe", on: false, current: "wardrobe", section: "personas" };
    expect(afterToggle(both, { ...base, home: "/personas" })).toBe("/p/showroom/log");
    const alone = [pkg("wardrobe", true, "personas", 40)];
    expect(afterToggle(alone, { ...base, home: "/personas" })).toBe("/personas");
    expect(afterToggle(alone, base)).toBe("/");
    expect(afterToggle(alone, { ...base, on: true })).toBeUndefined();
    expect(afterToggle(both, { ...base, current: "showroom" })).toBeUndefined();
    expect(afterToggle(alone, { ...base, current: "", home: "/personas" })).toBeUndefined();
  });
});
