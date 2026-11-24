import type { Package, PersonaCard } from "@evergenie/sdk-web";
import { EFFORT_LEVELS, MODEL_CHOICES } from "../../lib/modelChoices";

// 設定 → 職位與秘書的純邏輯（REQ-237）：每張卡以「職位」（名冊上的位置，例 life、health）為主，
// 標題取職責的第一段（「個人秘書（生活、家庭…）」→「個人秘書」、「健康：飲食…」→「健康」），沒有職責就用職位 id。

export function functionTitle(duty: string | undefined, id: string): string {
  const head = (duty ?? "").split(/[（(：:，,]/)[0]?.trim() ?? "";
  return head || id;
}

/**
 * 角色庫的「坐在 X」（WEB-107）：寫職位的名字（跟「職位與秘書」的卡同一個標題），不寫秘書的顯示名——
 * 秘書的顯示名就是她坐的角色名，寫在那張角色卡上只會變成「Builder 坐在 Builder」。
 */
export function seatName(
  agents: readonly { id?: string; duty?: string }[] | undefined,
  id: string,
): string {
  return functionTitle(agents?.find((a) => a.id === id)?.duty, id);
}

/** 預設模型的選項（引擎認得的別名；名冊上寫了別的也照樣列出來）。 */
export const modelChoices = MODEL_CHOICES;

/** 思考力度的選項；空字串＝沒指定（引擎自己決定）。 */
export const effortChoices = ["", ...EFFORT_LEVELS] as const;

/** 下拉的選項：目前的值不在清單裡（名冊被改過）也要列出來，免得看起來像「沒設定」。 */
export function withCurrent(list: readonly string[], current: string | undefined): string[] {
  return current && !list.includes(current) ? [current, ...list] : [...list];
}

/**
 * 套件帶來、名冊上還沒有的職位（REQ-238）：只看啟用中的套件；同一個職位 id 只列第一個。
 * optional＝選用職位（樣板 optional: true，DEV-157）：設定精靈不列，「職位與秘書」頁另一組標「選用」。
 */
export interface UnclaimedFunction {
  pkg: string;
  id: string;
  duty: string;
  color?: string;
  colorDark?: string;
  optional?: true;
}

export function unclaimedFunctions(
  packages: readonly Package[],
  taken: ReadonlySet<string>,
): UnclaimedFunction[] {
  const out: UnclaimedFunction[] = [];
  const seen = new Set(taken);
  for (const p of packages) {
    if (!p.enabled || p.state === "rejected" || !p.id) continue;
    for (const a of p.agents ?? []) {
      if (!a.id || seen.has(a.id)) continue;
      seen.add(a.id);
      out.push({
        pkg: p.id,
        id: a.id,
        duty: a.duty ?? "",
        color: a.color,
        colorDark: a.color_dark,
        ...(a.optional ? { optional: true as const } : {}),
      });
    }
  }
  return out;
}

/**
 * 認領時預設選的角色：職位自己帶的上線角色（pkg 是那個職位的套件 id；還沒坐位置的優先）→
 * 還沒坐任何位置的第一位 → 第一位。
 */
export function suggestPersona(cards: readonly PersonaCard[], pkg?: string): string {
  const own = pkg ? cards.filter((c) => c.source === "package" && c.package === pkg) : [];
  const pool = own.length > 0 ? own : cards;
  return (pool.find((c) => (c.seats ?? []).length === 0) ?? pool[0])?.slug ?? "";
}
