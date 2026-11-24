import type { Menu, MenuEntry, MenuItem } from "@evergenie/sdk-web";
import { useQueries } from "@tanstack/react-query";
import { useMemo } from "react";
import { menuQuery } from "../data/queries";
import { dataOf, useSites } from "../data/sites";

// 選單徽章加總各台（S3-7a；計畫 2026-11-01-board-multi-home 設計決定 5）：選單本身照主伺服器的（設定、套件不合併），
// 核心項目（core.*：今天、聊天、訊息、任務…，這幾頁是合併的）的徽章加上別台同一個項目的數字。數字都是各台伺服器算的。
// 只看連得上（ok）的台；只有一台時回原本的選單物件，什麼都不變。

const isCore = (id: string | undefined) => (id ?? "").startsWith("core.");

/** 別台的核心徽章：項目 id → 各台相加的數字（pool 一個項目一筆，不會重複算）。 */
export function otherBadges(others: readonly (Menu | undefined)[]): Map<string, number> {
  const out = new Map<string, number>();
  for (const m of others) {
    for (const item of m?.pool ?? []) {
      if (!isCore(item.id) || !item.badge) continue;
      out.set(item.id ?? "", (out.get(item.id ?? "") ?? 0) + item.badge);
    }
  }
  return out;
}

function addEntry(e: MenuEntry, extra: Map<string, number>): MenuEntry {
  const plus = isCore(e.id) ? (extra.get(e.id ?? "") ?? 0) : 0;
  const children = e.children?.map((c) => addEntry(c, extra));
  if (plus === 0 && children === e.children) return e;
  return { ...e, badge: e.badge + plus, ...(children ? { children } : {}) };
}

function addItem(item: MenuItem, extra: Map<string, number>): MenuItem {
  const plus = isCore(item.id) ? (extra.get(item.id ?? "") ?? 0) : 0;
  return plus === 0 ? item : { ...item, badge: item.badge + plus };
}

/** 主伺服器的選單加上別台的核心徽章；別台都沒有數字時回同一個物件。 */
export function mergeMenuBadges(menu: Menu, others: readonly (Menu | undefined)[]): Menu {
  const extra = otherBadges(others);
  if (extra.size === 0) return menu;
  return {
    ...menu,
    tree: menu.tree?.map((e) => addEntry(e, extra)),
    pool: menu.pool?.map((i) => addItem(i, extra)),
  };
}

/** 外殼用：主伺服器的選單＋各台（ok 的）核心徽章。 */
export function useMergedMenu(menu: Menu): Menu {
  const sites = useSites();
  const others = useMemo(() => sites.filter((s) => !s.primary && s.state === "ok"), [sites]);
  const menus = useQueries({ queries: others.map((s) => menuQuery(s)), combine: dataOf });
  return useMemo(() => mergeMenuBadges(menu, menus), [menu, menus]);
}
