import type { MenuEntry } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import {
  flattenTree,
  isActiveItem,
  isActiveRoute,
  menuPathFor,
  sectionsOf,
  splitBar,
} from "./flatten";

const item = (id: string, route = `/${id}`): MenuEntry => ({ id, title: id, route, badge: 0 });
const group = (id: string, ...children: MenuEntry[]): MenuEntry => ({
  id,
  title: `群組 ${id}`,
  badge: 0,
  children,
});

// F6 以前的預設樹（六項）：daily（今天、聊天、待辦、訊息）、work（任務單）、設定；用來測「六項以內全放」的切法。
const defaultTree: MenuEntry[] = [
  group("daily", item("overview", "/"), item("chat"), item("reminders"), item("messages")),
  group("work", item("tasks")),
  item("settings"),
];

// 現在的預設樹（F6、REQ-201）：任務單在日常、套件項目（group_hint = daily）接在它後面；工作＝權限申請、Goal。
const newDefaultTree = (...packages: MenuEntry[]): MenuEntry[] => [
  group(
    "daily",
    item("overview", "/"),
    item("chat"),
    item("reminders"),
    item("messages"),
    item("tasks"),
    ...packages,
  ),
  group("work", item("policy"), item("goals")),
  item("settings"),
];

describe("選單攤平與「更多」的切法", () => {
  // Guards: REQ-201
  test("現在的預設樹：前 5 格是今天、聊天、待辦、訊息、任務單，其餘進「更多」", () => {
    const { bar, rest } = splitBar(flattenTree(newDefaultTree()));
    expect(bar.map((i) => i.id)).toEqual(["overview", "chat", "reminders", "messages", "tasks"]);
    expect(rest.map((i) => i.id)).toEqual(["policy", "goals", "settings"]);
  });

  // Guards: REQ-201
  test("裝了一堆套件，任務單仍在第 5 格（套件項目只會排在後面）", () => {
    const pkgs = [item("habits.log"), item("fitness.log"), item("nutrition.log"), item("closet")];
    const { bar, rest } = splitBar(flattenTree(newDefaultTree(...pkgs)));
    expect(bar.map((i) => i.id)).toEqual(["overview", "chat", "reminders", "messages", "tasks"]);
    expect(rest.map((i) => i.id).slice(0, 4)).toEqual([
      "habits.log",
      "fitness.log",
      "nutrition.log",
      "closet",
    ]);
  });

  // Guards: REQ-105
  test("群組依序展開，群組本身不算一格", () => {
    const ids = flattenTree(defaultTree).map((i) => i.id);
    expect(ids).toEqual(["overview", "chat", "reminders", "messages", "tasks", "settings"]);
  });

  // Guards: REQ-105
  test("預設樹 6 項：全部放底部列，任務單是第 5 格，沒有「更多」", () => {
    const { bar, rest } = splitBar(flattenTree(defaultTree));
    expect(bar.map((i) => i.id)).toEqual([
      "overview",
      "chat",
      "reminders",
      "messages",
      "tasks",
      "settings",
    ]);
    expect(rest).toEqual([]);
  });

  // Guards: REQ-105
  test("總數超過 6 個：前 5 個放底部列，其餘進「更多」", () => {
    const seven = flattenTree([...defaultTree, item("extra")]);
    const { bar, rest } = splitBar(seven);
    expect(bar.map((i) => i.id)).toEqual(["overview", "chat", "reminders", "messages", "tasks"]);
    expect(rest.map((i) => i.id)).toEqual(["settings", "extra"]);
  });

  // Guards: REQ-105
  test("總數 ≤ 6 個：全部放，沒有「更多」", () => {
    const six = flattenTree([item("a"), item("b"), item("c"), item("d"), item("e"), item("f")]);
    expect(splitBar(six)).toEqual({ bar: six, rest: [] });
    const two = flattenTree([item("a"), item("b")]);
    expect(splitBar(two).rest).toEqual([]);
  });

  // Guards: REQ-106
  test("設定被放在最後也找得到：6 項以內在底部列、超過就在「更多」裡", () => {
    const six = splitBar(flattenTree(defaultTree));
    expect(six.bar.some((i) => i.id === "settings")).toBe(true);
    const { rest } = splitBar(flattenTree([...defaultTree, item("extra")]));
    expect(rest.some((i) => i.id === "settings")).toBe(true);
  });

  // Guards: REQ-105
  test("「更多」分群組列出，頂層項目不帶標題", () => {
    const { rest } = splitBar(flattenTree([...defaultTree, item("extra")]));
    expect(sectionsOf(rest).map((s) => [s.title, s.items.map((i) => i.id)])).toEqual([
      [undefined, ["settings", "extra"]],
    ]);
  });

  // Guards: REQ-105
  test("同一個群組被底部列切開時，剩下的部分仍留在同名群組", () => {
    const tree = [
      group("g", item("a"), item("b"), item("c")),
      group("h", item("d"), item("e"), item("f")),
    ];
    const { bar, rest } = splitBar(flattenTree([...tree, group("i", item("g"))]));
    expect(bar.map((i) => i.id)).toEqual(["a", "b", "c", "d", "e"]);
    expect(sectionsOf(rest).map((s) => [s.title, s.items.map((i) => i.id)])).toEqual([
      ["群組 h", ["f"]],
      ["群組 i", ["g"]],
    ]);
  });

  // Guards: REQ-107
  test("目前頁面：根網址只對上自己，其他用前綴", () => {
    expect(isActiveRoute("/", "/")).toBe(true);
    expect(isActiveRoute("/", "/chat")).toBe(false);
    expect(isActiveRoute("/settings", "/settings/notify")).toBe(true);
    expect(isActiveRoute("/chat", "/chat/code")).toBe(true);
    expect(isActiveRoute("/chat", "/chatty")).toBe(false);
  });

  // Guards: REQ-235
  test("同一個 section 的套件項目合成一個入口：名稱用 section_title、徽章相加、每個分頁都算目前", () => {
    const sec = (id: string, badge: number): MenuEntry => ({
      ...item(id, `/p/${id.replace(".", "/")}`),
      badge,
      section: "health",
      section_title: "健康",
    });
    const items = flattenTree(
      newDefaultTree(
        sec("nutrition.log", 1),
        item("habits.list"),
        sec("fitness.log", 0),
        sec("body.log", 2),
      ),
    );
    const health = items.find((i) => i.section === "health");
    expect(items.filter((i) => i.section === "health")).toHaveLength(1);
    expect(health).toMatchObject({
      id: "section.health",
      title: "健康",
      route: "/p/nutrition/log",
      badge: 3,
      routes: ["/p/nutrition/log", "/p/fitness/log", "/p/body/log"],
    });
    // 合成的入口排在第一個分頁原本的位置。
    expect(items.map((i) => i.id).slice(5, 7)).toEqual(["section.health", "habits.list"]);
    expect(isActiveItem(health as never, "/p/body/log")).toBe(true);
    expect(isActiveItem(health as never, "/p/habits/list")).toBe(false);
    // 每個分頁原本的名字與網址都留著，指令面板打「飲食」這種分頁名才找得到（WEB-101）。
    expect(health?.parts).toEqual([
      { title: "nutrition.log", route: "/p/nutrition/log" },
      { title: "fitness.log", route: "/p/fitness/log" },
      { title: "body.log", route: "/p/body/log" },
    ]);
    expect(items.find((i) => i.id === "habits.list")?.parts).toBeUndefined();
  });

  // Guards: REQ-237
  test("放在頂層的同一個 section 也合成一個入口", () => {
    const sec = (id: string): MenuEntry => ({
      ...item(id, `/p/${id.replace(".", "/")}`),
      section: "health",
      section_title: "健康",
    });
    const items = flattenTree([sec("nutrition.log"), sec("fitness.log"), item("core.settings")]);
    expect(items.map((i) => i.id)).toEqual(["section.health", "core.settings"]);
    expect(items[0]?.routes).toEqual(["/p/nutrition/log", "/p/fitness/log"]);
  });

  test("套件裡沒放進選單的頁（飲食趨勢、訓練計畫）算在同一個套件的入口上，選單不會一個都沒亮", () => {
    const sec = (id: string): MenuEntry => ({
      ...item(id, `/p/${id.replace(".", "/")}`),
      section: "health",
      section_title: "健康",
    });
    const items = flattenTree(
      newDefaultTree(
        sec("nutrition.log"),
        sec("fitness.log"),
        item("habits.list", "/p/habits/list"),
      ),
    );
    const health = items.find((i) => i.section === "health");
    const at = menuPathFor(items, "/p/nutrition/trend");
    expect(isActiveItem(health as never, at)).toBe(true);
    expect(isActiveItem(health as never, menuPathFor(items, "/p/fitness/plan"))).toBe(true);
    // 有項目涵蓋的網址照原樣；不是套件頁、也沒有對上的套件時也照原樣。
    expect(menuPathFor(items, "/p/habits/list")).toBe("/p/habits/list");
    expect(menuPathFor(items, "/settings/notify")).toBe("/settings/notify");
    expect(menuPathFor(items, "/p/unknown/x")).toBe("/p/unknown/x");
  });

  // 「角色」頁（DEV-117、WEB-136）：核心的介紹與套件的衣櫃、展示間同 section，合成一個入口，連到第一個分頁（核心）；
  // 套件沒寫 section_title 時跟著核心的名稱。
  test("核心頁與套件頁同 section 合成一個入口：名稱用寫了的 section_title、網址是核心頁", () => {
    const core: MenuEntry = {
      ...item("core.personas", "/personas"),
      title: "介紹",
      section: "personas",
      section_title: "角色",
    };
    const pkg = (id: string, title: string): MenuEntry => ({
      ...item(id, `/p/${id.replace(".", "/")}`),
      title,
      section: "personas",
    });
    const items = flattenTree(
      newDefaultTree(core, pkg("closet.wardrobe", "衣櫃"), pkg("closet.showroom", "展示間")),
    );
    const merged = items.filter((i) => i.section === "personas");
    expect(merged).toHaveLength(1);
    expect(merged[0]).toMatchObject({ id: "section.personas", title: "角色", route: "/personas" });
    expect(merged[0]?.parts?.map((p) => p.title)).toEqual(["介紹", "衣櫃", "展示間"]);
    // 第一個沒寫 section_title（使用者的樹把衣櫃排在前面）：名稱用後面寫了的。
    const swapped = flattenTree(newDefaultTree(pkg("closet.wardrobe", "衣櫃"), core));
    expect(swapped.find((i) => i.section === "personas")?.title).toBe("角色");
    // 只剩核心一個（衣櫃停用）：還是一個入口，名稱照 section_title。
    const alone = flattenTree(newDefaultTree(core));
    expect(alone.find((i) => i.section === "personas")).toMatchObject({
      title: "角色",
      route: "/personas",
    });
  });
});
