// 這個家支援什麼（衣櫃的程式每一輪寫的 closet.capabilities/main，衣櫃卡 CLO-32）：頁面只照它決定畫什麼，
// 不自己判斷是哪種家（不問 system.get_mode，衣櫃卡 CLO-37、closet-mine 卡 CMI-24）。
// 套件不能互相 import 程式，這裡抄衣櫃 pages/lib/caps.ts 用得到的那一小段；名字跟衣櫃一致由衣櫃的 Go 測試
// TestCapNamesMatchSchemaAndPages 一起守。讀紀錄的 loadCapabilities 在 store.ts。
export const CAPABILITIES = "closet.capabilities";
/** 只有開發家：整套與單張的狀態、評論、篩選、放回開發中、升成官方（都在衣櫃的角色頁「修改」分頁）。 */
export const CAP_EDIT = "edit";

/** 這個家支援的能力（closet.capabilities 的 supports）；還沒讀到、讀不到、封存了都是空的（fail closed）。 */
export function toSupports(
  r: { fields?: Record<string, unknown>; archived?: boolean } | null | undefined,
): string[] {
  if (!r || r.archived) return [];
  const v = r.fields?.supports;
  return Array.isArray(v)
    ? v.map((x) => (typeof x === "string" ? x.trim() : "")).filter((x) => x !== "")
    : [];
}

/**
 * 這個家能不能修改衣服（edit，開發家才有）：「衣服」分頁只用來決定標不標設計中、下架，下架的那套算不算刪掉的（CMI-24）。
 * 還沒讀到（undefined）當不能。
 */
export function canEdit(supports: readonly string[] | undefined): boolean {
  return supports?.includes(CAP_EDIT) === true;
}
