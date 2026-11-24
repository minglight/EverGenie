// closet-mine 頁面的純函式與文字（沒有 I/O，vitest 直接測）。頁面（DEV-143）：角色頁的「衣服」分頁（outfits）、
// 角色清單「新增角色」裡的「上傳半身照」（new-character）。判斷以套件的程式為準：
// 這裡的檢查只是先在本機擋掉明顯不行的（太大、副檔名或類型不對），免得白傳一次（REQ-126）。

/** 伺服器的上傳上限（20 MB）。 */
export const MAX_BYTES = 20 * 1024 * 1024;
/** 每 500 毫秒讀一次請求，最多 10 秒。 */
export const POLL_MS = 500;
export const TIMEOUT_MS = 10_000;

export const T = {
  title: "上傳衣服與角色",
  loading: "載入中",
  /** 接在「暫時載不進來」前面。 */
  loadWhat: "角色資料",
  sendError: "沒送出去：",
  sending: "處理中…",
  notFound: "衣櫃裡還沒有這位角色",
  notFoundHint:
    "角色剛建好時要等衣櫃的程式整理一下；自己建的角色在下面上傳一張半身照就有第一套衣服。",
  // 衣服分頁
  outfitsHint: "每套衣服一列，點一套看每張照片、加照片或動畫、刪掉不要的。",
  chipPhotos: (n: number) => (n > 0 ? `照片 ${n} 張` : "缺半身"),
  chipVideo: (moving: number, total: number) =>
    moving > 0 ? `動畫 ${moving}/${total} 張` : "缺動畫",
  chipHidden: (name: string, have: number, total: number) => `${name} ${have}/${total}`,
  backToAll: "全部衣服",
  // 一套的照片（CMI-24）：每張半身照、它的動畫、全身照各一列，可以換、刪。
  photosTitle: "照片",
  photoHalf: (n: number) => `第 ${n} 張`,
  photoFull: "全身照",
  photoClip: (k: number) => `動畫 ${k}`,
  photoHiddenTitle: "自己的隱藏版",
  photosLocked: "還沒兌換：兌換後才看得到每張照片。",
  photosNone: "還沒有照片。",
  replace: "換一張",
  replaceVideo: "換一支",
  addClipTo: "加動畫",
  removePhoto: "刪掉",
  removeAsk: (what: string) => `確定刪掉「${what}」？刪掉的可以在「刪掉的」拿回來。`,
  removeAskClips: (what: string, n: number) =>
    `確定刪掉「${what}」？它的 ${n} 支動畫會一起刪；刪掉的可以在「刪掉的」拿回來。`,
  removeYes: "確定刪掉",
  removeNo: "先不要",
  addOutfitOpen: "＋ 加一套衣服",
  toWardrobe: "在衣櫃看",
  toShowroom: "在展示間看",
  partHalf: "半身",
  partFull: "全身",
  partRotation: (n: number) => `輪播 ${n} 張`,
  partVideo: (n: number) => `動畫 ${n} 支`,
  missHalf: "缺半身",
  missFull: "缺全身",
  missRotation: "缺輪播照",
  missVideo: "缺動畫",
  partsLabel: "這套衣服有什麼",
  lockedNote: "還沒兌換：真圖兌換後才看得到",
  // 開發家不遮（衣櫃 CLO-27）：沒兌換的也看得到真圖與每張的狀態，標籤照樣說未兌換。
  tagPreview: "未兌換（開發家看得到全部）",
  zoom: "看大圖",
  // 整套（CMI-24）
  costTitle: "要用點數換",
  costSave: "改好了",
  costNow: (n: number) => (n === 0 ? "現在：一上傳就擁有" : `現在：要 ${n} 點兌換`),
  removeOutfit: "刪掉這套",
  removeOutfitAsk: "確定刪掉這一套（連它的照片）？刪掉的可以在「刪掉的」拿回來。",
  // 刪掉的（CMI-24）
  deletedTitle: "刪掉的",
  deletedHint: "刪掉的衣服與照片可以拿回來（照片的備份留 30 天）。",
  deletedOutfit: (name: string) => `${name}（整套）`,
  deletedPhoto: (outfit: string, what: string) => `${outfit} · ${what}`,
  deletedHalf: "半身照",
  deletedVideo: "動畫",
  restore: "拿回來",
  // 請求做完的白話
  doneRemoved: "刪掉了，可以在「刪掉的」拿回來",
  doneRestored: "拿回來了，衣櫃與展示間幾秒內會更新",
  doneCost: "改好了，衣櫃與展示間幾秒內會更新",
  toRedeem: "去兌換",
  openDetail: "看詳情",
  // 新增角色
  newTitle: "新增我的角色",
  newHint:
    "給一張半身照就建好了；建好會開到這位角色的頁，在「衣服」分頁加衣服、在「介紹」指派職位。",
  charName: "角色名",
  charDesc: "一句話介紹",
  charPhoto: "半身照",
  chooseFile: "選擇檔案",
  charPhotoHint: "PNG、JPEG 或 WEBP，20 MB 以內",
  advanced: "進階：自訂代號",
  codeLabel: "代號",
  codeHint: "my_ 開頭的小寫英文與數字（例 my_luna）；不填就自動取",
  createBtn: "建立角色",
  cancelBtn: "先不要",
  // 加衣服
  outfitsTitle: "衣服",
  outfitsEmpty: "還沒有衣服，下面上傳一張半身照就有第一套。",
  outfitCount: (n: number) => (n === 0 ? "還沒有衣服" : `${n} 套衣服`),
  outfitTitle: "加一套衣服",
  outfitHint: "上傳一張半身照就好，之後可以再加照片、全身照與動畫。",
  outfitName: "衣服名",
  outfitDesc: "一句話說明",
  outfitPhoto: "半身照",
  // 「要用點數換」（DEV-160 第 2 層，作者定）：開＝沒兌換前只看剪影，填幾點（預設 1，之後可以改）；關＝一上傳就擁有。
  paid: "要用點數換",
  paidHint: "開著：沒兌換前衣櫃只看得到剪影，用點數兌換後才看得到、才能穿；關著：一上傳就擁有",
  pointsLabel: "幾點",
  pointsHint: "1 到 99",
  pointsBad: "請填 1 到 99 的整數",
  addOutfitBtn: "加這套衣服",
  // 一套衣服
  addPhoto: "加一張半身照",
  addFull: "放全身照",
  adultMode: "放進自己的隱藏版",
  adultHint: "勾了之後，接下來放的照片、全身照、動畫都放進自己的隱藏版；只在自己的隱藏版開著時顯示",
  addClipHint: "同一張照片的動畫可以有好幾支，每次隨機挑一支播；要回到原位才自然",
  tagDraft: "設計中",
  tagOwned: "已擁有",
  tagLocked: "未兌換",
  tagRetired: "下架",
  cost: (n: number) => (n === 0 ? "免費" : `${n} 點`),
  // 結果
  done: "好了，衣櫃與展示間幾秒內會更新",
  timeout: "已送出，等程式處理（套件沒在跑時會晚一點）",
  unknownReject: "這次沒有成功",
  // 本機檢查
  tooBig: "檔案太大（上限 20 MB）",
  imageOnly: "只收 PNG、JPEG、WEBP 圖片",
  videoOnly: "動畫只收 MP4",
  needFile: "請選一個檔案",
  needName: "請填名稱",
  nameTooLong: "名稱最多 40 字",
  badCode: "代號要用 my 開頭的小寫英數",
};

export type FileWant = "image" | "video";

const IMAGE_MIME = new Set(["image/png", "image/jpeg", "image/webp"]);
const IMAGE_EXT = new Set(["png", "jpg", "jpeg", "webp"]);

function extOf(name: string): string {
  const i = name.lastIndexOf(".");
  return i < 0 ? "" : name.slice(i + 1).toLowerCase();
}

export interface FileLike {
  name: string;
  size: number;
  type: string;
}

/**
 * 本機先擋：大小、類型。類型看瀏覽器給的 MIME，沒有 MIME 時看副檔名；回空字串＝可以送。
 * 內容真的是不是圖由伺服器端的程式看檔案開頭判斷（偽裝成 .png 的文字檔會被它拒絕）。
 */
export function checkFile(file: FileLike | null | undefined, want: FileWant): string {
  if (!file) return T.needFile;
  if (file.size > MAX_BYTES) return T.tooBig;
  const ext = extOf(file.name);
  if (want === "video") {
    const ok = file.type ? file.type === "video/mp4" : ext === "mp4";
    return ok ? "" : T.videoOnly;
  }
  const ok = file.type ? IMAGE_MIME.has(file.type) : IMAGE_EXT.has(ext);
  return ok ? "" : T.imageOnly;
}

export function validCode(code: string): boolean {
  return /^my(_[a-z0-9]{1,24}|[a-z0-9]{1,20})$/.test(code);
}

/** 名稱欄位的檢查（角色名、衣服名）；回空字串＝可以。 */
export function checkName(name: string): string {
  const n = name.trim();
  if (n === "") return T.needName;
  return [...n].length > 40 ? T.nameTooLong : "";
}

// ---- 讀回來的資料 ----

export interface RecordLike {
  id?: string;
  fields?: Record<string, unknown>;
}

export interface MinePhoto {
  url: string;
  real: boolean;
  /** 這張照片現在有幾支動畫（DEV-125，最多 MAX_CLIPS）。 */
  clips: number;
}

/** 同一張照片最多幾支動畫（跟 closet-mine 的 MaxClips 一致）。 */
export const MAX_CLIPS = 6;

/** 一張照片在某一版有沒有圖、幾支動畫（衣櫃程式算的，CLO-21）。 */
export interface SlotCell {
  img: boolean;
  videos: number;
}

/** 一套衣服每張照片、全身照在正常版與各隱藏版的狀態；只有擁有的與設計中的衣服有。 */
export interface OutfitStatus {
  photos: { n: number; std: SlotCell; hidden: Record<string, SlotCell> }[];
  full: { std: boolean; hidden: Record<string, boolean> };
}

/** 這個家能用的隱藏版（自己的隱藏版開關關著時不含它）。 */
export interface HiddenVersion {
  id: string;
  name: string;
}

export interface MineOutfit {
  record: string;
  outfit: string;
  name: string;
  cost: number;
  state: string;
  owned: boolean;
  /** 沒兌換、但這個家是開發家所以衣櫃程式照樣給真圖與狀態（CLO-27）；正式家永遠是 false。 */
  preview: boolean;
  /** 可以升成官方（衣櫃的程式算的，CLO-29）：開發家、官方角色、source = mine、已上架、不是自己的隱藏版；正式家永遠 false。 */
  promotable: boolean;
  source: string;
  full: string;
  photos: MinePhoto[];
  status?: OutfitStatus;
}

export interface MinePerson {
  persona: string;
  name: string;
  outfits: MineOutfit[];
  hidden: HiddenVersion[];
  /** 照片編號 → 公開網址（衣櫃程式這一輪真的放進公開資料夾的；沒擁有的、隱藏版關著的不列）。 */
  photoUrls: Record<string, string>;
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
const rec = (v: unknown): Record<string, unknown> =>
  v && typeof v === "object" ? (v as Record<string, unknown>) : {};

function toOutfit(v: unknown): MineOutfit {
  const m = rec(v);
  return {
    record: str(m.record),
    outfit: str(m.outfit),
    name: str(m.name) || str(m.outfit),
    cost: num(m.cost),
    state: str(m.state) || "active",
    owned: m.owned === true,
    preview: m.preview === true,
    promotable: m.promotable === true,
    source: str(m.source) || "official",
    full: str(m.full),
    // 只取一般版的照片：隱藏版開著時 closet 會把隱藏版的圖接在後面（帶 variant，2026-11-13），這裡的張數、縮圖、
    // 缺什麼都是算一般版；隱藏版各張有沒有圖看 status。
    photos: list(m.photos)
      .filter((p) => str(rec(p).variant) === "")
      .map((p) => ({
        url: str(rec(p).url),
        real: rec(p).real === true,
        clips: Math.max(list(rec(p).videos).length, str(rec(p).video) ? 1 : 0),
      })),
    status: toStatus(m.status),
  };
}

function toCell(v: unknown): SlotCell {
  const m = rec(v);
  return { img: m.img === true, videos: num(m.videos) };
}

function toStatus(v: unknown): OutfitStatus | undefined {
  if (!v || typeof v !== "object") return undefined;
  const m = rec(v);
  const full = rec(m.full);
  return {
    photos: list(m.photos).map((p, i) => {
      const r = rec(p);
      return {
        n: num(r.n) || i + 1,
        std: toCell(r.std),
        hidden: Object.fromEntries(Object.entries(rec(r.hidden)).map(([k, c]) => [k, toCell(c)])),
      };
    }),
    full: {
      std: full.std === true,
      hidden: Object.fromEntries(Object.entries(rec(full.hidden)).map(([k, b]) => [k, b === true])),
    },
  };
}

function toHidden(v: unknown): HiddenVersion[] {
  return list(v)
    .map((h) => ({
      id: str(rec(h).id),
      name: str(rec(h).name) || str(rec(h).id),
    }))
    .filter((h) => h.id !== "");
}

/** 總覽一列的狀態小標（DEV-143）：照片幾張、全身、動畫幾張有、每個隱藏版有幾張。 */
export interface StatusChip {
  key: string;
  label: string;
  ok: boolean;
}

export function statusChips(o: MineOutfit, hidden: HiddenVersion[]): StatusChip[] {
  const st = o.status;
  if (!st) return [];
  const total = st.photos.length;
  const std = st.photos.filter((p) => p.std.img).length;
  const moving = st.photos.filter((p) => p.std.videos > 0).length;
  const chips: StatusChip[] = [
    { key: "photos", label: T.chipPhotos(std), ok: std > 0 },
    {
      key: "full",
      label: st.full.std ? T.partFull : T.missFull,
      ok: st.full.std,
    },
    { key: "video", label: T.chipVideo(moving, std), ok: moving > 0 },
  ];
  for (const h of hidden) {
    const have = st.photos.filter((p) => p.hidden[h.id]?.img).length;
    chips.push({
      key: `h-${h.id}`,
      label: T.chipHidden(h.name, have, total),
      ok: have > 0,
    });
  }
  return chips;
}

export function toPerson(r: RecordLike): MinePerson {
  const f = r.fields ?? {};
  const persona = str(f.persona) || (r.id ?? "");
  return {
    persona,
    name: str(f.name) || persona,
    outfits: list(f.outfits).map(toOutfit),
    hidden: toHidden(f.hidden),
    photoUrls: Object.fromEntries(
      Object.entries(rec(f.photo_urls)).flatMap(([k, v]) => (str(v) ? [[k, str(v)]] : [])),
    ),
  };
}

export interface MineRequest {
  id: string;
  state: string;
  reason: string;
  result: string;
}

export function toRequest(r: RecordLike): MineRequest {
  const f = r.fields ?? {};
  return {
    id: r.id ?? "",
    state: str(f.state),
    reason: str(f.reason),
    result: str(f.result),
  };
}

export type Outcome =
  | { kind: "waiting" }
  | { kind: "done"; text: string; result?: string }
  | { kind: "rejected"; text: string }
  | { kind: "timeout"; text: string };

/**
 * 讀到的請求 → 要顯示的結果；req 是 undefined 表示還沒讀到。done 帶程式回填的 result（影響的衣服編號）；
 * doneText 是做完時要說的話（刪、拿回、改點數各自說，沒給用 T.done）。
 */
export function requestOutcomeText(
  req: MineRequest | undefined,
  timedOut: boolean,
  doneText: string = T.done,
): Outcome {
  if (req?.state === "done") {
    return req.result
      ? { kind: "done", text: doneText, result: req.result }
      : { kind: "done", text: doneText };
  }
  if (req?.state === "rejected") return { kind: "rejected", text: req.reason || T.unknownReject };
  if (timedOut) return { kind: "timeout", text: T.timeout };
  return { kind: "waiting" };
}

/** 角色是不是「我的」：第一套 base 的來源是 mine（closet-mine 建的角色）。 */
export function isMinePerson(p: MinePerson): boolean {
  return p.outfits.some((o) => o.outfit === "base" && o.source === "mine");
}

export interface PersonaLike {
  slug?: string;
  name?: string;
  source?: string;
  status?: string;
  seats?: string[];
  shadowed: boolean;
  avatar_url?: string;
}

/** 這頁的一位角色：角色庫的卡（坐哪、上線了沒）＋衣櫃裡她的衣服。 */
export interface MineCharacter {
  persona: string;
  name: string;
  mine: boolean;
  /** 設計中的卡不能指派（D-31）。 */
  design: boolean;
  seats: string[];
  /** 總覽的照片：有人坐就用現在穿的頭像，否則第一張真圖。 */
  thumb: string;
  outfits: MineOutfit[];
  hidden: HiddenVersion[];
  photoUrls: Record<string, string>;
}

function firstReal(outfits: MineOutfit[]): string {
  const base = outfits.find((o) => o.outfit === "base");
  for (const o of base ? [base, ...outfits] : outfits) {
    const p = o.photos.find((x) => x.real && x.url);
    if (p) return p.url;
  }
  return "";
}

/**
 * 所有角色：角色庫的卡（被官方蓋住的不算）加上衣櫃裡有、角色庫沒有的；
 * 有人坐的在前（依位置），再來是我的、官方的（各依代號）（REQ-233）。
 */
export function buildCharacters(people: MinePerson[], cards: PersonaLike[]): MineCharacter[] {
  const byPersona = new Map(people.map((p) => [p.persona, p]));
  const out: MineCharacter[] = [];
  const seen = new Set<string>();
  for (const c of cards) {
    const slug = c.slug ?? "";
    if (!slug || c.shadowed || seen.has(slug)) continue;
    seen.add(slug);
    const person = byPersona.get(slug);
    const outfits = person?.outfits ?? [];
    const seats = [...(c.seats ?? [])].sort();
    out.push({
      persona: slug,
      name: c.name || person?.name || slug,
      mine: c.source === "mine" || (person ? isMinePerson(person) : false),
      design: c.status === "design",
      seats,
      thumb: (seats.length > 0 ? c.avatar_url : "") || firstReal(outfits),
      outfits,
      hidden: person?.hidden ?? [],
      photoUrls: person?.photoUrls ?? {},
    });
  }
  for (const p of people) {
    if (seen.has(p.persona) || p.outfits.length === 0) continue;
    out.push({
      persona: p.persona,
      name: p.name,
      mine: isMinePerson(p),
      design: false,
      seats: [],
      thumb: firstReal(p.outfits),
      outfits: p.outfits,
      hidden: p.hidden,
      photoUrls: p.photoUrls,
    });
  }
  const rank = (c: MineCharacter) => (c.seats.length > 0 ? 0 : c.mine ? 1 : 2);
  return out.sort(
    (a, b) =>
      rank(a) - rank(b) ||
      (a.seats[0] ?? "").localeCompare(b.seats[0] ?? "") ||
      a.persona.localeCompare(b.persona),
  );
}

/** 角色頁「衣服」分頁的 key（核心 /personas/<代號>?tab=<套件>.<頁面>，DEV-143）。 */
export const OUTFITS_TAB = "closet-mine.outfits";

/** 一位角色的「衣服」分頁網址。 */
export function characterHref(persona: string): string {
  return persona ? `/personas/${encodeURIComponent(persona)}?tab=${OUTFITS_TAB}` : "/personas";
}

/** 新角色請求做完回填的 result 是 `<代號>-base`（CMI-4）；拿出代號，對不上回空字串。 */
export function personaFromResult(result: string | undefined): string {
  const r = result ?? "";
  return r.endsWith("-base") ? r.slice(0, -"-base".length) : "";
}

export interface OutfitPart {
  key: "half" | "full" | "rotation" | "video";
  ok: boolean;
  label: string;
}

/**
 * 一套衣服的四個部分有沒有（DEV-143，衣服分頁一眼看到缺什麼）：半身（第 1 張照片）、全身照、輪播（2 張以上照片）、動畫（任一張有）。
 * 只看衣櫃程式算好的 person：沒兌換的衣服只有剪影，這裡不判斷（頁面改寫「還沒兌換」，見 locked）。
 */
export function outfitParts(o: MineOutfit): OutfitPart[] {
  const n = o.photos.length;
  const clips = o.photos.reduce((sum, p) => sum + p.clips, 0);
  return [
    { key: "half", ok: n >= 1, label: n >= 1 ? T.partHalf : T.missHalf },
    {
      key: "full",
      ok: o.full !== "",
      label: o.full !== "" ? T.partFull : T.missFull,
    },
    {
      key: "rotation",
      ok: n >= 2,
      label: n >= 2 ? T.partRotation(n) : T.missRotation,
    },
    {
      key: "video",
      ok: clips >= 1,
      label: clips >= 1 ? T.partVideo(clips) : T.missVideo,
    },
  ];
}

/**
 * 這套只看得到剪影（沒兌換）：開發家沒兌換的照樣有真圖（preview，CLO-27），不算；
 * 還沒上架的（state = draft）衣櫃也公開真圖（衣櫃的 pub.Revealed），不算。
 */
export function locked(o: Pick<MineOutfit, "owned" | "preview" | "state">): boolean {
  return !o.owned && !o.preview && o.state !== "draft";
}

/** 衣櫃、展示間看這位角色的網址（closet 的頁讀 ?persona=）。 */
export function closetHref(page: "wardrobe" | "showroom", persona: string): string {
  return `/p/closet/${page}?persona=${encodeURIComponent(persona)}`;
}

/** 衣服詳情頁（closet 的 outfit 頁，兌換也在那裡）。 */
export function outfitDetailHref(record: string): string {
  return `/p/closet/outfit?id=${encodeURIComponent(record)}`;
}

/** 衣服排序：基本款在前，其他依官方→我的、再依編號；草稿最後。 */
export function sortOutfits(outfits: MineOutfit[]): MineOutfit[] {
  const rank = (o: MineOutfit) =>
    o.outfit === "base" ? 0 : o.state === "draft" ? 3 : o.source === "mine" ? 2 : 1;
  return [...outfits].sort((a, b) => rank(a) - rank(b) || a.record.localeCompare(b.record));
}

/**
 * 一套的標籤（CMI-24）。設計中、下架這兩個字只在能修改衣服的家（edit，開發家）出現：整套的狀態在角色頁「修改」分頁改（CLO-35）。
 * 一般家沒有上架／下架這件事（作者 11-15）：設計中的由衣櫃每輪自動上架，照一般的標；我的下架的列在「刪掉的」（liveOutfits、deletedOf）。
 */
export function tagsOf(o: MineOutfit, edit = false): string[] {
  if (o.state === "draft" && edit) return [T.tagDraft];
  const tags = [o.owned ? T.tagOwned : o.preview ? T.tagPreview : T.tagLocked, T.cost(o.cost)];
  if (o.state === "retired" && edit) tags.push(T.tagRetired);
  return tags;
}

/** 這套衣服現在的第一張縮圖網址（沒有回空字串）。 */
export function thumbOf(o: MineOutfit): string {
  return o.photos[0]?.url ?? "";
}

// ---- 照片與衣服的紀錄（closet.photo、closet.outfit）：一套的每張照片、「刪掉的」（CMI-24） ----
// 哪張是哪套的、第幾張、配哪張、隱藏版與否、我的還是官方的，全照衣櫃的照片紀錄（closet.photo，CLO-34），不看檔名；
// 圖的網址照 closet.person 的 photo_urls（衣櫃這一輪真的放進公開資料夾的）。能不能做由程式判斷，這裡只決定畫哪些鈕。

export type PhotoType = "half" | "full" | "video";

/** 讀回來的一筆紀錄（含封存的）。 */
export interface StoredRecord extends RecordLike {
  archived?: boolean;
  updated_at?: string;
}

export interface PhotoRec {
  id: string;
  /** closet.outfit 的編號（<人設>-<衣服 id>）。 */
  outfit: string;
  type: PhotoType;
  order: number;
  /** 只給影片：配哪一張半身照（照片編號）。 */
  of: string;
  clip: number;
  hidden: boolean;
  source: string;
  archived: boolean;
  /** 最後改的時間（刪掉的照新到舊排）。 */
  at: string;
}

export function toPhotoRec(r: StoredRecord): PhotoRec {
  const f = r.fields ?? {};
  const t = str(f.type);
  return {
    id: r.id ?? "",
    outfit: str(f.outfit),
    type: t === "full" || t === "video" ? t : "half",
    order: num(f.order),
    of: str(f.of),
    clip: num(f.clip),
    hidden: f.hidden === true,
    source: str(f.source) || "official",
    archived: r.archived === true,
    at: r.updated_at ?? "",
  };
}

export interface OutfitRec {
  record: string;
  /** 衣服 id（closet.outfit 的 outfit 欄位）。 */
  outfit: string;
  name: string;
  source: string;
  state: string;
  /** 升成官方換了編號時，新的那筆記著舊的編號。 */
  from: string;
  archived: boolean;
  at: string;
}

/**
 * 總覽要列的衣服：衣櫃算好的 closet.person 會晚幾秒才跟上，紀錄已經封存（剛刪掉）的先不列，
 * 不然同一套會同時出現在列上與「刪掉的」。紀錄還沒讀到就照 closet.person。
 * 不能修改衣服的家（一般家，edit = false）沒有下架：我的、下架的那套算刪掉的（列在「刪掉的」，deletedOf），這裡不列。
 */
export function liveOutfits(
  outfits: MineOutfit[],
  records: readonly OutfitRec[] | undefined,
  edit = false,
): MineOutfit[] {
  if (!records) return outfits;
  const gone = new Set(
    records.filter((r) => r.archived || (!edit && retiredMine(r))).map((r) => r.record),
  );
  return outfits.filter((o) => !gone.has(o.record));
}

/** 一般家當成「刪掉的」的下架衣服：還在（沒封存）、我的、不是基本款、state = retired。 */
function retiredMine(r: OutfitRec): boolean {
  return !r.archived && r.source === "mine" && r.outfit !== "base" && r.state === "retired";
}

export function toOutfitRec(r: StoredRecord): OutfitRec {
  const f = r.fields ?? {};
  const record = r.id ?? "";
  const persona = str(f.persona);
  const outfit =
    str(f.outfit) ||
    (persona && record.startsWith(`${persona}-`) ? record.slice(persona.length + 1) : record);
  return {
    record,
    outfit,
    name: str(f.name) || outfit,
    source: str(f.source) || "official",
    state: str(f.state) || "active",
    from: str(f.from),
    archived: r.archived === true,
    at: r.updated_at ?? "",
  };
}

/** 一套裡的一張（半身、全身）或一支動畫。 */
export interface PhotoItem {
  id: string;
  type: PhotoType;
  hidden: boolean;
  /** 公開網址；這一輪還沒放進公開資料夾（剛上傳、沒擁有）是空字串。 */
  url: string;
  /** 「第 N 張」「全身照」「動畫 k」（同一版裡照衣櫃排的順序）。 */
  label: string;
  /** 我的照片才能換、刪（官方的不行，程式也會拒）。 */
  mine: boolean;
  /** 只有半身照：配它的動畫（照 clip 排）。 */
  clips: PhotoItem[];
  /** 能不能單獨刪：一般版最後一張半身照不能（衣服要有封面；程式也會拒，ReasonLastHalf）。 */
  removable: boolean;
}

/** 一套的照片，一版一組（一般版；自己的隱藏版開著時多一組）。 */
export interface PhotoGroup {
  hidden: boolean;
  halves: PhotoItem[];
  full: PhotoItem | null;
}

const byOrder = (a: PhotoRec, b: PhotoRec) =>
  a.order - b.order || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);

/**
 * 一套（record）現在的照片（不含封存的），分版：一般版一定有一組；自己的隱藏版開著（adult）而且有隱藏版照片才多一組
 * （關著時隱藏版完全不出現）。半身照照 order 排、影片照 clip 接在它配的那張底下。
 */
export function photoGroups(
  photos: readonly PhotoRec[],
  record: string,
  urls: Record<string, string>,
  adult: boolean,
): PhotoGroup[] {
  const live = photos.filter((p) => !p.archived && p.outfit === record);
  const group = (hidden: boolean): PhotoGroup => {
    const same = live.filter((p) => p.hidden === hidden);
    const halves = same.filter((p) => p.type === "half").sort(byOrder);
    const fulls = same.filter((p) => p.type === "full").sort(byOrder);
    const item = (p: PhotoRec, label: string): PhotoItem => ({
      id: p.id,
      type: p.type,
      hidden: p.hidden,
      url: urls[p.id] ?? "",
      label,
      mine: p.source === "mine",
      clips: [],
      removable: true,
    });
    return {
      hidden,
      halves: halves.map((h, i) => ({
        ...item(h, T.photoHalf(i + 1)),
        removable: hidden || halves.length > 1,
        clips: same
          .filter((v) => v.type === "video" && v.of === h.id)
          .sort((a, b) => a.clip - b.clip || (a.id < b.id ? -1 : 1))
          .map((v, k) => item(v, T.photoClip(k + 1))),
      })),
      full: fulls[0] ? item(fulls[0], T.photoFull) : null,
    };
  };
  const out = [group(false)];
  if (adult && live.some((p) => p.hidden)) out.push(group(true));
  return out;
}

/** 「刪掉的」一項：一整套（restore_outfit）或一張照片（restore_photo）。 */
export interface DeletedItem {
  kind: "outfit" | "photo";
  /** outfit：衣服 id；photo：照片編號。 */
  id: string;
  /** closet.outfit 的編號。 */
  record: string;
  label: string;
  at: string;
}

/**
 * 這位角色刪掉、拿得回來的（CMI-19、CMI-20）：
 * - 整套：封存的我的衣服（基本款不算；展示間以前「不要了」的也算）；升成官方換了編號的（有還在的衣服 from 是它）不列。
 *   不能修改衣服的家（一般家，edit = false）沒有下架：我的、下架的那套也列在這裡，「拿回來」＝改回上架（restore_outfit，CMI-20）。
 * - 單張：封存的我的照片、它那套還在（一般家下架的那套算刪掉了，它的照片不另外列）；影片配的那張半身照也要還在
 *   （一起刪的影片跟著那張拿回）；隱藏版只在開關開著時列。
 * 照新到舊排。拿不拿得回來（備份還在不在等）由程式判斷，拒絕的原因照實顯示。
 */
export function deletedOf(
  outfits: readonly OutfitRec[],
  photos: readonly PhotoRec[],
  adult: boolean,
  edit = false,
): DeletedItem[] {
  const promoted = new Set(outfits.filter((o) => !o.archived && o.from).map((o) => o.from));
  const liveMine = new Map(
    outfits
      .filter((o) => !o.archived && o.source === "mine" && (edit || !retiredMine(o)))
      .map((o) => [o.record, o]),
  );
  const livePhotos = new Set(photos.filter((p) => !p.archived).map((p) => p.id));
  const out: DeletedItem[] = [];
  for (const o of outfits) {
    const gone =
      (o.archived && o.source === "mine" && o.outfit !== "base" && !promoted.has(o.record)) ||
      (!edit && retiredMine(o));
    if (!gone) continue;
    out.push({
      kind: "outfit",
      id: o.outfit,
      record: o.record,
      label: T.deletedOutfit(o.name),
      at: o.at,
    });
  }
  for (const p of photos) {
    const o = liveMine.get(p.outfit);
    if (!p.archived || p.source !== "mine" || !o || (p.hidden && !adult)) continue;
    if (p.type === "video" && !livePhotos.has(p.of)) continue;
    const what =
      p.type === "full" ? T.photoFull : p.type === "video" ? T.deletedVideo : T.deletedHalf;
    out.push({
      kind: "photo",
      id: p.id,
      record: p.outfit,
      label: T.deletedPhoto(o.name, p.hidden ? `${what}（${T.photoHiddenTitle}）` : what),
      at: p.at,
    });
  }
  return out.sort((a, b) => (a.at < b.at ? 1 : a.at > b.at ? -1 : 0));
}

/** 頁面送的請求種類（closet-mine 的 schemas/request.toml）。 */
export type RequestType =
  | "character"
  | "outfit"
  | "photo"
  | "full"
  | "video"
  | "remove_photo"
  | "restore_photo"
  | "remove_outfit"
  | "restore_outfit"
  | "set_cost";

export interface SendFields {
  type: RequestType;
  /** 上傳檔的網址；不帶檔的整理（刪、拿回、改點數）沒有。 */
  upload?: string;
  persona?: string;
  outfit?: string;
  name?: string;
  desc?: string;
  /** 舊的寫法：true＝免費。有給 cost 就不寫它。 */
  free?: boolean;
  /** outfit、set_cost：兌換要幾點（0＝免費）。 */
  cost?: number;
  /** 照片紀錄的編號：photo、full、video 是換那一張（video 寫半身照的編號＝配那一張）；remove_photo、restore_photo 必填。 */
  photoId?: string;
  photo?: number;
  /** 只給 video：這張的第幾支動畫（預設 1；換掉舊的就給舊的號碼，加一支就給現有支數 + 1）。 */
  clip?: number;
  /**
   * 只給 photo、full、video：true＝自己的隱藏版的圖（檔一樣放 personas/mine/photos/、照片紀錄 hidden = true；家設定沒打開時程式會拒絕）。
   * 頁面有畫「放進自己的隱藏版」時一律送（沒勾送 false＝明說是一般版）；沒畫（開關關著）就不給（undefined），照程式的預設。
   */
  adult?: boolean;
}

/**
 * 「要用點數換」開關＋幾點 → 要寫的 cost（DEV-160 第 2 層）：關＝0（一上傳就擁有）；開＝1 到 99 的整數，不合格回 null。
 */
export function costOf(paid: boolean, points: string): number | null {
  if (!paid) return 0;
  const t = points.trim();
  if (!/^\d{1,2}$/.test(t)) return null;
  const n = Number(t);
  return n >= 1 ? n : null;
}

/** 開關打開時「幾點」欄的預設（作者定：預設 1）；這套本來就要點數就填現在的。 */
export function pointsText(cost: number): string {
  return String(cost > 0 ? cost : 1);
}

/** 衣櫃的「自己的隱藏版」開關（擴充點 closet.adult）是不是開著：只有 on 算開，其他（含沒設）都當關。 */
export function adultOn(values: Record<string, string> | undefined): boolean {
  return (values?.["closet.adult"] ?? "").trim().toLowerCase() === "on";
}

const UPLOADS = new Set<RequestType>(["photo", "full", "video"]);

/**
 * 組要寫進 closet-mine.request 的欄位：空值不寫、state 一律 pending。
 * 頁面不送 draft（請求的 draft = true 留給秘書；開發家在「修改」分頁「放回開發中」，CLO-35）；
 * adult 有給（true 或 false）才寫，沒給就不寫（照程式的預設，CMI-13）。
 */
export function requestFields(f: SendFields): Record<string, unknown> {
  const out: Record<string, unknown> = { type: f.type, state: "pending" };
  if (f.upload) out.upload = f.upload;
  if (f.persona) out.persona = f.persona.trim();
  if (f.outfit) out.outfit = f.outfit;
  if (f.name?.trim()) out.name = f.name.trim();
  if (f.desc?.trim()) out.desc = f.desc.trim();
  if (f.photoId) out.photo_id = f.photoId;
  if (f.type === "outfit") {
    if (typeof f.cost === "number") out.cost = f.cost;
    else out.free = f.free === true;
  }
  if (f.type === "set_cost" && typeof f.cost === "number") out.cost = f.cost;
  if (f.type === "video") {
    // 寫了 photo_id 就是照編號指那一張，不用位置。
    if (!f.photoId) out.photo = f.photo && f.photo > 0 ? f.photo : 1;
    if (f.clip && f.clip > 1) out.clip = f.clip; // 第 1 支不寫（預設）
  }
  if (typeof f.adult === "boolean" && UPLOADS.has(f.type)) out.adult = f.adult;
  return out;
}
