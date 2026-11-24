import type { MenuItem, MenuPrefEntry } from "@evergenie/sdk-web";

// 選單編輯器的純邏輯（REQ-141）：畫面只呼叫這些函式，樹的規則（最多兩層、項目只出現一次）都在這裡。
// 全部不改傳進來的樹，回傳新的；被擋下的操作回 { ok: false } 並原封不動帶回原本的樹。

export type Entry = MenuPrefEntry;

/** 永遠可達、不能移除的設定頁（schemas/menu-prefs.md）。 */
export const SETTINGS_ID = "core.settings";

export type EditFailure = "third_level" | "missing" | "empty_title";
export type EditResult =
  | { ok: true; tree: Entry[] }
  | { ok: false; reason: EditFailure; tree: Entry[] };

export const isGroup = (e: Entry): boolean => Boolean(e.group);

/** 頂層一項的 id：群組是群組 id，葉是項目 id。 */
export const entryId = (e: Entry): string => e.group || e.item || "";

function clone(tree: Entry[]): Entry[] {
  return tree.map((e) => (isGroup(e) ? { ...e, children: [...(e.children ?? [])] } : { ...e }));
}

export interface Where {
  /** 在哪個群組裡；頂層是 null。 */
  container: string | null;
  index: number;
}

/** 找一個項目或群組在樹的哪裡。 */
export function locate(tree: Entry[], id: string): Where | undefined {
  for (const [i, e] of tree.entries()) {
    if (entryId(e) === id) return { container: null, index: i };
    const at = (e.children ?? []).indexOf(id);
    if (isGroup(e) && at >= 0) return { container: e.group ?? null, index: at };
  }
  return undefined;
}

const hasGroup = (tree: Entry[], id: string) => tree.some((e) => e.group === id);

/** 從樹上拿掉一個項目或群組（群組連同底下的項目一起拿掉）。 */
function remove(tree: Entry[], id: string): Entry[] {
  const next = clone(tree);
  const w = locate(next, id);
  if (!w) return next;
  if (w.container === null) next.splice(w.index, 1);
  else next.find((e) => e.group === w.container)?.children?.splice(w.index, 1);
  return next;
}

/** 頂層的最後一項是 core.settings 就回它的位置（新東西要放在它前面）。 */
function topInsertIndex(tree: Entry[]): number {
  const last = tree.at(-1);
  return last && !isGroup(last) && last.item === SETTINGS_ID ? tree.length - 1 : tree.length;
}

/** 上移／下移一格（只在自己所在的那一層動）；visible 讓看不見的失效項目不佔位，點一下就真的動。 */
export function moveBy(
  tree: Entry[],
  id: string,
  delta: -1 | 1,
  visible: (id: string) => boolean = () => true,
): Entry[] {
  const w = locate(tree, id);
  if (!w) return tree;
  const next = clone(tree);
  const swapIn = <T>(list: T[], idOf: (x: T) => string): boolean => {
    let j = w.index + delta;
    while (j >= 0 && j < list.length && !visible(idOf(list[j] as T))) j += delta;
    const a = list[w.index];
    const b = list[j];
    if (j < 0 || j >= list.length || a === undefined || b === undefined) return false;
    list[w.index] = b;
    list[j] = a;
    return true;
  };
  const moved =
    w.container === null
      ? swapIn(next, entryId)
      : swapIn(next.find((e) => e.group === w.container)?.children ?? [], (c) => c);
  return moved ? next : tree;
}

/** 放進群組（附在群組最後）。群組不能放進群組：回 third_level，樹不變。 */
export function moveInto(tree: Entry[], id: string, group: string): EditResult {
  if (hasGroup(tree, id) || id === group) return { ok: false, reason: "third_level", tree };
  if (!hasGroup(tree, group)) return { ok: false, reason: "missing", tree };
  if (locate(tree, id)?.container === group) return { ok: true, tree };
  const next = remove(tree, id);
  next.find((e) => e.group === group)?.children?.push(id);
  return { ok: true, tree: next };
}

/** 項目移出群組：放在那個群組的正後面（頂層）。 */
export function moveOut(tree: Entry[], id: string): EditResult {
  const w = locate(tree, id);
  if (!w || w.container === null) return { ok: false, reason: "missing", tree };
  const groupAt = tree.findIndex((e) => e.group === w.container);
  const next = remove(tree, id);
  next.splice(groupAt + 1, 0, { item: id });
  return { ok: true, tree: next };
}

/** 還沒放進選單的項目放到最上層（core.settings 前面）。 */
export function moveToTop(tree: Entry[], id: string): EditResult {
  if (hasGroup(tree, id)) return { ok: false, reason: "missing", tree };
  const next = remove(tree, id);
  next.splice(topInsertIndex(next), 0, { item: id });
  return { ok: true, tree: next };
}

/**
 * 拖拉：把 id 放在 target 的前面（跟 target 同一層）。
 * 群組不能放進別的群組裡面（target 在群組裡面時，群組被擋下，回 third_level）。
 */
export function moveBefore(tree: Entry[], id: string, target: string): EditResult {
  if (id === target) return { ok: true, tree };
  const targetAt = locate(tree, target);
  if (!targetAt) return { ok: false, reason: "missing", tree };
  const moving = tree.find((e) => e.group === id);
  if (moving && targetAt.container !== null) return { ok: false, reason: "third_level", tree };
  const next = remove(tree, id);
  const at = locate(next, target);
  if (!at) return { ok: false, reason: "missing", tree };
  if (at.container === null) {
    next.splice(
      at.index,
      0,
      moving ? { ...moving, children: [...(moving.children ?? [])] } : { item: id },
    );
  } else {
    next.find((e) => e.group === at.container)?.children?.splice(at.index, 0, id);
  }
  return { ok: true, tree: next };
}

export function renameGroup(tree: Entry[], id: string, title: string): EditResult {
  const name = title.trim();
  if (!hasGroup(tree, id)) return { ok: false, reason: "missing", tree };
  if (name === "") return { ok: false, reason: "empty_title", tree };
  return { ok: true, tree: tree.map((e) => (e.group === id ? { ...e, title: name } : e)) };
}

export function setGroupIcon(tree: Entry[], id: string, icon: string): Entry[] {
  return tree.map((e) => (e.group === id ? { ...e, icon } : e));
}

/** 刪掉群組：底下的項目從樹上消失，回到「沒放進選單」。 */
export function deleteGroup(tree: Entry[], id: string): Entry[] {
  return hasGroup(tree, id) ? remove(tree, id) : tree;
}

/** 新增空群組（放在 core.settings 前面）；群組 id 自動取一個沒用過的 groupN。 */
export function addGroup(tree: Entry[], title: string): Entry[] {
  const used = new Set(tree.filter(isGroup).map((e) => e.group));
  let n = 1;
  while (used.has(`group${n}`)) n++;
  const next = clone(tree);
  next.splice(topInsertIndex(next), 0, { group: `group${n}`, title, icon: "folder", children: [] });
  return next;
}

/** 可用項目裡沒放進樹的（core.settings 不算：它永遠在）。 */
export function unplaced(pool: MenuItem[], tree: Entry[]): MenuItem[] {
  return pool.filter((it) => it.id !== SETTINGS_ID && it.id && !locate(tree, it.id));
}

/** 伺服器的偏好 → 編輯器的樹：core.settings 不在樹裡就補在最後（跟畫面上一樣）。 */
export function fromPrefs(tree: Entry[] | undefined): Entry[] {
  const next = clone(tree ?? []);
  if (!locate(next, SETTINGS_ID)) next.push({ item: SETTINGS_ID });
  return next;
}

/** 編輯器的樹 → 要存的偏好：結尾的 core.settings 不存（伺服器本來就補在最後，新項目的「未分類」才排在它前面）。 */
export function toPrefs(tree: Entry[]): Entry[] {
  const next = clone(tree);
  const last = next.at(-1);
  if (last && !isGroup(last) && last.item === SETTINGS_ID) next.pop();
  return next;
}

export function sameTree(a: Entry[], b: Entry[]): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/** 樹上依序出現的項目 id（群組底下的依序展開）。 */
export function itemOrder(tree: Entry[]): string[] {
  return tree.flatMap((e) => (isGroup(e) ? (e.children ?? []) : e.item ? [e.item] : []));
}

function sectionOf(pool: MenuItem[]): Map<string, string> {
  return new Map(
    pool.filter((it) => it.id && it.section).map((it) => [it.id ?? "", it.section ?? ""] as const),
  );
}

/**
 * 同一頁的項目（manifest 的 `section`，例 健康＝飲食、運動、身體）在選單上是一個入口（REQ-235），
 * 編輯器也當成一列來搬（REQ-237）：每個 section 以 anchor（剛搬的那一項）或樹上最前面那一項為準，
 * 其他項目照原本的先後接在它正後面（同一層）；還沒放進樹的同伴也一起放進去（剛打開的那一項回到同一頁）。
 * 沒有任何一項在樹上就不動。
 */
export function gatherSections(tree: Entry[], pool: MenuItem[], anchor?: string): Entry[] {
  const sec = sectionOf(pool);
  let next = clone(tree);
  for (const section of new Set(sec.values())) {
    const order = itemOrder(next).filter((id) => sec.get(id) === section);
    const lead = anchor !== undefined && order.includes(anchor) ? anchor : order[0];
    if (lead === undefined) continue;
    const others = [
      ...order.filter((id) => id !== lead),
      ...pool
        .map((it) => it.id ?? "")
        .filter((id) => sec.get(id) === section && !order.includes(id)),
    ];
    for (const id of others) next = remove(next, id);
    const w = locate(next, lead);
    if (!w) continue;
    if (w.container === null) next.splice(w.index + 1, 0, ...others.map((item) => ({ item })));
    else next.find((e) => e.group === w.container)?.children?.splice(w.index + 1, 0, ...others);
  }
  return next;
}

/** 合成一列時不另外畫的項目：每個 section 除了第一項（依 ids 的順序）以外的那些。 */
export function sectionFollowers(ids: string[], pool: MenuItem[]): Set<string> {
  const sec = sectionOf(pool);
  const seen = new Set<string>();
  const out = new Set<string>();
  for (const id of ids) {
    const s = sec.get(id);
    if (!s) continue;
    if (seen.has(s)) out.add(id);
    else seen.add(s);
  }
  return out;
}

/** 一列代表的同一頁分頁名稱（依 pool 的順序）；不是 section 的項目回空陣列。 */
export function sectionParts(id: string, pool: MenuItem[]): string[] {
  const s = sectionOf(pool).get(id);
  if (!s) return [];
  return pool.filter((it) => it.section === s).map((it) => it.title || it.id || "");
}

/** 今天是首頁，收不得（設定是 SETTINGS_ID，同樣收不得；規則跟伺服器的 tuckable 一樣，MNU-10）。 */
export const OVERVIEW_ID = "core.overview";

/** 這一項和同一頁的同伴（同 section 的項目在選單上是一列，要收一起收）。 */
function withSection(id: string, pool: MenuItem[]): string[] {
  const section = pool.find((it) => it.id === id)?.section;
  if (!section) return [id];
  return pool.filter((it) => it.section === section).map((it) => it.id ?? "");
}

export function tuckItem(hidden: string[], id: string, pool: MenuItem[]): string[] {
  return [...new Set([...hidden, ...withSection(id, pool)])];
}

export function untuckItem(hidden: string[], id: string, pool: MenuItem[]): string[] {
  const out = new Set(withSection(id, pool));
  return hidden.filter((h) => !out.has(h));
}

export function sameHidden(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((id) => b.includes(id));
}
