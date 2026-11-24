import type { Package, PersonaCard } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import {
  functionTitle,
  modelChoices,
  seatName,
  suggestPersona,
  unclaimedFunctions,
  withCurrent,
} from "./functions";

describe("職位與秘書", () => {
  // Guards: REQ-237
  test("卡片標題是職位：取職責的第一段，沒有職責用職位 id", () => {
    expect(functionTitle("個人秘書（生活、家庭、提醒、陪聊、學習）", "life")).toBe("個人秘書");
    expect(functionTitle("健康：飲食、運動、身體一起照顧（記錄、估算）", "health")).toBe("健康");
    expect(functionTitle("寫 code（讀 code、設計、review、CI）", "code")).toBe("寫 code");
    expect(functionTitle("", "system")).toBe("system");
    expect(functionTitle(undefined, "work")).toBe("work");
  });

  test("角色庫的「坐在 X」寫職位的名字，不寫秘書的顯示名（顯示名就是角色名）", () => {
    const agents = [
      { id: "code", duty: "寫 code（讀 code、設計）" },
      { id: "life", duty: "" },
    ];
    expect(seatName(agents, "code")).toBe("寫 code");
    expect(seatName(agents, "life")).toBe("life");
    expect(seatName(agents, "work")).toBe("work");
    expect(seatName(undefined, "work")).toBe("work");
  });

  // Guards: REQ-237
  test("模型下拉：名冊上的值不在清單裡也列出來", () => {
    expect(withCurrent(modelChoices, "sonnet")).toEqual(["sonnet", "opus", "fable", "haiku"]);
    expect(withCurrent(modelChoices, "claude-opus-5-5")).toEqual([
      "claude-opus-5-5",
      "sonnet",
      "opus",
      "fable",
      "haiku",
    ]);
    expect(withCurrent(modelChoices, "")).toEqual(["sonnet", "opus", "fable", "haiku"]);
  });
  // Guards: REQ-238
  test("還沒人負責的職位：只看啟用的套件，名冊上已有的、重複的都不列", () => {
    const pkgs = [
      {
        id: "secretaries",
        enabled: true,
        agents: [
          { id: "life", duty: "生活：家庭" },
          { id: "work", duty: "工作" },
        ],
      },
      { id: "health", enabled: true, agents: [{ id: "health", duty: "健康", color: "#3d8a1f" }] },
      { id: "off", enabled: false, agents: [{ id: "x1", duty: "停用的" }] },
      { id: "dup", enabled: true, agents: [{ id: "work", duty: "另一個工作" }] },
    ] as Package[];
    expect(unclaimedFunctions(pkgs, new Set(["life"]))).toEqual([
      { pkg: "secretaries", id: "work", duty: "工作", color: undefined, colorDark: undefined },
      { pkg: "health", id: "health", duty: "健康", color: "#3d8a1f", colorDark: undefined },
    ]);
  });

  // Guards: PKG-12, WEB-47
  test("選用職位（樣板 optional: true）帶 optional 標記，其他的不帶", () => {
    const pkgs = [
      { id: "secretaries", enabled: true, agents: [{ id: "work", duty: "工作" }] },
      {
        id: "admin",
        enabled: true,
        dev_only: true,
        agents: [{ id: "art", duty: "生圖", optional: true }],
      },
    ] as Package[];
    const got = unclaimedFunctions(pkgs, new Set());
    expect(got.map((f) => [f.pkg, f.id, f.optional])).toEqual([
      ["secretaries", "work", undefined],
      ["admin", "art", true],
    ]);
  });

  // 一般家沒有生圖秘書（DEV-160）：生圖 art 的樣板只在開發工坊 admin，一般家沒開它，可加的職位就沒有 art。
  // Guards: ADM-39
  test("開發工坊沒開的家（一般家），可加的職位沒有生圖 art", () => {
    const pkgs = [
      {
        id: "secretaries",
        enabled: true,
        agents: ["life", "work", "code", "system"].map((id) => ({ id, duty: id })),
      },
      {
        id: "admin",
        enabled: false,
        optional: true,
        dev_only: true,
        agents: [{ id: "art", duty: "生圖", optional: true }],
      },
    ] as Package[];
    expect(unclaimedFunctions(pkgs, new Set()).map((f) => f.id)).toEqual([
      "life",
      "work",
      "code",
      "system",
    ]);
  });

  // Guards: REQ-238
  test("認領時預設的角色：還沒坐位置的第一位", () => {
    const cards = [
      { slug: "a", seats: ["life"] },
      { slug: "b", seats: [] },
    ] as unknown as PersonaCard[];
    expect(suggestPersona(cards)).toBe("b");
    expect(suggestPersona([cards[0] as PersonaCard])).toBe("a");
    expect(suggestPersona([])).toBe("");
  });

  // Guards: REQ-326
  test("認領時預設的角色：職位自己帶的優先（先挑沒坐位置的），沒有才用別的", () => {
    const cards = [
      { slug: "b", source: "mine", seats: [] },
      { slug: "garden.fairy", source: "package", package: "garden", seats: ["x"] },
      { slug: "garden.elf", source: "package", package: "garden", seats: [] },
      { slug: "park.fairy", source: "package", package: "park", seats: [] },
    ] as unknown as PersonaCard[];
    expect(suggestPersona(cards, "garden")).toBe("garden.elf");
    expect(suggestPersona(cards.slice(0, 2), "garden")).toBe("garden.fairy");
    expect(suggestPersona(cards, "nothing")).toBe("b");
    expect(suggestPersona(cards)).toBe("b");
  });
});
