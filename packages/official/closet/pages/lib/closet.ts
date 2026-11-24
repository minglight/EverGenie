// 衣櫃套件頁面的純函式（REQ-125）。兩頁（DEV-112，作者 2026-11-03）：「衣櫃」只放擁有的衣服（現在穿哪套、輪流穿）；
// 「展示間」看所有衣服、用點數兌換，並開到核心角色頁的「衣服」分頁（closet-mine 掛的，DEV-143）。設計中的衣服不在這兩頁：
// 採用、評論都在開發家角色頁的「修改」分頁（CLO-35），展示間在開發家只放一行入口（CLO-37）。
// 只有開發家才有的東西一律看能力（caps.ts 的 canEdit），不問是哪種家（CLO-37）。
// 兩頁上面都是一排角色頭像，一次只看一位（DEV-143）；記得上次看誰（只是方便，存在這台瀏覽器）。
// 把 vault 記錄轉成頁面用的形狀、排序、按鈕狀態、請求結果的文字。
// 沒有任何 I/O。頁面不算點數也不決定能不能兌換：person、state 由套件的程式（evergenie-closet）算好寫進 vault，
// 兌換與否以程式為準（頁面只是先提示，不擋就不是錯）。
// 這個套件的字串都集中在這個檔案的 T（外殼的 i18n 表只給外殼自己用；套件的字串跟著套件走）。

export const T = {
  title: "衣櫃",
  showroomTitle: "展示間",
  points: "我的點數",
  pointUnit: "點",
  walletMissing: "還沒有點數：到「習慣」約定習慣、達標就會賺到",
  stateMissing: "點數還沒算出來（衣櫃的程式剛起來時要等一下）",
  loading: "載入中",
  /** 接在「暫時載不進來」前面。 */
  loadWhat: "衣櫃",
  emptyTitle: "衣櫃還是空的",
  emptyHint: "衣櫃只放已經擁有的衣服；到展示間看所有衣服、用點數兌換",
  showroomEmptyTitle: "展示間還是空的",
  showroomEmptyHint: "按右上的「⋯」新增角色或加一套衣服，或請秘書把衣服加進來",
  toShowroom: "到展示間看全部衣服 ›",
  showroomHint: "所有衣服都在這裡：沒擁有的只看得到剪影，用點數兌換；兌換或送的衣服會出現在衣櫃。",
  // 開發家不遮（CLO-27）：沒兌換的衣服也看得到真圖，說明要跟著改，不能還說只看得到剪影。
  showroomHintDev:
    "所有衣服都在這裡：這裡是開發家，沒兌換的衣服也看得到真圖（標「未兌換」的就是）；點數與兌換照舊，兌換或送的衣服會出現在衣櫃。",
  previewHint: "未兌換（開發家看得到全部）：點數與兌換照舊，兌換後才會進衣櫃、可以輪流穿。",
  toMineOutfit: "到角色頁管理這套（加照片） ›",
  /** 開發家、這套可以升成官方：升成官方在角色頁「修改」分頁那一套的塊裡（DEV-160 審查 S1）。 */
  toEditPromote: "到「修改」分頁升成官方 ›",
  newCharacter: "新增角色",
  addOutfit: "加一套衣服",
  /** 展示間右上「⋯」給讀屏的名字（DEV-156 U-15）。 */
  showroomMenu: "更多：新增角色、加一套衣服",
  /** 手機底部抽屜的標題（讀屏名字太長、不像標題）。 */
  showroomMenuTitle: "新增",
  pickPerson: "看哪位角色",
  /** 開發家展示間到「修改」分頁的入口（CLO-37）：這位角色還有幾張照片沒採用。 */
  // 開發家展示間到「修改」分頁的入口（CLO-37、CLO-38）：沒採用的照片幾張、等上架的幾套。
  toEdit: (n: number, ready = 0) =>
    `${[n > 0 ? `${n} 張照片還沒採用` : "", ready > 0 ? `${ready} 套等上架` : ""].filter(Boolean).join("、")}：到角色頁的「修改」看 ›`,
  /** 開發家：這位角色只有還沒上架的衣服（展示間不列設計中的，到「修改」看）。 */
  noShownOutfits: "這位角色還沒有上架的衣服",
  filterGroup: "篩選衣服",
  filterAll: "全部",
  filterPromotable: "可以升成官方",
  filterOfficial: "已經是官方",
  filterMine: "只在開發家（我自己的）",
  filterNone: "沒有符合的衣服",
  filterHintPromotable: "點一套就到那位角色的「修改」分頁，那一套的塊裡有「升成官方」的按鈕。",
  filterHintPromotableNoTab: "這幾套可以升成官方；升成官方要到管設計的地方按（這個家的「修改」分頁關著）。",
  filterHintMine: "自己加的、但不能升成官方的衣服（例如自己角色的、自己的隱藏版）。",
  nowWearing: "現在穿：",
  nobodyWearing: "還沒有穿的衣服",
  seatedAs: "目前由",
  tagFree: "送的",
  tagOwned: "已擁有",
  tagRetired: "下架",
  tagDraft: "設計中",
  tagLocked: "未兌換",
  // 現在穿這套（CLO-30）
  wearNow: "現在穿這套",
  wearingNow: "現在穿著",
  wearingToday: "現在穿著（今天都穿這套）",
  wearBusy: "換衣服中…",
  wearDone: "換好了",
  wearUndo: "復原",
  wearUndone: "已復原",
  wearToday: "今天都穿這套，明天起照輪流穿的換",
  wearRejected: "這次沒有換成",
  wearTimeout: "已送出，等一下就會換好",
  rotateHint: "「輪流穿」打開的幾套會每小時輪流換；指定「現在穿這套」的那天不輪。",
  toWardrobe: "到衣櫃看 ›",
  rotate: "輪流穿",
  rotateOn: "輪流穿：開",
  rotateOff: "輪流穿：關",
  lastOne: "至少要留一套輪流穿",
  /** 隱藏版的圖的名字（衣服詳情的全身照切換、看大圖）。 */
  levelHidden: "隱藏版",
  cancel: "取消",
  notFound: "找不到這套衣服",
  // 一般家沒有上架／下架這些字（作者 11-15，M5）。
  notFoundHint: "它可能已經收起來或被移走；已經擁有的衣服不會不見",
  price: "價格",
  free: "免費",
  unlock: (cost: number) => `用 ${cost} 點兌換`,
  unlockAsk: (cost: number) => `確定用 ${cost} 點兌換嗎？`,
  unlockConfirm: "確定兌換",
  waiting: "處理中…",
  unlocked: "兌換了",
  timeout: "已送出，等衣櫃處理（套件沒在跑時會晚一點）",
  lockedHint: "還沒兌換：只看得到模糊的剪影，真正的圖要兌換才會出現",
  photos: "輪播照片",
  zoom: "看大圖",
  clipWord: "動畫",
  viewHalf: "半身",
  viewFull: "全身",
  viewGroup: "照片類型",
  unknownReject: "這次沒有兌換成功",
  shortOf: (need: number, have: number) => `點數不夠：要 ${need} 點，現在 ${have} 點`,
  draftCannot: "設計中的衣服還不能兌換",
  retiredCannot: "這套現在不能兌換",
  ownedAlready: "已經擁有了",
  variantInWindow: "現在是設定的時段，自動開著",
  // 展示間 ↔ 習慣（DEV-157、CLO-24）
  toHabits: "到習慣頁看怎麼賺點數 ›",
  nextReady: "可以換一套了",
  nextShort: (n: number) => `再 ${n} 點可以換下一套`,
  // 正在看某位角色時照那位算（DEV-157），說出是誰的。
  nextReadyOf: (name: string) => `可以換${name}的一套了`,
  nextShortOf: (n: number, name: string) => `再 ${n} 點可以換${name}的下一套`,
  // 沒有衣服時照角色包的狀態說明（DEV-157、CLO-25）
  // 沒有衣服包（DEV-156 U-09）：白話說「還沒裝」，給一條找秘書的路（開聊天清單、不替主人送話）。
  packNoneTitle: "衣服包還沒裝",
  packNoneHint: "這一版還沒帶衣服包；之後的版本有了會自動裝好。",
  // 只開聊天清單、不替主人送話，所以字寫「到聊天」，不寫成按了就會請好的樣子（DEV-156 review）。
  packAsk: "到聊天請秘書幫忙 ›",
  packPendingTitle: "衣服還沒下載好",
  packDownloading: "正在下載衣服，好了這裡就會出現。會自動再試。",
  packFailed: (why: string) => `上次沒成功${why ? `：${why}` : ""}。會自動再試。`,
  packSyncedTitle: "這位角色還沒有衣服",
  packSyncedAllTitle: "還沒有任何角色的衣服",
  packSyncedHint: "衣服包已經同步好了，但裡面沒有這位角色的衣服。",
  packSyncedAllHint: "衣服包已經同步好了，但裡面沒有這個家的角色的衣服。",
  packRetry: "現在再試",
  packRetrying: "正在試…",
  packRetryFailed: "這次沒試成：",
  ownUpload: "自己上傳一套 ›",
  ownUploadHint: "也可以自己上傳角色與衣服。",
};

export interface RecordLike {
  id?: string;
  fields?: Record<string, unknown>;
}

export interface Photo {
  /** 第幾張（1 起算）；舊資料沒有 n 就用它在清單裡的位置。 */
  n: number;
  url: string;
  real: boolean;
  video: string;
  /** 這張所有影片的網址（第 1 支在前）；舊資料只有 video 就是它一支。 */
  videos: string[];
  /** 隱藏版是 h（DEV-160 S2 起只有一組）；一般版是空字串。隱藏版開著時兩版都列，同一個 n 可能各有一張（2026-11-13）。 */
  variant: string;
}

export interface PersonOutfit {
  /** outfit 編號，例 athena-seaside。 */
  record: string;
  outfit: string;
  name: string;
  desc: string;
  cost: number;
  state: string;
  owned: boolean;
  rotating: boolean;
  photos: Photo[];
  /** 全身照網址；只有擁有的與設計中的衣服有，沒有是空字串。 */
  full: string;
  /** 隱藏版開著時這套的隱藏版全身照網址（跟 full 並存）；沒有是空字串。 */
  fullHidden: string;
  /** official｜mine（mine 是使用者自己加的，closet-mine 頁面用）。 */
  source: string;
  /** 沒兌換、但這個家是開發家所以程式照樣給真圖（CLO-27）；正式家永遠是 false。 */
  preview: boolean;
  /** 可以升成官方（程式算的，CLO-29：開發家、官方角色、source = mine、已上架、照片不是全部隱藏版）；正式家永遠是 false。 */
  promotable: boolean;
}

export interface Person {
  persona: string;
  name: string;
  /** 現在坐這個人設的職位 id；沒人坐是空字串。 */
  agent: string;
  worn: string;
  wornPhoto: number;
  rotation: string[];
  /** 現在這套是指定穿的（現在穿這套，CLO-30）；false＝照輪流穿。 */
  picked: boolean;
  /** 指定穿到什麼時候（RFC 3339）；沒指定是空字串。 */
  wornUntil: string;
  outfits: PersonOutfit[];
  /** 照片編號 → 這一輪公開的網址（closet.person 的 photo_urls，DEV-160 CLO-35；只列真的公開的）。 */
  photoUrls: Record<string, string>;
  /** 自己的隱藏版的照片編號（closet.person 的 owner_hidden，CLO-18、CLO-35）：「修改」分頁不給改成一般版。 */
  ownerHidden: string[];
  /**
   * 隱藏版開關關著、這一輪不公開的隱藏版照片編號（closet.person 的 hidden_off，DEV-160 審查 S3a）：
   * 「修改」分頁整張不列、也不出「顯示隱藏版」（開關關著不留框），頁面不自己猜開關。
   */
  hiddenOff: string[];
}

export interface ClosetState {
  earned: number;
  spent: number;
  points: number;
  wallet: boolean;
}

export interface UnlockRequest {
  id: string;
  state: string;
  reason: string;
  unlock: string;
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
const rec = (v: unknown): Record<string, unknown> =>
  v && typeof v === "object" ? (v as Record<string, unknown>) : {};

function toPhoto(v: unknown, i = 0): Photo {
  const m = rec(v);
  const video = str(m.video);
  const videos = list(m.videos).map(str).filter(Boolean);
  return {
    n: num(m.n) || i + 1,
    url: str(m.url),
    real: m.real === true,
    video,
    videos: videos.length > 0 ? videos : video ? [video] : [],
    // 舊資料沒有 variant：網址帶 ?v=<編號> 的就是隱藏版。
    variant: str(m.variant) || (str(m.url).match(/[?&]v=([^&]+)/)?.[1] ?? ""),
  };
}

function toOutfit(v: unknown): PersonOutfit {
  const m = rec(v);
  return {
    record: str(m.record),
    outfit: str(m.outfit),
    name: str(m.name) || str(m.outfit),
    desc: str(m.desc),
    cost: num(m.cost),
    state: str(m.state) || "active",
    owned: m.owned === true,
    rotating: m.rotating === true,
    photos: list(m.photos).map((p, i) => toPhoto(p, i)),
    full: str(m.full),
    fullHidden: str(m.full_hidden),
    source: str(m.source) || "official",
    preview: m.preview === true,
    promotable: m.promotable === true,
  };
}

export function toPerson(r: RecordLike): Person {
  const f = r.fields ?? {};
  const persona = str(f.persona) || (r.id ?? "");
  return {
    persona,
    name: str(f.name) || persona,
    agent: str(f.agent),
    worn: str(f.worn),
    wornPhoto: num(f.worn_photo),
    rotation: list(f.rotation).map(str).filter(Boolean),
    picked: f.picked === true,
    wornUntil: str(f.worn_until),
    outfits: list(f.outfits).map(toOutfit),
    photoUrls: Object.fromEntries(
      Object.entries(rec(f.photo_urls)).filter(
        (e): e is [string, string] => typeof e[1] === "string" && e[1] !== "",
      ),
    ),
    ownerHidden: list(f.owner_hidden).filter((x): x is string => typeof x === "string" && x !== ""),
    hiddenOff: list(f.hidden_off).filter((x): x is string => typeof x === "string" && x !== ""),
  };
}

export function toState(r: RecordLike | null | undefined): ClosetState | null {
  if (!r) return null;
  const f = r.fields ?? {};
  return {
    earned: num(f.earned),
    spent: num(f.spent),
    points: num(f.points),
    wallet: f.wallet === true,
  };
}

/** 隱藏版的開關（CLO-17）：程式寫在 closet.state 的 variants，頁面照它畫，不自己算時段。DEV-160 S2 起只有一組（id = h）。 */
export interface VariantSwitch {
  id: string;
  name: string;
  /** 這個開關管的 closet.variant 編號（按了每一筆的 on 一起改）；舊的 state 沒有就是 [id]。 */
  ids: string[];
  /** 開關（任一筆 closet.variant 的 on）。 */
  on: boolean;
  /** 現在在設定的時段裡（時段內不論開關都開著）。 */
  inWindow: boolean;
}

/** 衣櫃頁要畫的隱藏版開關：只列 button = true 的，照程式排好的順序。 */
export function variantSwitches(r: RecordLike | null | undefined): VariantSwitch[] {
  return list(r?.fields?.variants)
    .map(rec)
    .filter((m) => m.button === true && str(m.id) !== "")
    .map((m) => {
      const ids = list(m.ids).map(str).filter(Boolean);
      return {
        id: str(m.id),
        name: str(m.name) || str(m.id),
        ids: ids.length > 0 ? ids : [str(m.id)],
        on: m.on === true,
        inWindow: m.in_window === true,
      };
    });
}

export function toRequest(r: RecordLike): UnlockRequest {
  const f = r.fields ?? {};
  return { id: r.id ?? "", state: str(f.state), reason: str(f.reason), unlock: str(f.unlock) };
}

/** 有人坐的在前（依職位 id），沒人坐的在後（依人設代號）；一套衣服都沒有的角色不顯示。 */
export function orderPeople(people: Person[]): { seated: Person[]; unseated: Person[] } {
  const shown = people.filter((p) => p.outfits.length > 0);
  const seated = shown
    .filter((p) => p.agent !== "")
    .sort((a, b) => a.agent.localeCompare(b.agent) || a.persona.localeCompare(b.persona));
  const unseated = shown
    .filter((p) => p.agent === "")
    .sort((a, b) => a.persona.localeCompare(b.persona));
  return { seated, unseated };
}

export function wornOutfit(p: Person): PersonOutfit | undefined {
  return p.outfits.find((o) => o.outfit === p.worn);
}

/** 現在穿的那張圖的網址（沒有圖回空字串）。 */
export function wornImage(p: Person): string {
  const o = wornOutfit(p);
  if (!o) return "";
  const photo = o.photos[p.wornPhoto - 1] ?? o.photos[0];
  return photo?.real ? photo.url : "";
}

/**
 * 展示間的衣服格子：所有衣服（擁有的真圖、沒擁有的剪影），不含設計中——設計中的在開發家角色頁的「修改」分頁看（CLO-35），
 * 展示間在開發家只放一行入口（CLO-37）。
 */
export function gridOf(p: Person): PersonOutfit[] {
  return p.outfits.filter((o) => o.state !== "draft");
}

/**
 * 展示間列哪幾位角色（CLO-37）：不能修改衣服的家（一般家）只看格子裡的衣服，只有設計中衣服的角色不列（不然是空的一段）；
 * 能修改的家（開發家）照列，那位的一段裡有到「修改」的入口。
 */
export function showroomPeople(people: Person[], edit: boolean): Person[] {
  if (edit) return pickerPeople(people);
  return pickerPeople(people.map((p) => ({ ...p, outfits: gridOf(p) })));
}

/**
 * 衣櫃看到的角色（CLO-14）：只留擁有的衣服（設計中不算擁有、沒擁有的下架衣服本來就不在 person 裡）；
 * 一套都沒擁有的角色在 orderPeople 被拿掉。現在穿的一定是擁有的，所以 wornOutfit／wornImage 照樣找得到。
 */
export function ownedView(p: Person): Person {
  return { ...p, outfits: p.outfits.filter((o) => o.owned && o.state !== "draft") };
}

/**
 * 標籤：送的、已擁有、N 點、下架（下架而且擁有的才看得到）。
 * 開發家沒兌換卻看得到真圖的（preview，CLO-27）在「N 點」前面多標「未兌換」：圖上沒有鎖頭了，要有字說它還沒兌換。
 * 「下架」只在能修改衣服的家（edit，開發家）標：一般家沒有上架／下架這件事（作者 11-15，M5），下架又擁有的照常列、不標。
 */
export function tagsOf(o: PersonOutfit, edit = false): string[] {
  const tags: string[] = [];
  if (o.state === "draft") return [T.tagDraft];
  if (o.owned) tags.push(o.cost === 0 ? T.tagFree : T.tagOwned);
  else {
    if (o.preview) tags.push(T.tagLocked);
    tags.push(`${o.cost} ${T.pointUnit}`);
  }
  if (o.state === "retired" && edit) tags.push(T.tagRetired);
  return tags;
}

/**
 * 衣服詳情頁的標籤（CLO-13）：上面已經有一行「價格：N 點」，沒擁有的不再重複「N 點」，改標「未兌換」；
 * 其他跟 tagsOf 一樣（「下架」也只在 edit 的家標）。衣櫃格子沒有價格那行，照用 tagsOf。
 */
export function detailTagsOf(o: PersonOutfit, edit = false): string[] {
  if (o.state === "draft" || o.owned) return tagsOf(o, edit);
  return o.state === "retired" && edit ? [T.tagLocked, T.tagRetired] : [T.tagLocked];
}

/** 這套看不看得到真圖的全部（輪播照、動畫）：擁有的，或開發家沒兌換的（preview，CLO-27）。 */
export function seesAll(o: PersonOutfit): boolean {
  return o.owned || o.preview;
}

/** 「看大圖」的一項（`@evergenie/ui` 的 MediaViewer 吃這個形狀）。 */
export interface ViewItem {
  src: string;
  name: string;
  type: "image" | "video";
}

/**
 * 衣服詳情頁可以放大看的清單（FEN-30）：每張真圖、看得到全部時（seesAll）接它的動畫，最後是全身照（一般版、隱藏版）。
 * 剪影不放大（沒有東西可看）。
 */
export function viewerItems(o: PersonOutfit): ViewItem[] {
  const items: ViewItem[] = [];
  const all = seesAll(o);
  o.photos.forEach((ph, i) => {
    if (!ph.real || ph.url === "") return;
    items.push({ src: ph.url, name: `${o.name} ${i + 1}`, type: "image" });
    if (!all) return;
    for (const v of ph.videos)
      items.push({ src: v, name: `${o.name} ${i + 1} ${T.clipWord}`, type: "video" });
  });
  if (o.full !== "") items.push({ src: o.full, name: `${o.name} ${T.viewFull}`, type: "image" });
  if (o.fullHidden !== "")
    items.push({
      src: o.fullHidden,
      name: `${o.name} ${T.viewFull}（${T.levelHidden}）`,
      type: "image",
    });
  return items;
}

/** 開發家展示間的篩選（CLO-29）：網址 ?filter=；all＝不篩（照舊一位一位看）。 */
export type ShowFilter = "all" | "promotable" | "official" | "mine";

/**
 * 一套衣服算哪一類（一套只算一類，三類加起來就是全部）：
 * 可以升成官方＝程式給的 promotable（CLO-29）；已經是官方＝source = official；
 * 只在開發家＝source = mine 但不能升（自己角色的、看起來是自己的隱藏版的）。
 * closet-mine 不能用時沒有「升成官方」這件事，自己加的全部算「只在開發家」。
 */
export function filterKindOf(
  o: Pick<PersonOutfit, "source" | "promotable">,
  mine: boolean,
): Exclude<ShowFilter, "all"> {
  if (o.source !== "mine") return "official";
  return mine && o.promotable ? "promotable" : "mine";
}

export interface FilterEntry {
  person: Person;
  outfit: PersonOutfit;
}

/**
 * 跨所有角色、符合篩選的衣服（角色照頭像那排的順序，衣服照原本的順序）。只算展示間格子裡的（gridOf：設計中的在「修改」分頁看），
 * 而且只用 closet.person 已經給頁面的衣服——關著的隱藏版本來就不在裡面，不會因為篩選多露出來。
 */
export function filterEntries(people: Person[], filter: ShowFilter, mine: boolean): FilterEntry[] {
  const out: FilterEntry[] = [];
  for (const person of pickerPeople(people)) {
    for (const outfit of gridOf(person)) {
      if (filter === "all" || filterKindOf(outfit, mine) === filter) out.push({ person, outfit });
    }
  }
  return out;
}

export interface FilterOption {
  value: ShowFilter;
  label: string;
  count: number;
}

/**
 * 篩選那一排有哪些按鈕、各幾套。只有能修改衣服的家（edit，開發家）有（一般家沒有官方／我的、升成官方這些事，回空的＝不畫；CLO-37）；
 * closet-mine 不能用（或還沒讀到）時沒有「可以升成官方」。
 */
export function filterOptions(
  edit: boolean,
  mine: boolean | undefined,
  people: Person[],
): FilterOption[] {
  if (!edit) return [];
  const can = mine === true;
  const count = (f: ShowFilter) => filterEntries(people, f, can).length;
  const out: FilterOption[] = [{ value: "all", label: T.filterAll, count: count("all") }];
  if (can) out.push({ value: "promotable", label: T.filterPromotable, count: count("promotable") });
  out.push({ value: "official", label: T.filterOfficial, count: count("official") });
  out.push({ value: "mine", label: T.filterMine, count: count("mine") });
  return out;
}

/** 網址的 ?filter=；不認得、或這個家現在沒有那個按鈕（一般家、closet-mine 沒開）都當「全部」。 */
export function chosenFilter(wanted: string, options: FilterOption[]): ShowFilter {
  return options.find((o) => o.value === wanted)?.value ?? "all";
}

/**
 * 篩選清單裡一格點了去哪：可以升的到那位角色的「修改」分頁（那一套的塊裡有「升成官方」，DEV-160 審查 S1）；其他照舊到衣服詳情。
 * 「修改」分頁關掉了（editTab=false，closet.edit-tab）就到衣服詳情，不連到不存在的分頁（SEC-12）。
 */
export function filterHref(filter: ShowFilter, e: FilterEntry, editTab = true): string {
  return filter === "promotable" && editTab ? editTabHref(e.person.persona) : outfitHref(e.outfit.record);
}

/** 這位角色的「修改」分頁（核心的分頁 key 是 <套件>.<頁面 id>＝closet.edit；只有能修改衣服的家才有，CLO-35）。 */
export function editTabHref(persona: string): string {
  return `/personas/${encodeURIComponent(persona)}?tab=closet.edit`;
}

/** 衣服詳情頁沒擁有時的那行說明：開發家看得到真圖的不能再說「只看得到剪影」（CLO-27）。 */
export function lockedHintOf(o: PersonOutfit): string {
  if (o.owned || o.state === "draft") return "";
  return o.preview ? T.previewHint : T.lockedHint;
}

export interface UnlockButton {
  /** hidden：不顯示按鈕（已擁有、草稿、下架）。 */
  show: boolean;
  enabled: boolean;
  /** 按鈕下面的一行說明；空字串不顯示。 */
  hint: string;
}

/** 兌換按鈕的狀態。判斷以程式為準：這裡只是先提示，沒擋到不是錯。 */
export function unlockButton(o: PersonOutfit, state: ClosetState | null): UnlockButton {
  if (o.owned) return { show: false, enabled: false, hint: "" };
  if (o.state === "draft") return { show: false, enabled: false, hint: T.draftCannot };
  if (o.state === "retired") return { show: false, enabled: false, hint: T.retiredCannot };
  if (state && !state.wallet) return { show: true, enabled: false, hint: T.walletMissing };
  if (state && state.points < o.cost) {
    return { show: true, enabled: false, hint: T.shortOf(o.cost, state.points) };
  }
  return { show: true, enabled: true, hint: "" };
}

/** 頂端點數旁的提示：沒有錢包（還沒裝或沒賺過）才有字。 */
export function walletHint(state: ClosetState | null): string {
  if (!state) return T.stateMissing;
  return state.wallet ? "" : T.walletMissing;
}

/**
 * 切換「輪流穿」之後的清單（衣服 id）。目前有效清單是 person.rotation；
 * 關掉最後一個會回 null（不能取消最後一個勾）；開啟已經在清單裡的、關掉不在清單裡的，清單不變。
 */
export function nextRotation(p: Person, outfit: string, on: boolean): string[] | null {
  const cur = p.rotation;
  if (on) return cur.includes(outfit) ? cur : [...cur, outfit];
  if (!cur.includes(outfit)) return cur;
  const next = cur.filter((x) => x !== outfit);
  return next.length === 0 ? null : next;
}

export type Outcome =
  | { kind: "waiting" }
  | { kind: "done"; text: string }
  | { kind: "rejected"; text: string }
  | { kind: "timeout"; text: string };

/** 讀到的請求 → 要顯示的結果；request 是 undefined 表示還沒讀到。 */
export function requestOutcome(req: UnlockRequest | undefined, timedOut: boolean): Outcome {
  if (req?.state === "done") return { kind: "done", text: T.unlocked };
  if (req?.state === "rejected") return { kind: "rejected", text: req.reason || T.unknownReject };
  if (timedOut) return { kind: "timeout", text: T.timeout };
  return { kind: "waiting" };
}

/** 「現在穿這套」或復原要送的請求（CLO-30）。 */
export interface WearAction {
  type: "wear" | "unwear";
  outfit: string;
}

/**
 * 按了「現在穿這套」之後的「復原」要送什麼：回到剛才那套。剛才那套也是指定穿的（而且不是同一套）就再指定穿它；
 * 否則取消這次的指定、回到輪流穿（同一個小時裡輪流穿的就是剛才那套）。before 是按之前穿的那套：
 * 送出當下記住的（剛指定穿、頁面還沒重讀到的那套也算），不靠重讀。
 */
export function undoOf(before: Pick<Person, "worn" | "picked">, outfit: string): WearAction {
  if (before.picked && before.worn && before.worn !== outfit) {
    return { type: "wear", outfit: before.worn };
  }
  return { type: "unwear", outfit };
}

/** 讀到的「現在穿這套」請求 → 要顯示的結果（同 requestOutcome，字不一樣）。 */
export function wearOutcome(req: UnlockRequest | undefined, timedOut: boolean): Outcome {
  if (req?.state === "done") return { kind: "done", text: T.wearDone };
  if (req?.state === "rejected") return { kind: "rejected", text: req.reason || T.wearRejected };
  if (timedOut) return { kind: "timeout", text: T.wearTimeout };
  return { kind: "waiting" };
}

/** 衣櫃頁看某位角色的網址（兌換成功後的「到衣櫃看」）。 */
export function wardrobeHref(persona: string): string {
  return `${WARDROBE_HREF}${withPersona("", persona)}`;
}

/** outfit 頁網址的 id（?id=athena-seaside）。 */
export function idFromSearch(search: string): string {
  return new URLSearchParams(search).get("id") ?? "";
}

export const SHOWROOM_HREF = "/p/closet/showroom";
export const WARDROBE_HREF = "/p/closet/wardrobe";

/** 衣服詳情頁的網址；返回用看板外殼的「上一頁」（CLO-16）。 */
export function outfitHref(record: string): string {
  return `/p/closet/outfit?id=${encodeURIComponent(record)}`;
}

/**
 * 新增角色、替某位角色加衣服與照片的網址（DEV-143）：都在核心的角色頁——新增在角色清單的「新增角色」（?new=1），
 * 衣服在那位角色的「衣服」分頁（closet-mine 掛的 closet-mine.outfits）；還沒選角色就到角色清單。closet-mine 沒開時展示間不顯示這些按鈕（CLO-15）。
 */
export function mineHref(kind: "new" | "person", persona = ""): string {
  if (kind === "new") return "/personas?new=1";
  if (persona) return `/personas/${encodeURIComponent(persona)}?tab=closet-mine.outfits`;
  return "/personas";
}

/**
 * 衣服詳情頁「到角色頁管理這套」的網址（CLO-27）：那位角色的「衣服」分頁、直接打開這一套
 * （角色頁的 ?item= 交給分頁的頁面，closet-mine 用它當「打開哪一套」；board 卡 WEB-163）。
 */
export function mineOutfitHref(persona: string, record: string): string {
  if (!persona) return mineHref("person");
  return `${mineHref("person", persona)}&item=${encodeURIComponent(record)}`;
}

/**
 * 衣服詳情頁要不要放「到角色頁管理這套（加照片）」（CLO-27）：只在能修改衣服的家（edit，開發家；讀不到當不能，CLO-37）、
 * closet-mine 能用、這套是主人自己加的（source = mine）時。可以升成官方的（promotable）另外連到「修改」分頁（升成官方在那裡，
 * DEV-160 審查 S1）；把關在 closet-mine 的程式（CMI-14）。
 */
export function showMineLink(
  edit: boolean,
  mine: boolean | undefined,
  o: Pick<PersonOutfit, "source">,
): boolean {
  return edit && mine === true && o.source === "mine";
}

/** 這台瀏覽器記得上次看哪位角色（衣櫃與展示間共用；只是方便，讀不到就從第一位開始）。 */
export const PERSONA_KEY = "eg.closet.persona.v1";

/** 頭像列的順序：有人擔任職位的在前、其他在後（同 orderPeople；沒衣服的不列）。 */
export function pickerPeople(people: Person[]): Person[] {
  const { seated, unseated } = orderPeople(people);
  return [...seated, ...unseated];
}

/** 網址的 ?persona=（從角色頁的「在衣櫃看」連過來）。 */
export function personaFromSearch(search: string): string {
  return new URLSearchParams(search).get("persona") ?? "";
}

/** 現在看誰：網址指定的、再來是上次看的，都不在清單上就第一位；清單空的回空字串。 */
export function chosenPersona(list: Person[], wanted: string, remembered: string): string {
  for (const code of [wanted, remembered]) {
    if (code && list.some((p) => p.persona === code)) return code;
  }
  return list[0]?.persona ?? "";
}

/** 把網址的 ?persona= 換成 code（其他查詢照留；code 空的就拿掉）。 */
export function withPersona(search: string, code: string): string {
  const q = new URLSearchParams(search);
  if (code) q.set("persona", code);
  else q.delete("persona");
  const s = q.toString();
  return s ? `?${s}` : "";
}

/** 習慣頁（點數在那裡賺）。只在「習慣」套件能用時才連過去（CLO-24）。 */
export const HABITS_HREF = "/p/habits/list";

/**
 * 展示間點數卡的那一句（CLO-24）：還有沒兌換、可以兌換的衣服（上架中、要點數）時，點數夠換最便宜的那套就說「可以換一套了」，
 * 不夠就說「再 N 點可以換下一套」；沒有可換的衣服、或點數還沒算出來，回空字串（不顯示）。
 * focus：正在看的那位角色（DEV-157）——只照這位的衣服算、字裡說是誰的（「再 2 點可以換露娜的下一套」）；這位沒有可換的就不顯示。
 * 沒有 focus（篩選這類總覽）照全部角色算。只是提示：能不能兌換以程式為準（CLO-N4）。
 */
export function nextOutfitHint(
  people: Person[],
  state: ClosetState | null | undefined,
  focus?: Person,
): string {
  if (!state) return "";
  const costs = (focus ? [focus] : people)
    .flatMap((p) => p.outfits)
    .filter((o) => !o.owned && o.state === "active" && o.cost > 0)
    .map((o) => o.cost);
  if (costs.length === 0) return "";
  const need = Math.min(...costs) - state.points;
  if (focus) {
    const name = focus.name || focus.persona;
    return need <= 0 ? T.nextReadyOf(name) : T.nextShortOf(need, name);
  }
  return need <= 0 ? T.nextReady : T.nextShort(need);
}

/** 角色包的現況（personas.pack_status 回的，只留空狀態說明要用的）。 */
export interface PackStatus {
  registered?: string;
  latest?: string;
  source?: boolean;
  pending?: { reason?: string };
  last?: { status?: string; message?: string; reason?: string; records_later?: boolean };
}

/**
 * 沒有衣服時是哪一種（CLO-25）：
 * none＝這一版沒有官方衣服包（指標沒有版號）；pending＝正在下載或上次沒成功（待補、紀錄還沒寫、或有包卻還沒同步過）；
 * synced＝已經同步好了、只是沒有衣服；unknown＝讀不到角色包狀態、或這個家是角色包的來源（不同步）——照原本的空狀態說明。
 */
export type PackKind = "none" | "pending" | "synced" | "unknown";

export function packKind(s: PackStatus | null | undefined): PackKind {
  if (!s || s.source) return "unknown";
  if (!s.latest) return "none";
  // 不看 last.status：已是最新、不退版時服務清掉待補但不改 last，舊的失敗會一直留著。
  if (s.pending || s.last?.records_later || !s.registered) return "pending";
  return "synced";
}

export interface EmptyNotice {
  kind: PackKind;
  title: string;
  hint: string;
  /** 顯示「現在再試」按鈕。 */
  retry: boolean;
}

/**
 * 衣櫃、展示間沒有衣服時要說什麼（CLO-25）。wanted＝網址指定要看的那位（?persona=），有的話說「這位角色」。
 * fallback 是原本的空狀態說明（讀不到角色包狀態時用）。
 */
export function emptyNotice(
  s: PackStatus | null | undefined,
  wanted: string,
  fallback: { title: string; hint: string },
): EmptyNotice {
  const kind = packKind(s);
  switch (kind) {
    case "none":
      return { kind, title: T.packNoneTitle, hint: T.packNoneHint, retry: false };
    case "pending": {
      const failed = !!s?.pending || s?.last?.status === "failed";
      // 原因用短的那句（reason）；message 是整句給人看的話，已經含「沒有同步成功」「會自動再試」，接上去會重複。
      const why = s?.last?.reason || s?.pending?.reason || s?.last?.message || "";
      return {
        kind,
        title: T.packPendingTitle,
        hint: failed ? T.packFailed(why.replace(/[。.]$/, "")) : T.packDownloading,
        retry: true,
      };
    }
    case "synced":
      return wanted
        ? { kind, title: T.packSyncedTitle, hint: T.packSyncedHint, retry: false }
        : { kind, title: T.packSyncedAllTitle, hint: T.packSyncedAllHint, retry: false };
    default:
      return { kind, ...fallback, retry: false };
  }
}

/** closet-mine 能不能用：啟用中而且沒有被拒（依賴沒開）。讀不到套件（沒裝）當作不能用。 */
export function mineAvailable(pkg: { enabled?: boolean; state?: string } | undefined): boolean {
  return pkg?.enabled === true && pkg.state !== "rejected";
}

/** 在所有角色裡找一套衣服（用 outfit 編號）。 */
/**
 * 衣服詳情頁看不看得到這套（CLO-38）：設計中的（state = draft）只有能修改衣服的家（edit，開發家）看得到；
 * 一般家沒有設計中這件事，打開它的網址當找不到（不顯示「設計中」）。
 */
export function shownOnDetail(o: Pick<PersonOutfit, "state">, edit: boolean): boolean {
  return o.state !== "draft" || edit;
}

export function findOutfit(
  people: Person[],
  record: string,
): { person: Person; outfit: PersonOutfit } | undefined {
  for (const person of people) {
    const outfit = person.outfits.find((o) => o.record === record);
    if (outfit) return { person, outfit };
  }
  return undefined;
}
