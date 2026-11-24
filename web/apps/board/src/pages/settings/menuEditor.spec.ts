import type { MenuItem } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import {
  addGroup,
  deleteGroup,
  type Entry,
  fromPrefs,
  gatherSections,
  itemOrder,
  moveBefore,
  moveBy,
  moveInto,
  moveOut,
  moveToTop,
  renameGroup,
  sameTree,
  sectionFollowers,
  sectionParts,
  toPrefs,
  unplaced,
} from "./menuEditor";

const tree = (): Entry[] => [
  { group: "daily", title: "日常", icon: "sun", children: ["core.overview", "core.chat"] },
  { group: "work", title: "工作", icon: "briefcase", children: ["core.tasks"] },
  { item: "core.messages" },
  { item: "core.settings" },
];

const ids = (t: Entry[]) => t.map((e) => e.group ?? e.item);
const kids = (t: Entry[], g: string) => t.find((e) => e.group === g)?.children ?? [];

describe("menuEditor", () => {
  // Guards: REQ-141
  test("TestGroupIntoGroupRejected：把群組放進群組被擋下，樹不變", () => {
    const before = tree();
    const res = moveInto(before, "work", "daily");
    expect(res.ok).toBe(false);
    if (!res.ok) expect(res.reason).toBe("third_level");
    expect(res.tree).toBe(before);
    expect(sameTree(res.tree, tree())).toBe(true);
    // 拖拉也一樣：把群組放到群組裡的項目前面。
    const drag = moveBefore(before, "work", "core.chat");
    expect(drag.ok).toBe(false);
    expect(sameTree(drag.tree, tree())).toBe(true);
    // 群組放進自己也不行。
    expect(moveInto(before, "daily", "daily").ok).toBe(false);
  });

  // Guards: REQ-141
  test("TestMoveUpDown：頂層與群組裡各自上移下移，到頭不動", () => {
    expect(ids(moveBy(tree(), "work", -1))).toEqual([
      "work",
      "daily",
      "core.messages",
      "core.settings",
    ]);
    expect(ids(moveBy(tree(), "daily", 1))).toEqual([
      "work",
      "daily",
      "core.messages",
      "core.settings",
    ]);
    expect(kids(moveBy(tree(), "core.chat", -1), "daily")).toEqual(["core.chat", "core.overview"]);
    expect(kids(moveBy(tree(), "core.overview", 1), "daily")).toEqual([
      "core.chat",
      "core.overview",
    ]);
    const t = tree();
    expect(moveBy(t, "daily", -1)).toBe(t);
    expect(moveBy(t, "core.settings", 1)).toBe(t);
    expect(moveBy(t, "core.chat", 1)).toBe(t);
    // 看不見的失效項目不佔位：跳過它找下一個看得見的。
    const stale = [{ item: "a" }, { item: "gone" }, { item: "b" }];
    expect(ids(moveBy(stale, "b", -1, (id) => id !== "gone"))).toEqual(["b", "gone", "a"]);
  });

  // Guards: REQ-141
  test("TestDeleteGroupReturnsToUnplaced：刪群組後底下的項目回到沒放進選單", () => {
    const pool: MenuItem[] = [
      "core.overview",
      "core.chat",
      "core.tasks",
      "core.messages",
      "core.settings",
    ].map((id) => ({ id, title: id, badge: 0 }));
    expect(unplaced(pool, tree()).map((i) => i.id)).toEqual([]);
    const after = deleteGroup(tree(), "daily");
    expect(ids(after)).toEqual(["work", "core.messages", "core.settings"]);
    expect(unplaced(pool, after).map((i) => i.id)).toEqual(["core.overview", "core.chat"]);
  });

  // Guards: REQ-141
  test("放進群組、移出群組、放到最上層", () => {
    const into = moveInto(tree(), "core.messages", "work");
    expect(into.ok && kids(into.tree, "work")).toEqual(["core.tasks", "core.messages"]);
    expect(into.ok && ids(into.tree)).toEqual(["daily", "work", "core.settings"]);
    // 從 daily 搬到 work：只出現在 work。
    const moved = moveInto(tree(), "core.chat", "work");
    expect(moved.ok && kids(moved.tree, "daily")).toEqual(["core.overview"]);
    expect(moved.ok && kids(moved.tree, "work")).toEqual(["core.tasks", "core.chat"]);
    // 移出群組：放在那個群組的正後面。
    const out = moveOut(tree(), "core.overview");
    expect(out.ok && ids(out.tree)).toEqual([
      "daily",
      "core.overview",
      "work",
      "core.messages",
      "core.settings",
    ]);
    expect(out.ok && kids(out.tree, "daily")).toEqual(["core.chat"]);
    expect(moveOut(tree(), "core.messages").ok).toBe(false);
    // 還沒放進樹的項目放最上層：在 core.settings 前面。
    const top = moveToTop(tree(), "demo.home");
    expect(top.ok && ids(top.tree)).toEqual([
      "daily",
      "work",
      "core.messages",
      "demo.home",
      "core.settings",
    ]);
    // 拖拉：放在目標前面，跟目標同一層。
    const before = moveBefore(tree(), "core.messages", "core.chat");
    expect(before.ok && kids(before.tree, "daily")).toEqual([
      "core.overview",
      "core.messages",
      "core.chat",
    ]);
    const group = moveBefore(tree(), "work", "daily");
    expect(group.ok && ids(group.tree)).toEqual([
      "work",
      "daily",
      "core.messages",
      "core.settings",
    ]);
  });

  // Guards: REQ-141
  test("改名、新增群組（id 不重複、放在 core.settings 前面）與空名稱被擋", () => {
    const renamed = renameGroup(tree(), "work", "  公事 ");
    expect(renamed.ok && renamed.tree[1]?.title).toBe("公事");
    expect(renameGroup(tree(), "work", "   ").ok).toBe(false);
    const once = addGroup(tree(), "新群組");
    const twice = addGroup(once, "新群組");
    expect(ids(twice)).toEqual([
      "daily",
      "work",
      "core.messages",
      "group1",
      "group2",
      "core.settings",
    ]);
    expect(kids(twice, "group2")).toEqual([]);
  });

  // Guards: REQ-141
  test("fromPrefs／toPrefs：core.settings 補在最後，存的時候結尾那個不存", () => {
    const saved: Entry[] = [{ group: "g", title: "G", children: ["a"] }];
    const shown = fromPrefs(saved);
    expect(ids(shown)).toEqual(["g", "core.settings"]);
    expect(sameTree(toPrefs(shown), saved)).toBe(true);
    // 使用者把 core.settings 放在中間或群組裡就照存。
    const mid = fromPrefs([{ item: "core.settings" }, { group: "g", title: "G", children: ["a"] }]);
    expect(ids(toPrefs(mid))).toEqual(["core.settings", "g"]);
    expect(ids(fromPrefs(undefined))).toEqual(["core.settings"]);
  });
});

describe("menuEditor：同一頁的項目合成一列", () => {
  const health = (id: string, title: string): MenuItem => ({
    id,
    title,
    icon: "",
    route: `/p/${id}/log`,
    badge: 0,
    section: "health",
    section_title: "健康",
  });
  const pool: MenuItem[] = [
    { id: "core.overview", title: "今天", icon: "", route: "/", badge: 0 },
    { id: "core.chat", title: "聊天", icon: "", route: "/chat", badge: 0 },
    health("nutrition.log", "飲食"),
    health("fitness.log", "運動"),
    health("body.log", "身體"),
  ];

  // Guards: REQ-237
  test("散在兩個群組、有一項沒放進樹：以最前面那項為準，其他接在它後面", () => {
    const before: Entry[] = [
      { group: "daily", title: "日常", icon: "sun", children: ["core.overview", "fitness.log"] },
      { group: "life", title: "生活", icon: "heart", children: ["core.chat", "nutrition.log"] },
      { item: "core.settings" },
    ];
    const got = gatherSections(before, pool);
    expect(kids(got, "daily")).toEqual([
      "core.overview",
      "fitness.log",
      "nutrition.log",
      "body.log",
    ]);
    expect(kids(got, "life")).toEqual(["core.chat"]);
    // 畫面上只畫第一項；它代表的分頁依 pool 的順序。
    expect([...sectionFollowers(itemOrder(got), pool)]).toEqual(["nutrition.log", "body.log"]);
    expect(sectionParts("fitness.log", pool)).toEqual(["飲食", "運動", "身體"]);
    expect(sectionParts("core.chat", pool)).toEqual([]);
  });

  // Guards: REQ-237
  test("搬那一列＝整組一起搬：下移、放進群組、移出群組（以剛搬的那項為準）", () => {
    const start = gatherSections(
      [
        { group: "daily", title: "日常", icon: "sun", children: ["core.overview", "core.chat"] },
        { group: "work", title: "工作", icon: "briefcase", children: [] },
        { item: "nutrition.log" },
        { item: "core.settings" },
      ],
      pool,
    );
    expect(ids(start)).toEqual([
      "daily",
      "work",
      "nutrition.log",
      "fitness.log",
      "body.log",
      "core.settings",
    ]);
    const followers = sectionFollowers(itemOrder(start), pool);
    const visible = (id: string) => !followers.has(id);
    // 下移一格：跳過同伴，跟 core.settings 換位置，同伴跟著走。
    const down = gatherSections(moveBy(start, "nutrition.log", 1, visible), pool, "nutrition.log");
    expect(ids(down)).toEqual([
      "daily",
      "work",
      "core.settings",
      "nutrition.log",
      "fitness.log",
      "body.log",
    ]);
    const into = moveInto(start, "nutrition.log", "daily");
    expect(into.ok).toBe(true);
    const inDaily = gatherSections(into.tree, pool, "nutrition.log");
    expect(kids(inDaily, "daily")).toEqual([
      "core.overview",
      "core.chat",
      "nutrition.log",
      "fitness.log",
      "body.log",
    ]);
    expect(ids(inDaily)).toEqual(["daily", "work", "core.settings"]);
    const out = moveOut(inDaily, "nutrition.log");
    expect(ids(gatherSections(out.tree, pool, "nutrition.log"))).toEqual([
      "daily",
      "nutrition.log",
      "fitness.log",
      "body.log",
      "work",
      "core.settings",
    ]);
  });
});
