import type { Menu } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import { mergeMenuBadges } from "./siteBadges";

const menu = (badges: Record<string, number>, extra: Menu["pool"] = []): Menu => {
  const pool = Object.entries(badges).map(([id, badge]) => ({ id, title: id, route: id, badge }));
  return {
    pool: [...pool, ...extra],
    tree: [
      { id: "core.overview", route: "/", badge: badges["core.overview"] ?? 0 },
      {
        id: "g.daily",
        title: "日常",
        badge: 0,
        children: [
          { id: "core.messages", route: "/messages", badge: badges["core.messages"] ?? 0 },
          { id: "health.log", route: "/p/health/log", badge: 2 },
        ],
      },
    ],
  };
};

describe("選單徽章加總各台", () => {
  test("TestMenuBadgesAddOtherSitesCoreItems：核心項目加上別台的數字，樹與 pool 都改", () => {
    const own = menu({ "core.overview": 2, "core.messages": 1 });
    const b = menu({ "core.overview": 3, "core.messages": 4 }, [
      { id: "health.log", route: "/p/health/log", badge: 5 },
    ]);
    const merged = mergeMenuBadges(own, [b]);
    expect(merged.tree?.[0]?.badge).toBe(5);
    expect(merged.tree?.[1]?.children?.[0]?.badge).toBe(5);
    // 套件項目不合併（套件頁照台分開，計畫設計決定 4）。
    expect(merged.tree?.[1]?.children?.[1]?.badge).toBe(2);
    expect(merged.pool?.find((i) => i.id === "core.overview")?.badge).toBe(5);
  });

  test("TestMenuBadgesUnchangedWithOneSite：沒有別台（或別台都是 0、還沒讀到）回原本的物件", () => {
    const own = menu({ "core.overview": 2 });
    expect(mergeMenuBadges(own, [])).toBe(own);
    expect(mergeMenuBadges(own, [undefined, menu({ "core.overview": 0 })])).toBe(own);
  });
});
