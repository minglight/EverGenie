// 這個家支援什麼（closet.capabilities/main，CLO-32）與別的套件登記的動作（CLO-33）：衣櫃的程式算好，頁面只照它畫，
// 不自己判斷是哪種家（不問 system.get_mode）。動作的鈕、狀態、篩選都從這裡讀；衣櫃不處理動作寫出去的請求，由登記的套件自己做。
// 這裡只放純函式；讀紀錄的 loadCapabilities 在 store.ts。
export const CAPABILITIES = "closet.capabilities";
/** 每個家都有：自己的衣服（新增／刪一套、上傳／刪單張、要不要用點數換）。 */
export const CAP_MINE = "mine";
/** 只有開發家：整套與單張的狀態、評論、篩選（「修改」分頁）。 */
export const CAP_EDIT = "edit";
/** 有 edit 而且「修改」分頁沒關（擴充點 closet.edit-tab）：角色頁有「修改」分頁，連到它的入口才出現。 */
export const CAP_EDIT_TAB = "edit-tab";

export type ActionScope = "outfit" | "photo";
export type RequestField = "persona" | "outfit" | "photo";
type Scalar = string | number | boolean;

/** 一個登記好、這個家撐得起的動作（衣櫃的程式驗過才會在清單裡）。 */
export interface ClosetAction {
  id: string;
  /** 登記的套件 id。 */
  package: string;
  label: string;
  scope: ActionScope;
  order: number;
  /** 按下去寫哪個種類的請求（`<套件>.<種類>`）。 */
  requestKind: string;
  requestFields: RequestField[];
  /** 請求另外固定帶的欄位。 */
  requestSet: Record<string, Scalar>;
  /** 狀態：看 kind 的 field，值照 labels 換成白話；沒有＝不顯示狀態。 */
  status?: { kind: string; field: string; labels: Record<string, string> };
  /** 篩選鈕：只留狀態是 values 的；沒有＝不給篩選。 */
  filter?: { label: string; values: string[] };
}

export interface Capabilities {
  supports: string[];
  actions: ClosetAction[];
}

/** 還沒算好、封存了：什麼都不支援（fail closed）。 */
export const NO_CAPABILITIES: Capabilities = { supports: [], actions: [] };

/** 動作的目標：哪位角色的哪一套（或哪一張）。 */
export interface ActionTarget {
  persona: string;
  /** closet.outfit 的編號。 */
  outfit?: string;
  /** 照片編號（scope = photo 才有）。 */
  photo?: string;
}

const text = (v: unknown): string => (typeof v === "string" ? v.trim() : "");
const strings = (v: unknown): string[] =>
  Array.isArray(v) ? v.map(text).filter((s) => s !== "") : [];
const isObj = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);
const REQUEST_FIELDS: readonly string[] = ["persona", "outfit", "photo"];
/** 衣櫃自己與 closet-mine（官方套件，不能登記動作）：頁面寫得進它們的請求，所以它們的名字一律不收（DEV-160 審查 S4）。 */
const OWN_PACKAGES: readonly string[] = ["closet", "closet-mine"];

/**
 * 頁面也再驗一次動作是不是那個套件自己的（DEV-160 審查 S4；程式那邊照 schemas/action.toml 驗過，這裡不信紀錄）：
 * 編號以「<package>-」開頭、request_kind 與 status_kind（有寫的話）以「<package>.」開頭，套件不是衣櫃或 closet-mine。
 * 不然一筆寫壞或偽造的 closet.capabilities 能讓主人按一顆鈕就寫別的套件（例如 closet-mine.request）的請求。
 */
function ownedByPackage(pkg: string, id: string, requestKind: string, statusKind: string): boolean {
  if (!pkg || OWN_PACKAGES.includes(pkg)) return false;
  const named = (s: string, sep: string) => s.startsWith(pkg + sep) && s.length > pkg.length + 1;
  return named(id, "-") && named(requestKind, ".") && (statusKind === "" || named(statusKind, "."));
}

function toAction(v: unknown): ClosetAction | null {
  if (!isObj(v)) return null;
  const id = text(v.id);
  const scope = text(v.scope);
  const requestKind = text(v.request_kind);
  if (!ownedByPackage(text(v.package), id, requestKind, text(v.status_kind))) return null;
  const requestFields = strings(v.request_fields).filter((f): f is RequestField =>
    REQUEST_FIELDS.includes(f),
  );
  if (!id || !text(v.label) || (scope !== "outfit" && scope !== "photo") || !requestKind) {
    return null;
  }
  if (requestFields.length === 0) return null;
  const requestSet: Record<string, Scalar> = {};
  if (isObj(v.request_set)) {
    for (const [k, x] of Object.entries(v.request_set)) {
      if (typeof x === "string" || typeof x === "number" || typeof x === "boolean") {
        requestSet[k] = x;
      }
    }
  }
  const a: ClosetAction = {
    id,
    package: text(v.package),
    label: text(v.label),
    scope,
    order: typeof v.order === "number" ? v.order : 0,
    requestKind,
    requestFields,
    requestSet,
  };
  const statusKind = text(v.status_kind);
  const statusField = text(v.status_field);
  if (statusKind && statusField && isObj(v.status_labels)) {
    const labels: Record<string, string> = {};
    for (const [k, x] of Object.entries(v.status_labels)) {
      if (text(x)) labels[k] = text(x);
    }
    a.status = { kind: statusKind, field: statusField, labels };
    const filterLabel = text(v.filter_label);
    if (filterLabel) a.filter = { label: filterLabel, values: strings(v.filter_values) };
  }
  return a;
}

/** 讀 closet.capabilities/main 這筆紀錄；沒有、封存了都當什麼都不支援。動作照程式排好的順序（scope、order、編號）。 */
export function toCapabilities(
  r: { fields?: Record<string, unknown>; archived?: boolean } | null | undefined,
): Capabilities {
  if (!r || r.archived) return NO_CAPABILITIES;
  const f = r.fields ?? {};
  const actions = Array.isArray(f.actions) ? f.actions.map(toAction) : [];
  return {
    supports: strings(f.supports),
    actions: actions.filter((a): a is ClosetAction => a !== null),
  };
}

export function hasCap(c: Capabilities, name: string): boolean {
  return c.supports.includes(name);
}

/**
 * 這個家能不能修改衣服（edit，開發家才有）：頁面要「只有開發家才有」的東西（沒兌換也看得到真圖的說明、篩選、到「修改」的入口）
 * 一律問這個，不問是哪種家（CLO-37）。還沒讀到、讀不到（undefined）當不能（fail closed）。
 */
export function canEdit(c: Capabilities | undefined): boolean {
  return c !== undefined && hasCap(c, CAP_EDIT);
}

/** 角色頁有沒有「修改」分頁（連過去的入口照這個決定出不出現；關掉時連過去會是一頁空的）。 */
export function canEditTab(c: Capabilities | undefined): boolean {
  return c !== undefined && hasCap(c, CAP_EDIT_TAB);
}

/** 對整套（outfit）或單張（photo）的動作。 */
export function actionsFor(c: Capabilities, scope: ActionScope): ClosetAction[] {
  return c.actions.filter((a) => a.scope === scope);
}

/** 按下去要寫的請求欄位：request_set 加上 request_fields 照目標填；目標缺了要的欄位回 null（不給按）。 */
export function actionRequest(a: ClosetAction, t: ActionTarget): Record<string, Scalar> | null {
  const out: Record<string, Scalar> = { ...a.requestSet };
  for (const f of a.requestFields) {
    const v = t[f];
    if (!v) return null;
    out[f] = v;
  }
  return out;
}

/**
 * 這個目標現在的狀態值：在 status.kind 的紀錄裡找 request_fields 都對得上目標的那幾筆（不含封存），取 updated_at 最新的，
 * 回它 status.field 的值；沒有狀態、沒找到回空字串。白話用 statusLabel。
 */
export function actionStatus(
  a: ClosetAction,
  records: readonly { fields?: Record<string, unknown>; archived?: boolean; updated_at?: string }[],
  t: ActionTarget,
): string {
  if (!a.status) return "";
  let best: { at: string; value: string } | null = null;
  for (const r of records) {
    if (r.archived) continue;
    const f = r.fields ?? {};
    if (!a.requestFields.every((k) => t[k] && text(f[k]) === t[k])) continue;
    const at = r.updated_at ?? "";
    if (!best || at > best.at) best = { at, value: text(f[a.status.field]) };
  }
  return best?.value ?? "";
}

/** 狀態值的白話（沒列在 status_labels 的不顯示，回空字串）。 */
export function statusLabel(a: ClosetAction, value: string): string {
  return (value && a.status?.labels[value]) || "";
}

/** 篩選鈕按著時這個目標留不留：狀態是 filter.values 之一才留。沒有篩選的動作一律留。 */
export function filterKeeps(a: ClosetAction, value: string): boolean {
  return !a.filter || a.filter.values.includes(value);
}
