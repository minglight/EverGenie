// 一位角色一頁（DEV-143）的分頁：核心的「介紹」在最前面，後面是套件掛在 slot `core.persona.tab` 的頁面（標題由套件給）。
// 網址 /personas/<代號>?tab=<分頁>：介紹不寫 tab，套件的分頁寫它的頁面 id（`<套件>.<頁面>`）。

/** 套件在一位角色的頁上掛分頁的位置（`[[contributes.page]] slot = "core.persona.tab"`）。 */
export const SLOT_PERSONA_TAB = "core.persona.tab";
/** 套件在角色清單「新增角色」裡掛新增方式的位置（`slot = "core.personas.new"`）。 */
export const SLOT_PERSONAS_NEW = "core.personas.new";
/** 核心「介紹」分頁的 key；網址上不寫。 */
export const INTRO_TAB = "intro";

export interface PersonaTab {
  key: string;
  label: string;
}

/** 分頁清單：介紹在前，再照 slot 的順序（slot_front 在前、再依 id，`pkg/Slot` 的 slotPages 排好）。 */
export function personaTabs(
  introLabel: string,
  pages: readonly { id?: string; title?: string }[],
): PersonaTab[] {
  return [
    { key: INTRO_TAB, label: introLabel },
    ...pages.filter((p) => p.id).map((p) => ({ key: p.id ?? "", label: p.title || (p.id ?? "") })),
  ];
}

/**
 * 現在是哪個分頁：網址的 tab 對得到就是它；沒寫、寫 intro、或那個套件已經關掉（對不到）都回介紹。
 * pagesLoaded = false（套件頁清單還沒讀到）而且網址指名套件的分頁時回 null：先顯示載入中，不先閃一下介紹。
 */
export function currentPersonaTab(
  tabs: readonly PersonaTab[],
  wanted: string | undefined,
  pagesLoaded: boolean,
): string | null {
  if (!wanted || wanted === INTRO_TAB) return INTRO_TAB;
  if (tabs.some((x) => x.key === wanted)) return wanted;
  return pagesLoaded ? INTRO_TAB : null;
}

/**
 * 套件分頁裡打開某一項時的網址查詢（?tab=<分頁>&item=<那一項>）：item 是分頁的頁面自己定的 id，核心不解讀；
 * 空字串＝關掉，不寫 item。介紹分頁沒有 item。
 */
export function itemSearch(tab: string | undefined, item: string): { tab?: string; item?: string } {
  if (!tab || tab === INTRO_TAB) return {};
  return item ? { tab, item } : { tab };
}

/** 分頁的網址查詢：介紹不寫 tab。 */
export function tabSearch(key: string): { tab?: string } {
  return key === INTRO_TAB ? {} : { tab: key };
}
