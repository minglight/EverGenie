import type { MenuEntry } from "@evergenie/sdk-web";

export interface FlatItem {
  id: string;
  title: string;
  icon: string;
  route: string;
  badge: number;
  /** 所在的群組；頂層項目沒有。 */
  group?: { id: string; title: string };
  /** 這個入口涵蓋的網址：一般項目只有自己；section 合成的入口是每個分頁（REQ-235）。 */
  routes: string[];
  /** 套件宣告的「同一頁」（例 health）；合成之後的入口也帶著。 */
  section?: string;
  /** section 合成的入口裡每個分頁的名稱與網址（例「飲食」「運動」「身體」）：指令面板打分頁名找得到（WEB-101）。 */
  parts?: { title: string; route: string }[];
}

export interface MenuSection {
  /** 群組名稱；頂層項目的區段沒有名稱。 */
  title?: string;
  items: FlatItem[];
}

/** 手機底部列最多放幾格（含「更多」）。 */
export const BAR_SLOTS = 6;

function leaf(e: MenuEntry, group?: FlatItem["group"]): FlatItem {
  return {
    id: e.id ?? "",
    title: e.title ?? "",
    icon: e.icon ?? "",
    route: e.route ?? "",
    badge: e.badge,
    group,
    routes: [e.route ?? ""],
    section: e.section || undefined,
  };
}

/**
 * 同一個群組裡 section 相同的項目合成一個入口（REQ-235）：放在第一個的位置，名稱是 section_title（沒有就用第一個的名稱），
 * 網址是第一個分頁，徽章相加，`routes` 列出每個分頁（在任何一個分頁都算「目前」）。
 */
function mergeSections(
  entries: MenuEntry[],
  group?: FlatItem["group"],
  bySection = new Map<string, FlatItem>(),
  titled = new Set<string>(),
): FlatItem[] {
  const out: FlatItem[] = [];
  for (const e of entries) {
    const item = leaf(e, group);
    const key = e.section ?? "";
    const merged = key ? bySection.get(key) : undefined;
    if (merged) {
      merged.routes.push(item.route);
      merged.parts?.push({ title: item.title, route: item.route });
      merged.badge += item.badge;
      // 第一個沒寫 section_title（套件可以跟著核心的名稱不寫）：用後面寫了的（WEB-136）。
      if (!titled.has(key) && e.section_title) {
        merged.title = e.section_title;
        titled.add(key);
      }
      continue;
    }
    if (key) {
      item.parts = [{ title: item.title, route: item.route }];
      if (e.section_title) titled.add(key);
      item.title = e.section_title || item.title;
      item.id = `section.${key}`;
      bySection.set(key, item);
    }
    out.push(item);
  }
  return out;
}

/** 把選單樹依序攤平成項目清單（群組底下的項目依序展開，群組本身不算項目）。 */
export function flattenTree(tree: MenuEntry[] | undefined): FlatItem[] {
  const out: FlatItem[] = [];
  // 頂層的項目也一樣合（REQ-237：選單編輯器把「健康」整組移到頂層時，側欄還是一個入口）。
  const top = new Map<string, FlatItem>();
  const topTitled = new Set<string>();
  for (const e of tree ?? []) {
    if (e.children && e.children.length > 0) {
      out.push(...mergeSections(e.children, { id: e.id ?? "", title: e.title ?? "" }));
    } else if (e.route) {
      out.push(...mergeSections([e], undefined, top, topTitled));
    }
  }
  return out;
}

/** 底部列的切法：總數 ≤ 6 全部放；否則放前 5 個，其餘進「更多」。 */
export function splitBar(items: FlatItem[]): { bar: FlatItem[]; rest: FlatItem[] } {
  if (items.length <= BAR_SLOTS) return { bar: items, rest: [] };
  return { bar: items.slice(0, BAR_SLOTS - 1), rest: items.slice(BAR_SLOTS - 1) };
}

/** 「更多」裡分群組列出：同一個群組的項目放在一起，頂層項目各自獨立不帶標題。 */
export function sectionsOf(items: FlatItem[]): MenuSection[] {
  const sections: MenuSection[] = [];
  for (const item of items) {
    const last = sections[sections.length - 1];
    if (item.group && last?.title === item.group.title) {
      last.items.push(item);
    } else if (item.group) {
      sections.push({ title: item.group.title, items: [item] });
    } else if (last && last.title === undefined) {
      last.items.push(item);
    } else {
      sections.push({ items: [item] });
    }
  }
  return sections;
}

/** 這個入口是不是目前的頁面：它涵蓋的任何一個網址對上就算（section 合成的入口有好幾個）。 */
export function isActiveItem(item: Pick<FlatItem, "route" | "routes">, pathname: string): boolean {
  return (item.routes.length > 0 ? item.routes : [item.route]).some((r) =>
    isActiveRoute(r, pathname),
  );
}

/**
 * 選單拿來比「目前」的網址（WEB-100）：有項目涵蓋這個網址就照原樣；沒有、而它是套件頁（`/p/<套件>/<頁>`）時，
 * 算在同一個套件的第一個入口上（例「飲食趨勢」`/p/nutrition/trend` 算「健康」、「訓練計畫」算「健康」），
 * 不讓選單與手機底部列一個都沒亮。
 */
export function menuPathFor(items: Pick<FlatItem, "route" | "routes">[], pathname: string): string {
  if (items.some((i) => isActiveItem(i, pathname))) return pathname;
  const pkg = /^\/p\/[^/]+\//.exec(pathname)?.[0];
  if (!pkg) return pathname;
  const owner = items.find((i) =>
    (i.routes.length > 0 ? i.routes : [i.route]).some((r) => r.startsWith(pkg)),
  );
  return owner?.route ?? pathname;
}

/** 這個網址屬於哪個項目：根網址只對上 "/"，其他用前綴（/settings/notify 屬於 /settings）。 */
export function isActiveRoute(route: string, pathname: string): boolean {
  if (route === "/") return pathname === "/";
  return pathname === route || pathname.startsWith(`${route}/`);
}
