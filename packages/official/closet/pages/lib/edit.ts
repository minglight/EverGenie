// 開發家的「修改」分頁（DEV-160 S6，CLO-35）：一位角色所有衣服的每一張照片、所有狀態，能改狀態、能評論。
// 這裡只放純函式與字：照片（closet.photo）、整套（closet.outfit）、評論（closet.note）怎麼讀成畫面要的樣子、篩選、排序、
// 照片的網址、評論寫出的欄位與傳給生圖秘書的話。讀寫紀錄在 store.ts，畫面在 editParts.tsx 與 ../edit.tsx。
// 這頁不判斷是哪種家：分頁出不出現由核心看 show_when = "cap:closet.edit"（CLO-32）。外掛動作的鈕與篩選只照 closet.capabilities（CLO-33）。
import {
  type ActionTarget,
  actionStatus,
  type Capabilities,
  type ClosetAction,
  filterKeeps,
  NO_CAPABILITIES,
} from "./caps";
import { type Person, pickerPeople } from "./closet";

export const T = {
  loading: "載入中…",
  loadWhat: "衣服與照片",
  filterGroup: "篩選",
  scopeGroup: "看哪位角色",
  allPersonas: "所有角色",
  official: "官方",
  wip: "開發中",
  showHidden: "顯示隱藏版",
  emptyTitle: "這位角色還沒有照片紀錄",
  emptyHint: "每張照片登記好之後會列在這裡，可以一張一張採用、改成隱藏版、評論。",
  emptyAllTitle: "還沒有任何角色的照片紀錄",
  noMatch: "沒有符合篩選的衣服",
  noMatchHint: "把上面的篩選關掉幾個看看。",
  outfitNoPhotos: "這套還沒有照片。",
  hiddenOut: (n: number) => `還有 ${n} 張隱藏版沒列出來（打開「顯示隱藏版」看）。`,
  stateDraft: "開發中",
  stateActive: "上架中",
  stateRetired: "已下架",
  sourceOfficial: "官方",
  sourceMine: "我的",
  publish: "上架",
  retire: "下架",
  publishNeed: (n: number) => `還有 ${n} 張沒採用，每張都採用了才能上架。`,
  publishNeedOff: (n: number) => `其中 ${n} 張是隱藏版，隱藏版開關打開才列得出來。`,
  publishNotAll: (n: number) => `還有 ${n} 張沒採用，每張都採用了才能上架；畫面重新整理後再看。`,
  adoptedCount: (done: number, all: number) => `已採用 ${done}／${all} 張`,
  toDraft: "放回開發中",
  draftMineOnly: "只有自己的衣服（不是基本款）能放回開發中；官方的狀態跟著角色包。",
  photoChanged: "這張剛換過，請再看一次",
  adopt: "採用",
  unadopt: "取消採用",
  adopted: "已採用",
  notAdopted: "開發中",
  levelGroup: "一般版或隱藏版",
  levelStd: "一般版",
  levelHidden: "隱藏版",
  typeGroup: "類型",
  half: "半身",
  full: "全身",
  video: "影片",
  zoom: "看大圖",
  // 隱藏版開關關著的隱藏版整張不列（hidden_off，S3a），不會走到這裡。
  noUrl: "這張現在沒有公開的圖（同一版已經有全身照、檔不在，或剛改過、衣櫃還沒重新發佈）",
  levelFollows: "一般／隱藏跟著配的半身照",
  ownerHiddenShort: "自己的隱藏版，不能在這裡改成一般版",
  ownerToHiddenHint: "自己的照片改成隱藏版後，這頁就不能再改回一般版",
  clipLags: "跟配的半身照不同版，衣櫃不會公開它",
  clipSync: "跟上半身",
  fullTakenShort: "這一版已經有全身照",
  otherFullTakenShort: "另一版已經有全身照",
  comment: "評論",
  commentLabel: "評論",
  commentPlaceholder: "想跟生圖秘書說什麼？（例：「臉再柔和一點」「這張改成全身」「先留著」）",
  commentSend: "送出評論",
  commentCancel: "先不要",
  commentEmpty: "請先寫一句評論",
  commentSent: "評論已送出",
  commentWarn: "評論已記下，但叫不到生圖秘書，請你直接跟秘書說一聲。",
  noArt: "這個家沒有啟用中的生圖秘書；",
  noArtKept: "評論照樣記下來，秘書上班後看得到。",
  noArtLink: "到職位與秘書看看",
  notesTitle: (n: number) => `評論紀錄（${n}）`,
  noteOpen: "等秘書處理",
  noteRedone: "已重做",
  noteDismissed: "已處理",
  noteResult: "秘書：",
  actionFailed: "沒送出去：",
  photoGone: "這張照片的紀錄不見了，畫面重新整理後再看。",
  outfitGone: "這套衣服的紀錄不見了，畫面重新整理後再看。",
  failed: "沒改成：",
  noId: "沒有拿到新的編號",
  actionMissing: "這顆鈕缺了要帶的欄位，先不能按。",
  retypeHasVideo: "這張配了影片（影片只配半身照），不能改成全身。",
  videoLevelFollows: "影片的一般／隱藏跟著配的那張半身照，請改那張。",
  ownerHiddenStays:
    "這張是自己的隱藏版（跟著「自己的隱藏版」開關），不能在這裡改成一般版：改了就會跟一般版一起公開、匯出。",
  fullTaken: "那一版已經有全身照了（衣櫃一版只放一張全身照）。要換，先把原本那張改成半身或另一版。",
  videoPaired: "配了影片，不能改成全身",
};

// ---- 讀紀錄 ----

export type PhotoType = "half" | "full" | "video";

/** 一筆 closet.photo（欄位見 schemas/photo.toml）。 */
export interface EditPhoto {
  id: string;
  persona: string;
  /** closet.outfit 的編號（<人設>-<衣服 id>）。 */
  outfit: string;
  type: PhotoType;
  order: number;
  /** 影片配哪一張照片（closet.photo 編號）。 */
  of: string;
  clip: number;
  hidden: boolean;
  source: string;
  file: string;
  adopted: boolean;
  /** 紀錄的版本：按「採用」時比對畫面那張還是不是現在這張（S2）。 */
  revision: string;
}

/** 一筆 closet.outfit（畫面要的那幾欄）。 */
export interface EditOutfit {
  record: string;
  persona: string;
  /** 衣服 id（closet.outfit 的 outfit 欄位，例 seaside）。 */
  outfit: string;
  name: string;
  state: string;
  source: string;
  /** 沒寫是 null（排最後，同衣櫃的程式 rules.SortOutfits）。 */
  order: number | null;
}

/** 一筆 closet.note（評論紀錄）。 */
export interface EditNote {
  id: string;
  scope: "outfit" | "photo";
  photo: string;
  record: string;
  piece: string;
  level: string;
  comment: string;
  state: string;
  result: string;
  /** 寫的時間（核心蓋的 created_at，沒有就用 updated_at）。 */
  at: string;
}

interface RecordLike {
  id?: string;
  fields?: Record<string, unknown>;
  updated_at?: string;
  revision?: string;
}

const str = (v: unknown): string => (typeof v === "string" ? v.trim() : "");
const int = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);

export function toEditPhoto(r: RecordLike): EditPhoto {
  const f = r.fields ?? {};
  const t = str(f.type);
  return {
    id: r.id ?? "",
    persona: str(f.persona),
    outfit: str(f.outfit),
    type: t === "full" || t === "video" ? t : "half",
    order: int(f.order),
    of: str(f.of),
    clip: int(f.clip),
    hidden: f.hidden === true,
    source: str(f.source) || "official",
    file: str(f.file),
    adopted: f.adopted === true,
    revision: r.revision ?? "",
  };
}

export function toEditOutfit(r: RecordLike): EditOutfit {
  const f = r.fields ?? {};
  const record = r.id ?? "";
  const persona = str(f.persona);
  const outfit =
    str(f.outfit) ||
    (persona && record.startsWith(`${persona}-`) ? record.slice(persona.length + 1) : record);
  return {
    record,
    persona,
    outfit,
    name: str(f.name) || outfit,
    state: str(f.state) || "active",
    source: str(f.source) || "official",
    order: typeof f.order === "number" && Number.isFinite(f.order) ? f.order : null,
  };
}

export function toEditNote(r: RecordLike): EditNote {
  const f = r.fields ?? {};
  const persona = str(f.persona);
  const outfit = str(f.outfit);
  return {
    id: r.id ?? "",
    scope: str(f.scope) === "outfit" ? "outfit" : "photo",
    photo: str(f.photo),
    record: str(f.record) || (persona && outfit ? `${persona}-${outfit}` : ""),
    piece: str(f.piece),
    level: str(f.level),
    comment: str(f.comment),
    state: str(f.state) || "open",
    result: str(f.result),
    at: str(f.created_at) || (r.updated_at ?? ""),
  };
}

// ---- 照片的網址 ----

/**
 * 一筆 closet.photo → 縮圖（影片是影片）的網址：衣櫃的程式每一輪在 closet.person 寫好「照片編號 → 公開網址」
 * （photo_urls，CLO-35；隱藏版的公開檔名是 h-<n>），這裡照編號查，不照位置猜。查不到＝這一輪沒有公開這張
 * （隱藏版沒打開、同一版已經有全身照、檔不在，或剛改過、衣櫃還沒重新發佈），回空字串，畫面顯示佔位。
 */
export function photoUrl(p: Pick<EditPhoto, "id" | "persona">, people: readonly Person[]): string {
  return people.find((x) => x.persona === p.persona)?.photoUrls[p.id] ?? "";
}

/**
 * 這張是不是隱藏版開關關著、這一輪不公開的隱藏版（closet.person 的 hidden_off，S3a）：「修改」分頁整張不列。
 * 沒有那位的檢視（衣櫃還沒算好）時照列（跟對不到網址的其他照片一樣畫佔位；只是看不到圖，不會多公開什麼）。
 */
export function isHiddenOff(
  p: Pick<EditPhoto, "id" | "persona" | "hidden">,
  people: readonly Person[],
): boolean {
  if (!p.hidden) return false;
  return people.find((x) => x.persona === p.persona)?.hiddenOff.includes(p.id) ?? false;
}

/** 「顯示隱藏版」要不要出現：這次看的角色（全部角色時每一位）有列得出來的隱藏版照片才有（開關關著時沒有，S3a）。 */
export function hasListableHidden(
  photos: readonly EditPhoto[],
  people: readonly Person[],
  persona: string,
  all: boolean,
): boolean {
  return photos.some((p) => p.hidden && (all || p.persona === persona) && !isHiddenOff(p, people));
}

/** 這套能不能升成官方：衣櫃的程式算好的（closet.person 檢視的 promotable，CLO-29），這頁不自己判斷。 */
export function promotableOf(
  people: readonly Person[],
  o: Pick<EditOutfit, "persona" | "record">,
): boolean {
  return (
    people.find((x) => x.persona === o.persona)?.outfits.find((x) => x.record === o.record)
      ?.promotable === true
  );
}

// ---- 排序 ----

const byText = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

/** 衣服照 order（沒寫的排最後）再照編號排，同衣櫃的程式。 */
export function sortOutfits(list: readonly EditOutfit[]): EditOutfit[] {
  return [...list].sort((a, b) => {
    if ((a.order === null) !== (b.order === null)) return a.order === null ? 1 : -1;
    if (a.order !== null && b.order !== null && a.order !== b.order) return a.order - b.order;
    return byText(a.record, b.record);
  });
}

/**
 * 一套裡的照片：半身（一般版在前、再照 order）→ 全身 → 影片（跟著配的那張的順序，同一張再照 clip）。
 * 配的那張不在（被篩掉、改成全身）的影片照它自己的 order 排在影片最後面。
 */
export function sortPhotos(list: readonly EditPhoto[]): EditPhoto[] {
  const rank = (p: EditPhoto) => [p.hidden ? 1 : 0, p.order] as const;
  const cmp = (a: readonly number[], b: readonly number[]) => {
    for (let i = 0; i < a.length; i++) {
      const d = (a[i] ?? 0) - (b[i] ?? 0);
      if (d !== 0) return d;
    }
    return 0;
  };
  const sortBy = (xs: EditPhoto[], key: (p: EditPhoto) => readonly number[]) =>
    xs.sort((a, b) => cmp(key(a), key(b)) || byText(a.id, b.id));
  const halves = sortBy(
    list.filter((p) => p.type === "half"),
    rank,
  );
  const fulls = sortBy(
    list.filter((p) => p.type === "full"),
    rank,
  );
  const pos = new Map(halves.map((p, i) => [p.id, i]));
  const videos = sortBy(
    list.filter((p) => p.type === "video"),
    (p) => {
      const i = pos.get(p.of);
      return i === undefined ? [1, p.hidden ? 1 : 0, p.order, p.clip] : [0, i, 0, p.clip];
    },
  );
  return [...halves, ...fulls, ...videos];
}

/** 這張半身有沒有配影片（影片的 of 指到它）：配了就不能改成全身（影片只配半身照）。 */
export function hasClips(photos: readonly EditPhoto[], p: Pick<EditPhoto, "id">): boolean {
  return photos.some((x) => x.type === "video" && x.of === p.id);
}

/** 改類型（半身↔全身）時放到那一類最後一張：同一套、那個類型（一般版與隱藏版一起算）最大的 order 加一，沒有就是 1。 */
export function nextOrder(photos: readonly EditPhoto[], p: EditPhoto, to: PhotoType): number {
  let max = 0;
  for (const x of photos) {
    if (x.id !== p.id && x.outfit === p.outfit && x.type === to) max = Math.max(max, x.order);
  }
  return max + 1;
}

/**
 * 改一般／隱藏版要寫的（只給半身與全身；影片跟著配的那張半身照）：這張的 hidden；半身照在那一版撞到同一個 order
 * 就放到半身最後一張（衣櫃一版裡同一個 order 只公開一張，CLO-34）；配這張的影片（of）一起改，
 * 因為影片只跟同一版的半身照一起公開。這樣改完、衣櫃下一輪發佈後每張都對得到圖。
 */
export function hiddenPatch(
  photos: readonly EditPhoto[],
  p: EditPhoto,
  hidden: boolean,
): { set: { hidden: boolean; order?: number }; videos: string[] } {
  const set: { hidden: boolean; order?: number } = { hidden };
  if (p.type !== "half") return { set, videos: [] };
  const taken = photos.some(
    (x) =>
      x.id !== p.id &&
      x.outfit === p.outfit &&
      x.type === "half" &&
      x.hidden === hidden &&
      x.order === p.order,
  );
  if (taken) set.order = nextOrder(photos, p, "half");
  const videos = photos
    .filter((x) => x.type === "video" && x.of === p.id && x.hidden !== hidden)
    .map((x) => x.id);
  return { set, videos };
}

/**
 * 那一版（一般／隱藏）是不是已經有別張全身照：衣櫃一版只公開一張全身照（CLO-34 的 arrange），
 * 再放一張就永遠是佔位，所以改類型、改一般／隱藏都不給做出第二張（CLO-35）。
 */
export function fullTaken(
  photos: readonly EditPhoto[],
  p: Pick<EditPhoto, "id" | "outfit">,
  hidden: boolean,
): boolean {
  return photos.some(
    (x) => x.id !== p.id && x.outfit === p.outfit && x.type === "full" && x.hidden === hidden,
  );
}

/**
 * 這張是不是自己的隱藏版（CLO-18）：紀錄本身就是（hidden 而且 source = mine，剛切成隱藏版、檢視還沒重算也算），
 * 或衣櫃的程式算好的 owner_hidden 有它（舊家放在官方位置的；頁面不自己看路徑猜）。
 */
export function isOwnerHidden(
  p: Pick<EditPhoto, "id" | "persona" | "hidden" | "source">,
  people: readonly Person[],
): boolean {
  if (p.hidden && p.source === "mine") return true;
  return people.find((x) => x.persona === p.persona)?.ownerHidden.includes(p.id) ?? false;
}

/** 影片跟配的半身照不同版（上次改一般／隱藏寫到一半）：回那張半身照，畫面給一顆「跟上半身」；沒落後回 undefined。 */
export function clipLagsBehind(photos: readonly EditPhoto[], p: EditPhoto): EditPhoto | undefined {
  if (p.type !== "video") return undefined;
  const half = photos.find((x) => x.id === p.of && x.type === "half");
  return half && half.hidden !== p.hidden ? half : undefined;
}

/** 給人看的名稱：「半身 2」「全身」「影片 2-1」（配第 2 張的第 1 支）。 */
export function photoLabel(p: EditPhoto): string {
  if (p.type === "full") return T.full;
  if (p.type === "video") return `${T.video} ${p.order || "?"}-${p.clip || 1}`;
  return `${T.half} ${p.order || "?"}`;
}

export const TYPE_LABEL: Record<PhotoType, string> = { half: T.half, full: T.full, video: T.video };

export function stateLabel(state: string): string {
  if (state === "draft") return T.stateDraft;
  if (state === "retired") return T.stateRetired;
  return T.stateActive;
}

/** 整套的上架／下架鈕：上架中的給「下架」；開發中、已下架的給「上架」（開發中要每張都採用了才能按，publishBlocked）。 */
export function stateAction(state: string): { label: string; to: "active" | "retired" } {
  return state === "active"
    ? { label: T.retire, to: "retired" }
    : { label: T.publish, to: "active" };
}

/** 「放回開發中」給不給（S1）：自己的衣服、不是基本款、現在不是開發中。官方的狀態跟著角色包，不給。 */
export function canDraft(o: Pick<EditOutfit, "source" | "outfit" | "state">): boolean {
  return o.source === "mine" && o.outfit !== "base" && o.state !== "draft";
}

/** 開發中的那套還有幾張沒採用（沿用現有規則：每張都採用了才上架）；不是開發中、或一張照片都沒有的回 0。 */
export function publishBlocked(o: Pick<EditOutfit, "state">, photos: readonly EditPhoto[]): number {
  if (o.state !== "draft") return 0;
  return photos.filter((p) => !p.adopted).length;
}

// ---- 篩選（網址的 ?item=，重新整理還在） ----

/** 頁面最上面的篩選：選角色＋各自獨立的切換鈕（預設都關），同時開＝條件一起成立。 */
export interface Filters {
  /** 跨角色列出（一位一段）；關＝只看這一頁的角色。 */
  all: boolean;
  /** 只看官方的衣服（source = official）。 */
  official: boolean;
  /** 只看還沒採用的照片；整套沒有符合的照片就不列那套。 */
  wip: boolean;
  /** 一起列隱藏版的照片；關＝不列 hidden = true 的。 */
  hidden: boolean;
  /** 按著的外掛篩選鈕（動作編號）。 */
  actions: string[];
}

export const NO_FILTERS: Filters = {
  all: false,
  official: false,
  wip: false,
  hidden: false,
  actions: [],
};

const ACTION_PREFIX = "a:";

/**
 * 篩選放在角色頁網址的 ?item=（核心只留 tab 與 item，item 的值由分頁自己定，board WEB-163）：
 * 用逗號分開的幾個字，all、official、wip、hidden、a:<動作編號>；不認得的字略過。
 */
export function parseFilters(item: string): Filters {
  const f: Filters = { ...NO_FILTERS, actions: [] };
  for (const raw of item.split(",")) {
    const t = raw.trim();
    if (t === "all" || t === "official" || t === "wip" || t === "hidden") f[t] = true;
    else if (t.startsWith(ACTION_PREFIX) && t.length > ACTION_PREFIX.length) {
      const id = t.slice(ACTION_PREFIX.length);
      if (!f.actions.includes(id)) f.actions.push(id);
    }
  }
  return f;
}

/** parseFilters 的反過來：固定順序，都關是空字串（網址沒有 ?item=）。 */
export function filtersItem(f: Filters): string {
  const out: string[] = [];
  for (const k of ["all", "official", "wip", "hidden"] as const) if (f[k]) out.push(k);
  for (const id of [...f.actions].sort()) out.push(`${ACTION_PREFIX}${id}`);
  return out.join(",");
}

/** 有篩選鈕的外掛動作（一個 filter_label 一顆）。 */
export function filterActions(c: Capabilities): ClosetAction[] {
  return c.actions.filter((a) => a.filter !== undefined);
}

/** 外掛動作狀態要看的紀錄：status_kind → 那種的紀錄。 */
export type StatusRecords = Record<
  string,
  readonly { fields?: Record<string, unknown>; archived?: boolean; updated_at?: string }[]
>;

/** 這個動作在這個目標上的狀態值（actionStatus 換成從 StatusRecords 找紀錄）。 */
export function statusOf(a: ClosetAction, records: StatusRecords, t: ActionTarget): string {
  return a.status ? actionStatus(a, records[a.status.kind] ?? [], t) : "";
}

export interface OutfitBlock {
  outfit: EditOutfit;
  /** 篩過、排好的照片。 */
  photos: EditPhoto[];
  /** 這套全部的照片（不管篩選；上架要看每一張都採用了）。 */
  all: EditPhoto[];
  /** 只因為「顯示隱藏版」關著沒列出來的張數（隱藏版開關關著、hidden_off 的不算：那幾張本來就不列）。 */
  hiddenOut: number;
  /** 隱藏版開關關著所以不列、又還沒採用的張數（上架要每張都採用，這幾張要打開開關才看得到）。 */
  offLeft: number;
}

export interface PersonaSection {
  persona: string;
  name: string;
  /** 這位角色一共有幾筆 closet.photo（0＝顯示空狀態）。 */
  total: number;
  blocks: OutfitBlock[];
}

export interface ViewInput {
  persona: string;
  filters: Filters;
  outfits: readonly EditOutfit[];
  photos: readonly EditPhoto[];
  people: readonly Person[];
  caps: Capabilities;
  statuses: StatusRecords;
}

/**
 * 篩過的畫面：一位一段（只看這一頁的角色時就一段），每段裡一套一塊（照 closet.outfit 的 order）、每塊裡篩過排好的照片。
 * 切換鈕各自獨立、同時開＝交集：「官方」與整套的外掛篩選看整套；「開發中」、「顯示隱藏版」與單張的外掛篩選看每一張，
 * 「開發中」或單張的外掛篩選開著時，一張都不符合（或符合的都是沒列出的隱藏版）的那套不列；
 * 只有「顯示隱藏版」關著時，只是不列隱藏版那幾張，那套照列（塊裡說還有幾張沒列）。
 * 隱藏版開關關著的隱藏版（isHiddenOff）不管篩選一律不列、不算進 hiddenOut（S3a：開關關著不留框）；all 照樣含它們
 * （上架照每一張判斷，跟寫入前重讀的一樣），沒採用的有幾張記在 offLeft，塊裡說要打開隱藏版開關才看得到。
 * 「所有角色」只列有照片紀錄的角色，順序同衣櫃的頭像列（有人擔任職位的在前），不在檢視裡的照代號排在後面。
 */
export function buildView(v: ViewInput): PersonaSection[] {
  const on = new Set(v.filters.actions);
  const picked = filterActions(v.caps).filter((a) => on.has(a.id));
  const outfitFilters = picked.filter((a) => a.scope === "outfit");
  const photoFilters = picked.filter((a) => a.scope === "photo");
  const narrowing = v.filters.wip || photoFilters.length > 0;

  const names = new Map(v.people.map((p) => [p.persona, p.name]));
  let personas: string[];
  if (v.filters.all) {
    const have = new Set(v.photos.map((p) => p.persona).filter(Boolean));
    const ordered = pickerPeople([...v.people]).map((p) => p.persona);
    personas = [
      ...ordered.filter((p) => have.has(p)),
      ...[...have].filter((p) => !ordered.includes(p)).sort(byText),
    ];
  } else {
    personas = [v.persona];
  }

  return personas
    .map((persona): PersonaSection => {
      const mine = v.photos.filter((p) => p.persona === persona);
      const blocks: OutfitBlock[] = [];
      for (const o of sortOutfits(v.outfits.filter((x) => x.persona === persona))) {
        if (v.filters.official && o.source !== "official") continue;
        const target = { persona, outfit: o.record };
        if (!outfitFilters.every((a) => filterKeeps(a, statusOf(a, v.statuses, target)))) continue;
        const all = mine.filter((p) => p.outfit === o.record);
        let hiddenOut = 0;
        const shown = all.filter((p) => {
          if (isHiddenOff(p, v.people)) return false;
          if (v.filters.wip && p.adopted) return false;
          const t = { persona, outfit: o.record, photo: p.id };
          if (!photoFilters.every((a) => filterKeeps(a, statusOf(a, v.statuses, t)))) return false;
          if (!v.filters.hidden && p.hidden) {
            hiddenOut++;
            return false;
          }
          return true;
        });
        // 符合的只有隱藏版、而隱藏版關著時也不列（不然會看到一塊空的）。
        if (narrowing && shown.length === 0) continue;
        const offLeft = all.filter((p) => !p.adopted && isHiddenOff(p, v.people)).length;
        blocks.push({ outfit: o, photos: sortPhotos(shown), all, hiddenOut, offLeft });
      }
      return { persona, name: names.get(persona) || persona, total: mine.length, blocks };
    })
    .filter((s) => !v.filters.all || s.total > 0);
}

// ---- 展示間到這裡的入口（CLO-37） ----

/** 這位角色還沒採用的照片：「修改」分頁打開「開發中」與「顯示隱藏版」時列出來的那幾張。 */
export interface Unadopted {
  count: number;
  /** 其中隱藏版的張數（有的話入口一起打開「顯示隱藏版」，不然點過去看不到）。 */
  hidden: number;
  /** 照片都採用了、整套還是開發中（等按「上架」）的套數（CLO-38）：這種沒有沒採用的照片，入口也要出現。 */
  ready: number;
}

/**
 * 照 buildView 算（同一份紀錄、同一個算法），展示間寫的張數跟點過去看到的一樣。
 * ready：這位還在、開發中、有照片而且每張都採用了的套數（publishBlocked 是 0，「上架」按得下去）。
 */
export function unadoptedOf(
  persona: string,
  outfits: readonly EditOutfit[],
  photos: readonly EditPhoto[],
  people: readonly Person[] = [],
): Unadopted {
  const [section] = buildView({
    persona,
    filters: { ...NO_FILTERS, wip: true, hidden: true, actions: [] },
    outfits,
    photos,
    people,
    caps: NO_CAPABILITIES,
    statuses: {},
  });
  const list = (section?.blocks ?? []).flatMap((b) => b.photos);
  const ready = outfits.filter((o) => {
    if (o.persona !== persona || o.state !== "draft") return false;
    const own = photos.filter((p) => p.outfit === o.record);
    return own.length > 0 && publishBlocked(o, own) === 0;
  }).length;
  return { count: list.length, hidden: list.filter((p) => p.hidden).length, ready };
}

/**
 * 入口連到哪：那位角色的「修改」分頁（核心的分頁 key 是 <套件>.<頁面 id>＝closet.edit）。只有沒採用的照片時篩選打開「開發中」；
 * 有等上架的整套時不打開（「開發中」只列沒採用的照片，等上架的那套會被篩掉）。
 */
export function editHref(persona: string, u: Pick<Unadopted, "hidden" | "ready">): string {
  const item = filtersItem({
    ...NO_FILTERS,
    wip: u.ready === 0,
    hidden: u.hidden > 0,
    actions: [],
  });
  const tab = `/personas/${encodeURIComponent(persona)}?tab=closet.edit`;
  return item ? `${tab}&item=${encodeURIComponent(item)}` : tab;
}

// ---- 評論（closet.note） ----

/** 整套的評論（scope = outfit）怎麼填舊的必填欄位：piece 固定 outfit、type 固定 half（只是填必填，沒有意思；看 scope）。 */
export const OUTFIT_PIECE = "outfit";
export const OUTFIT_TYPE = "half";

export function levelOf(p: Pick<EditPhoto, "hidden">): "標準" | "隱藏版" {
  return p.hidden ? "隱藏版" : "標準";
}

/** 舊的 piece 寫法（closet.note、closet.outfit adopted 用的）：half-<n>、full、video-<n>-<k>；隱藏版不加前綴（level 分辨）。 */
export function legacyPiece(p: Pick<EditPhoto, "type" | "order" | "clip">): string {
  if (p.type === "full") return "full";
  if (p.type === "video") return `video-${p.order}-${Math.max(p.clip, 1)}`;
  return `half-${p.order}`;
}

/**
 * 按「評論」寫的 closet.note 欄位：scope、photo、persona、outfit（衣服 id）、record、comment、state = open、intent = comment；
 * 舊的必填欄位照那張填（piece、type，另有 level、slot、clip）；整套的照固定寫法（OUTFIT_PIECE、OUTFIT_TYPE）。評論不改採用狀態。
 */
export function commentFields(
  o: Pick<EditOutfit, "persona" | "outfit" | "record">,
  p: EditPhoto | null,
  comment: string,
): Record<string, string | number> {
  const base = {
    persona: o.persona,
    outfit: o.outfit,
    record: o.record,
    comment: comment.trim(),
    state: "open",
    intent: "comment",
  };
  if (!p) return { ...base, scope: "outfit", piece: OUTFIT_PIECE, type: OUTFIT_TYPE };
  const f: Record<string, string | number> = {
    ...base,
    scope: "photo",
    photo: p.id,
    piece: legacyPiece(p),
    type: p.type,
    level: levelOf(p),
  };
  if (p.type !== "full" && p.order > 0) f.slot = p.order;
  if (p.type === "video") f.clip = Math.max(p.clip, 1);
  return f;
}

/** 傳給生圖秘書（art）的話：哪位角色的哪套、哪一張（或整套）、評論是什麼、處理完要回填什麼。 */
export function commentMessage(
  person: { persona: string; name: string },
  o: Pick<EditOutfit, "name" | "record">,
  p: EditPhoto | null,
  noteId: string,
  comment: string,
): string {
  const who = `${person.name}（${person.persona}）的「${o.name}」（衣服編號 ${o.record}）`;
  const where = p
    ? `${who}的${photoLabel(p)}（${p.hidden ? T.levelHidden : T.levelStd}，照片 closet.photo/${p.id}）`
    : `${who}整套`;
  // 換圖一律用換圖請求（closet-mine.request 帶 photo_id），那張自動回到還沒採用（CMI-22、CMI-25），秘書不自己改 adopted。
  const swap = p
    ? `要換圖就送換圖請求（closet-mine.request 帶 photo_id = ${p.id}），系統自動回到還沒採用，不要自己改 adopted`
    : `要換哪一張的圖就送換圖請求（closet-mine.request 帶那張的 photo_id），系統自動回到還沒採用，不要自己改 adopted`;
  return (
    `我在「修改」分頁評論了 ${where}（closet.note/${noteId}）：「${comment.trim()}」。` +
    `請照評論的內容處理（沒說要重做就不要重做、不要新增衣服、評論本身不改採用）；${swap}。` +
    `處理完把 closet.note/${noteId} 的 state 改 redone（換了圖）或 dismissed（沒換圖），result 寫一句話做了什麼，再告訴我。`
  );
}

const seq = (id: string): number => Number(id.replace(/\D/g, "")) || 0;

const newestFirst = (a: EditNote, b: EditNote) =>
  byText(b.at, a.at) || seq(b.id) - seq(a.id) || byText(b.id, a.id);

/**
 * 這一張的評論紀錄（新的在前）：photo 指到這張的；舊的意見（展示間「設計中」寫的，沒有 photo）照同一套、
 * 同一個 piece（去掉隱藏版的「<編號>:」前綴）、同一版（level；沒寫就看有沒有前綴）對。
 */
export function notesOfPhoto(notes: readonly EditNote[], p: EditPhoto): EditNote[] {
  const key = legacyPiece(p);
  const level = levelOf(p);
  return notes
    .filter((n) => {
      if (n.scope === "outfit") return false;
      if (n.photo) return n.photo === p.id;
      if (n.record !== p.outfit) return false;
      const i = n.piece.indexOf(":");
      if ((i < 0 ? n.piece : n.piece.slice(i + 1)) !== key) return false;
      return n.level ? n.level === level : i >= 0 === p.hidden;
    })
    .sort(newestFirst);
}

/** 這一套整套的評論紀錄（scope = outfit，新的在前）。 */
export function notesOfOutfit(notes: readonly EditNote[], record: string): EditNote[] {
  return notes.filter((n) => n.scope === "outfit" && n.record === record).sort(newestFirst);
}

export function noteStateLabel(state: string): string {
  if (state === "redone") return T.noteRedone;
  if (state === "dismissed") return T.noteDismissed;
  return T.noteOpen;
}

/** 評論的時間：這台的當地時間「11/15 14:03」；讀不懂的照原樣。 */
export function noteTime(at: string): string {
  const d = new Date(at);
  if (!at || Number.isNaN(d.getTime())) return at;
  const two = (n: number) => String(n).padStart(2, "0");
  return `${d.getMonth() + 1}/${d.getDate()} ${two(d.getHours())}:${two(d.getMinutes())}`;
}

/** 外掛動作寫請求用的編號：那種資料是 given（不能 next_id）時自己給一個（<動作編號>-<時間>，符合編號規則）。 */
export function givenRequestId(actionId: string, now: number): string {
  const base = actionId
    .toLowerCase()
    .replace(/[^a-z0-9_-]/g, "-")
    .replace(/^[^a-z0-9]+/, "");
  return `${base || "action"}-${now.toString(36)}`;
}
