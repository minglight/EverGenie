import { describe, expect, test } from "vitest";
import { type FilterAgent, mergeFilterSites, mergeSiteRows, personCount } from "./filterSites";
import type { Site } from "./sites";

const site = (key: string) => ({ key, primary: key === "" }) as Site;
const agent = (s: string, id: string, extra: Partial<FilterAgent> = {}): FilterAgent => ({
  key: s ? `${s}/${id}` : id,
  id,
  name: id,
  duty: "",
  presence: "idle",
  ...extra,
});

describe("秘書篩選把兩台同一個職位合成一個頭像（作者 2026-11-02）", () => {
  test("同一個職位 id 併成一位：名字與狀態用第一台的（不合成）、有一台工作中就亮光圈、頭像缺了用別台補", () => {
    const { people, down } = mergeFilterSites([
      { site: site(""), agents: [agent("", "code", { name: "雅典娜", presence: "working" })] },
      {
        site: site("office"),
        agents: [
          agent("office", "code", { name: "別台", presence: "waiting", avatarUrl: "/b.png" }),
          agent("office", "work"),
        ],
      },
    ]);
    expect(down).toEqual([]);
    expect(people.map((p) => p.keys)).toEqual([["code", "office/code"], ["office/work"]]);
    const [code] = people;
    if (!code) throw new Error("沒有合併後的 code");
    // 一台工作、一台等你：不合成「比較要緊」的那個，工作中的光圈照亮。
    expect(code).toMatchObject({
      name: "雅典娜",
      presence: "working",
      working: true,
      avatarUrl: "/b.png",
    });
    expect(code.sites.map((s) => s.key)).toEqual(["", "office"]);
    expect(personCount(code, { code: 2, "office/code": 3, "office/work": 9 })).toBe(5);
  });

  test("沒有秘書的台（連不到、名冊還沒到）列進 down，畫面寫一行灰字", () => {
    const { people, down } = mergeFilterSites([
      { site: site(""), agents: [agent("", "code")] },
      { site: site("office"), agents: [] },
    ]);
    expect(people.map((p) => p.keys)).toEqual([["code"]]);
    expect(down.map((s) => s.key)).toEqual(["office"]);
  });
});

describe("今天的秘書卡片：一位一張（mergeSiteRows）", () => {
  const row = (s: string, id: string, n: number, presence: "idle" | "working" | "waiting") => ({
    id,
    key: s ? `${s}/${id}` : id,
    site: site(s),
    n,
    presence,
    data: { from: s },
  });

  test("兩台同一個職位合成一張：key 與資料用第一台的、件數加總、記下兩台與每台各自的狀態", () => {
    const cards = mergeSiteRows([
      row("", "code", 1, "idle"),
      row("", "life", 0, "working"),
      row("office", "code", 2, "waiting"),
      row("office", "work", 3, "idle"),
    ]);
    expect(cards.map((c) => [c.key, c.n, c.presence, c.sites.map((s) => s.key)])).toEqual([
      ["code", 3, "idle", ["", "office"]],
      ["life", 0, "working", [""]],
      ["office/work", 3, "idle", ["office"]],
    ]);
    expect(cards[0]?.data).toEqual({ from: "" });
    // 每台各自的狀態都留著（卡片一台一行，合成一個就有一台看不到）。
    expect(cards[0]?.each.map((e) => [e.site.key, e.presence])).toEqual([
      ["", "idle"],
      ["office", "waiting"],
    ]);
  });

  test("只有一台時跟以前一樣：每位一張", () => {
    const cards = mergeSiteRows([row("", "a", 1, "idle"), row("", "b", 0, "idle")]);
    expect(cards.map((c) => c.key)).toEqual(["a", "b"]);
    expect(cards.every((c) => c.sites.length === 1)).toBe(true);
  });
});
