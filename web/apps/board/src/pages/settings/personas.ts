import { ApiError, type PersonaCard } from "@evergenie/sdk-web";
import type { TextKey } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";

/**
 * 新增自己的角色時填的代號：小寫英文和數字，最多 24 個字；伺服器會記成 `my_<代號>`（已經打了 my_ 也收）。
 * 業務規則仍以伺服器為準。
 */
export const slugPattern = /^(my_)?[a-z0-9]{1,24}$/;

export const styleLevels = ["a", "b", "c", "d", "custom"] as const;
export type StyleLevel = (typeof styleLevels)[number];

export const styleTitleKey = {
  a: "settings.personas.style.a.title",
  b: "settings.personas.style.b.title",
  c: "settings.personas.style.c.title",
  d: "settings.personas.style.d.title",
  custom: "settings.personas.style.custom.title",
} as const satisfies Record<StyleLevel, TextKey>;

export const styleDescKey = {
  a: "settings.personas.style.a.desc",
  b: "settings.personas.style.b.desc",
  c: "settings.personas.style.c.desc",
  d: "settings.personas.style.d.desc",
  custom: "settings.personas.style.custom.desc",
} as const satisfies Record<StyleLevel, TextKey>;

export type PersonaTag =
  | { kind: "official" }
  | { kind: "mine" }
  | { kind: "package"; pkg: string }
  | { kind: "unassignable"; reason: string }
  | { kind: "design" }
  | { kind: "shadowed" }
  | { kind: "seat"; agent: string };

/** 來源標籤：官方、我的，或來自哪個套件（source = "package"，SEC-302 S3）；沒看過的來源當「我的」。 */
function sourceTag(
  card: Pick<PersonaCard, "source"> & Partial<Pick<PersonaCard, "package">>,
): PersonaTag {
  if (card.source === "official") return { kind: "official" };
  if (card.source === "package") return { kind: "package", pkg: card.package ?? "" };
  return { kind: "mine" };
}

/** 一張卡的標籤，依序：來源（官方／我的／來自套件）、不能指派、設計中、坐在誰那裡（每位一個）、被官方蓋住。 */
export function tagsOf(
  card: Pick<PersonaCard, "source" | "status" | "seats" | "shadowed"> &
    Partial<Pick<PersonaCard, "package" | "unassignable">>,
) {
  const tags: PersonaTag[] = [sourceTag(card)];
  if (card.unassignable) tags.push({ kind: "unassignable", reason: card.unassignable });
  if (card.status === "design") tags.push({ kind: "design" });
  for (const agent of card.seats ?? []) tags.push({ kind: "seat", agent });
  if (card.shadowed) tags.push({ kind: "shadowed" });
  return tags;
}

/** 別名輸入：逗號（全形半形）或換行分開，去掉空白和重複。 */
export function parseAka(text: string): string[] {
  const out: string[] = [];
  for (const part of text.split(/[,，、\n]/)) {
    const v = part.trim();
    if (v !== "" && !out.includes(v)) out.push(v);
  }
  return out;
}

export interface NewCardInput {
  slug: string;
  name: string;
}
export interface NewCardErrors {
  slug?: TextKey;
  name?: TextKey;
}

/** 新增、複製表單的前端檢查：只看必填與格式（撞名、代號重複交給伺服器）。 */
export function validateNewCard(v: NewCardInput): NewCardErrors {
  const errors: NewCardErrors = {};
  const slug = v.slug.trim();
  if (slug === "") errors.slug = "settings.personas.form.slug.required";
  else if (!slugPattern.test(slug)) errors.slug = "settings.personas.form.slug.bad";
  if (v.name.trim() === "") errors.name = "settings.personas.form.name.required";
  return errors;
}

export function hasErrors(e: NewCardErrors): boolean {
  return e.slug !== undefined || e.name !== undefined;
}

/** 伺服器的錯誤對到表單欄位（FEN-19）：field 對得上就寫在那個欄位下，對不上才當整張表單的錯誤。 */
export function fieldError(
  err: unknown,
  fields: readonly string[],
): { field?: string; text: string } {
  const text = reasonOf(err);
  if (err instanceof ApiError && err.field && fields.includes(err.field)) {
    return { field: err.field, text };
  }
  return { text };
}

export type SaveFailure = { kind: "conflict" | "other"; text: string };

/** 存檔失敗：檔案在讀到之後被改過（樂觀鎖，conflict）要提示重新載入，其他照伺服器的白話原因。 */
export function saveFailure(err: unknown): SaveFailure {
  if (err instanceof ApiError && err.code === "conflict") {
    return { kind: "conflict", text: err.message };
  }
  return { kind: "other", text: reasonOf(err) };
}

/** 編輯器的內容跟伺服器上的卡不一樣嗎（名字、別名順序與內容、內文）。 */
export function isDirty(
  card: Pick<PersonaCard, "name" | "aka">,
  body: string,
  draft: { name: string; aka: string[]; body: string },
): boolean {
  const aka = card.aka ?? [];
  return (
    draft.name !== (card.name ?? "") ||
    draft.body !== body ||
    draft.aka.length !== aka.length ||
    draft.aka.some((a, i) => a !== aka[i])
  );
}

/** 角色下拉與角色庫的分組：官方、我的、各套件（依套件 id 排）；沒有卡的組不列。 */
export interface PersonaGroup {
  key: string;
  kind: "official" | "mine" | "package";
  /** kind 是 package 時：套件 id。 */
  pkg: string;
  cards: PersonaCard[];
}

export function groupPersonas(cards: readonly PersonaCard[]): PersonaGroup[] {
  const official: PersonaCard[] = [];
  const mine: PersonaCard[] = [];
  const byPkg = new Map<string, PersonaCard[]>();
  for (const c of cards) {
    if (c.source === "official") official.push(c);
    else if (c.source === "package") {
      const id = c.package ?? "";
      byPkg.set(id, [...(byPkg.get(id) ?? []), c]);
    } else mine.push(c);
  }
  const out: PersonaGroup[] = [];
  if (official.length > 0)
    out.push({ key: "official", kind: "official", pkg: "", cards: official });
  if (mine.length > 0) out.push({ key: "mine", kind: "mine", pkg: "", cards: mine });
  for (const id of [...byPkg.keys()].sort()) {
    out.push({ key: `package-${id}`, kind: "package", pkg: id, cards: byPkg.get(id) ?? [] });
  }
  return out;
}

/** 現在能指派給秘書的卡：已上線、沒被官方蓋住、所屬套件沒停用或被拒。 */
export function assignablePersonas(cards: readonly PersonaCard[]): PersonaCard[] {
  return cards.filter((c) => c.status === "live" && !c.shadowed && !c.unassignable);
}
