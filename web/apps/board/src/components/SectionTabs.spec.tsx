import type { Menu, MenuItem, Package } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { menuQuery, packageListQuery } from "../data/queries";
import {
  CoreSectionTabs,
  isCoreItem,
  sectionHome,
  sectionOf,
  sectionTitleOf,
  withSectionTitles,
} from "./SectionTabs";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children, ...rest }: { to: string; children: ReactNode }) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
  useNavigate: () => () => {},
}));

afterEach(cleanup);

// 伺服器排好的 pool：核心項目在前（介紹在核心裡），套件項目照 order。
const core: MenuItem = {
  id: "core.personas",
  title: "介紹",
  route: "/personas",
  badge: 0,
  section: "personas",
  section_title: "角色",
};
const wardrobe: MenuItem = {
  id: "closet.wardrobe",
  title: "衣櫃",
  route: "/p/closet/wardrobe",
  badge: 0,
  section: "personas",
};
const showroom: MenuItem = {
  id: "closet.showroom",
  title: "展示間",
  route: "/p/closet/showroom",
  badge: 0,
  section: "personas",
};
const pool: MenuItem[] = [
  { id: "core.overview", title: "今天", route: "/", badge: 0 },
  core,
  { id: "core.settings", title: "設定", route: "/settings", badge: 0 },
  { id: "nutrition.log", title: "飲食", route: "/p/nutrition/log", badge: 0, section: "health" },
  wardrobe,
  showroom,
];

// 衣櫃套件（停用時選單沒有它的項目，但套件清單還列著，才開得回來）。
const closetPkg = {
  id: "closet",
  enabled: false,
  menu: [
    { id: "wardrobe", section: "personas", order: 40 },
    { id: "showroom", section: "personas", order: 41 },
  ],
} as unknown as Package;

function renderTabs(items: MenuItem[], packages: Package[] = []) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(menuQuery().queryKey, { pool: items, tree: [] } as Menu);
  client.setQueryData(packageListQuery().queryKey, { packages } as never);
  return render(
    <QueryClientProvider client={client}>
      <CoreSectionTabs route="/personas" />
    </QueryClientProvider>,
  );
}

describe("核心頁加入 section（角色頁，WEB-136、WEB-137）", () => {
  test("同 section 的分頁：核心頁在前、再照套件的順序；名稱用寫了的 section_title；核心頁是退路", () => {
    const siblings = sectionOf(pool, "/p/closet/showroom");
    expect(siblings.map((it) => it.id)).toEqual([
      "core.personas",
      "closet.wardrobe",
      "closet.showroom",
    ]);
    expect(sectionOf(pool, "/personas")).toEqual(siblings);
    expect(sectionTitleOf(siblings)).toBe("角色");
    expect(sectionTitleOf([wardrobe, showroom])).toBeUndefined();
    expect(sectionHome(siblings)).toBe("/personas");
    expect(sectionHome([wardrobe, showroom])).toBeUndefined();
    expect(isCoreItem(core)).toBe(true);
    expect(isCoreItem(wardrobe)).toBe(false);
  });

  test("介紹上方畫三個分頁（目前是介紹）與「顯示設定」開關", () => {
    renderTabs(pool);
    const nav = screen.getByRole("navigation", { name: /角色/ });
    const links = Array.from(nav.querySelectorAll("a"));
    expect(links.map((a) => a.textContent)).toEqual(["介紹", "衣櫃", "展示間"]);
    expect(links.map((a) => a.getAttribute("href"))).toEqual([
      "/personas",
      "/p/closet/wardrobe",
      "/p/closet/showroom",
    ]);
    expect(links[0]?.getAttribute("aria-current")).toBe("page");
    expect(links[1]?.getAttribute("aria-current")).toBeNull();
    // 開關的鈕叫「顯示設定」（DEV-156 U-15：以前叫「項目」，看不出是開關哪些分頁）。
    expect(screen.getByTestId("section-toggles").textContent).toBe("顯示設定");
  });

  test("衣櫃停用、只剩介紹時不畫只有一格的分頁列，但留「顯示設定」開關讓衣櫃開得回來", () => {
    renderTabs(
      pool.filter((it) => !it.id?.startsWith("closet.")),
      [closetPkg],
    );
    expect(screen.queryByRole("navigation")).toBeNull();
    expect(screen.getByTestId("section-toggles")).toBeTruthy();
  });

  test("section 裡沒有任何套件（衣櫃移除了）時什麼都不畫", () => {
    const { container } = renderTabs(pool.filter((it) => !it.id?.startsWith("closet.")));
    expect(container.innerHTML).toBe("");
  });

  // 存過選單、衣櫃排在介紹前面時，選單編輯器拿衣櫃當那一列的代表：那一列也要叫「角色」，跟側欄一樣。
  test("withSectionTitles：同 section 沒寫 section_title 的項目補上 section 的名稱", () => {
    const titled = withSectionTitles(pool);
    const byId = new Map(titled.map((it) => [it.id, it]));
    expect(byId.get("closet.wardrobe")?.section_title).toBe("角色");
    expect(byId.get("closet.showroom")?.section_title).toBe("角色");
    expect(byId.get("nutrition.log")?.section_title).toBeUndefined();
    expect(byId.get("core.overview")?.section_title).toBeUndefined();
    expect(withSectionTitles([wardrobe, showroom])[0]?.section_title).toBeUndefined();
  });
});
