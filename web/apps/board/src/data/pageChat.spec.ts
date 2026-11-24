import { describe, expect, test } from "vitest";
import { sectionOf } from "../components/SectionTabs";
import { chatAgentFor, parsePicks } from "./pageChat";

describe("套件頁旁邊的聊天與分頁", () => {
  // Guards: REQ-235
  test("跟誰聊：選過的優先、其次頁面的預設秘書，不在名冊或停用的不算", () => {
    const on = (id: string) => ["fitness", "life"].includes(id);
    expect(chatAgentFor({}, "health", "fitness", on)).toBe("fitness");
    expect(chatAgentFor({ health: "life" }, "health", "fitness", on)).toBe("life");
    // 選過的秘書被停用了：退回預設；預設也不在：空字串（讓主人選）。
    expect(chatAgentFor({ health: "nutrition" }, "health", "fitness", on)).toBe("fitness");
    expect(chatAgentFor({}, "habits.list", "", on)).toBe("");
    expect(chatAgentFor({}, "x", "nutrition", on)).toBe("");
    // localStorage 壞掉的內容當作沒選過。
    expect(parsePicks("not json")).toEqual({});
    expect(parsePicks('["a"]')).toEqual({});
    expect(parsePicks('{"health":"life","bad":3}')).toEqual({ health: "life" });
  });

  // Guards: REQ-235
  test("同一頁的分頁：同 section 的選單項目依選單順序；沒有 section 就沒有分頁", () => {
    const pool = [
      { id: "core.chat", route: "/chat", badge: 0 },
      {
        id: "nutrition.log",
        route: "/p/nutrition/log",
        badge: 0,
        section: "health",
        section_title: "健康",
      },
      { id: "habits.list", route: "/p/habits/list", badge: 0 },
      {
        id: "fitness.log",
        route: "/p/fitness/log",
        badge: 0,
        section: "health",
        section_title: "健康",
      },
      {
        id: "body.log",
        route: "/p/body/log",
        badge: 0,
        section: "health",
        section_title: "健康",
      },
    ];
    expect(sectionOf(pool, "/p/body/log").map((it) => it.id)).toEqual([
      "nutrition.log",
      "fitness.log",
      "body.log",
    ]);
    expect(sectionOf(pool, "/p/habits/list")).toEqual([]);
    expect(sectionOf(undefined, "/p/fitness/log")).toEqual([]);
  });
});
