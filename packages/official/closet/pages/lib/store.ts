// 衣櫃套件讀寫資料的地方：全部走 @evergenie/sdk-web 的 api.vault（D-28，沒有套件自己的 RPC）。
// 頁面不能寫生效的 closet.unlock：兌換只能寫一筆 closet.request（pending），由套件的程式驗證後處理。
import { api } from "@evergenie/sdk-web";
import {
  type ActionTarget,
  actionRequest,
  CAPABILITIES,
  type Capabilities,
  type ClosetAction,
  NO_CAPABILITIES,
  toCapabilities,
} from "./caps";
import {
  type ClosetState,
  mineAvailable,
  type PackStatus,
  type Person,
  toPerson,
  toRequest,
  toState,
  type UnlockRequest,
  type VariantSwitch,
  variantSwitches,
  type WearAction,
} from "./closet";
import {
  canDraft,
  commentFields,
  commentMessage,
  type EditNote,
  type EditOutfit,
  type EditPhoto,
  fullTaken,
  givenRequestId,
  hasClips,
  hiddenPatch,
  nextOrder,
  publishBlocked,
  type StatusRecords,
  T as T_EDIT,
  toEditNote,
  toEditOutfit,
  toEditPhoto,
  type Unadopted,
  unadoptedOf,
} from "./edit";
import { type PromoteRequest, promoteFields, toPromoteRequest } from "./promote";

export const PERSON = "closet.person";
export const STATE = "closet.state";
export const ROTATION = "closet.rotation";
export const REQUEST = "closet.request";
export const OUTFIT = "closet.outfit";
export const VARIANT = "closet.variant";
export const NOTE = "closet.note";
export const PHOTO = "closet.photo";
/** closet-mine 的請求（升成官方，CMI-14）：「修改」分頁的升成官方照舊寫這個種類，由 closet-mine 的程式處理（DEV-160 審查 S1）。 */
export const MINE_REQUEST = "closet-mine.request";

/** 「修改」分頁的評論要叫的秘書：art 職位（生圖秘書）。只有開發家才能有這個職位，開發家也不一定有（CLO-23）。 */
export const NOTIFY_TO = "art";

/**
 * 生圖秘書的狀態（CLO-23）：ready＝名冊上有 art 而且啟用中（傳話給她）；off＝在名冊上但停用；
 * none＝名冊上沒有 art；unknown＝讀不到名冊（照舊試著傳，叫不到再提醒）。
 */
export type ArtStatus = "ready" | "off" | "none" | "unknown";

/** 看名冊判斷生圖秘書在不在：get_roster 只列啟用中的；不在裡面再用 get_agents（含停用）分辨「停用」與「沒有」。 */
export async function loadArtStatus(): Promise<ArtStatus> {
  try {
    const r = await api.web.getRoster({});
    if ((r.agents ?? []).some((a) => a.id === NOTIFY_TO)) return "ready";
  } catch {
    return "unknown";
  }
  try {
    const all = await api.web.getAgents({});
    return (all.agents ?? []).some((a) => a.id === NOTIFY_TO) ? "off" : "none";
  } catch {
    return "none"; // 啟用中的確定沒有；分不出停用或沒有，就當沒有（連結一樣到「職位與秘書」）
  }
}

/** 沒有啟用中的生圖秘書：評論照樣記下，不傳話，「修改」分頁顯示說明。 */
export function artMissing(s: ArtStatus): s is "off" | "none" {
  return s === "off" || s === "none";
}

/** 送出評論的結果。 */
export interface PieceNoteResult {
  /** 評論已經記下，但後面某一步沒成功時要顯示的提醒；沒有＝都好。 */
  warn?: string;
  /** 送出當下生圖秘書的狀態；off、none 時沒有傳話（artMissing）。 */
  art: ArtStatus;
}

export async function loadPeople(): Promise<Person[]> {
  const r = await api.vault.list({ kind: PERSON, include_archived: false });
  return (r.records ?? []).map(toPerson);
}

export async function loadState(): Promise<ClosetState | null> {
  try {
    const r = await api.vault.get({ kind: STATE, id: "main" });
    return toState(r.record);
  } catch (e) {
    if (isNotFound(e)) return null;
    throw e;
  }
}

/** 衣櫃頁的隱藏版開關（CLO-17）：讀程式算好的 closet.state；還沒有就是空的。 */
export async function loadVariantSwitches(): Promise<VariantSwitch[]> {
  try {
    const r = await api.vault.get({ kind: STATE, id: "main" });
    return variantSwitches(r.record);
  } catch (e) {
    if (isNotFound(e)) return [];
    throw e;
  }
}

/**
 * 按隱藏版的開關＝改這一組每一筆 closet.variant 的 on（DEV-160 S2 起只有一組）；程式收到變更約一秒後換圖。
 * 一筆失敗就停下丟錯（已經改的留著，再按一次會補齊）。
 */
export async function setVariantOn(v: Pick<VariantSwitch, "ids">, on: boolean): Promise<void> {
  for (const id of v.ids) {
    await api.vault.patch({ kind: VARIANT, id, set: { on }, unset: [] });
  }
}

/** 展示間要不要顯示「新增角色」「加一套衣服」：看 closet-mine 套件開了沒（CLO-15）；讀不到就當沒開。 */
export async function loadMineAvailable(): Promise<boolean> {
  try {
    const r = await api.package.get({ id: "closet-mine" });
    return mineAvailable(r.package);
  } catch {
    return false;
  }
}

/** 這個家支援什麼（closet.capabilities/main，CLO-32）：還沒算好（找不到）當什麼都不支援；其他讀不到的錯照丟，由頁面說「讀不到」。 */
export async function loadCapabilities(): Promise<Capabilities> {
  try {
    return toCapabilities((await api.vault.get({ kind: CAPABILITIES, id: "main" })).record);
  } catch (e) {
    if (isNotFound(e)) return NO_CAPABILITIES;
    throw e;
  }
}

/** 展示間的點數處要不要連到習慣頁：看「習慣」套件開了沒（CLO-24）；讀不到就當沒開、不連。 */
export async function loadHabitsAvailable(): Promise<boolean> {
  try {
    const r = await api.package.get({ id: "habits" });
    return mineAvailable(r.package);
  } catch {
    return false;
  }
}

/** 角色包的現況（沒有衣服時的說明用，CLO-25）；讀不到回 null（照原本的空狀態說明）。 */
export async function loadPackStatus(): Promise<PackStatus | null> {
  try {
    return await api.personas.packStatus({});
  } catch {
    return null;
  }
}

/**
 * 「現在再試」：請服務照這一版的角色包再同步一次，回給人看的一行。不帶 pointer、force（看板也會拿掉）：
 * 一律用服務自己的指標、不蓋掉主人改過的。同步沒成功也會回結果（那一行說原因）；連不上才丟錯。
 */
export async function retryPack(): Promise<string> {
  const r = await api.personas.packSync({});
  return r.message || r.reason || "";
}

function isNotFound(e: unknown): boolean {
  return e instanceof Error && (e as { code?: string }).code === "not_found";
}

/** expected_revision 對不上（別人剛改過）：vault 回 conflict。 */
function isConflict(e: unknown): boolean {
  return e instanceof Error && (e as { code?: string }).code === "conflict";
}

export function reasonOf(e: unknown): string {
  return e instanceof Error && e.message ? e.message : "連不上看板服務，請稍後再試";
}

/** 寫輪播設定：不存在用 put、存在用 patch。 */
export async function saveRotation(persona: string, outfits: string[]): Promise<void> {
  let exists = true;
  try {
    const r = await api.vault.get({ kind: ROTATION, id: persona });
    exists = !!r.record;
  } catch (e) {
    if (!isNotFound(e)) throw e;
    exists = false;
  }
  if (exists) {
    await api.vault.patch({ kind: ROTATION, id: persona, set: { outfits }, unset: [] });
  } else {
    await api.vault.put({
      record: { kind: ROTATION, id: persona, fields: { outfits }, archived: false },
    });
  }
}

// 同一套衣服的寫入排隊（一個接一個；「修改」分頁改一般／隱藏、改類型用）：連按好幾張時，後一個一定讀得到前一個寫完的結果，不會互相蓋掉。
// 排隊只管這個頁面；別的分頁、秘書同時改的靠 expected_revision（對不上就說清楚、不蓋掉）。
const queues = new Map<string, Promise<unknown>>();

function serial<R>(record: string, work: () => Promise<R>): Promise<R> {
  const prev = queues.get(record) ?? Promise.resolve();
  const next = prev.then(work, work);
  const tail = next.then(
    () => undefined,
    () => undefined,
  );
  queues.set(record, tail);
  void tail.then(() => {
    if (queues.get(record) === tail) queues.delete(record);
  });
  return next;
}

/** 寫一筆 pending 的請求（兌換、現在穿這套、復原），回請求編號；由套件的程式驗證處理。 */
async function sendRequest(type: string, persona: string, outfit: string): Promise<string> {
  const { id } = await api.vault.nextId({ kind: REQUEST });
  if (!id) throw new Error("沒有拿到新的編號");
  await api.vault.put({
    record: {
      kind: REQUEST,
      id,
      fields: { type, persona, outfit, state: "pending" },
      archived: false,
    },
  });
  return id;
}

/** 送出兌換請求（pending），回請求編號。 */
export function sendUnlock(persona: string, outfit: string): Promise<string> {
  return sendRequest("unlock", persona, outfit);
}

/** 送出「現在穿這套」或它的復原（CLO-30）；頁面不直接改輪播設定的 wear，能不能穿由程式判斷。 */
export function sendWear(persona: string, action: WearAction): Promise<string> {
  return sendRequest(action.type, persona, action.outfit);
}

export async function loadRequest(id: string): Promise<UnlockRequest | undefined> {
  try {
    const r = await api.vault.get({ kind: REQUEST, id });
    return r.record ? toRequest(r.record) : undefined;
  } catch (e) {
    if (isNotFound(e)) return undefined;
    throw e;
  }
}

// ---- 開發家的「修改」分頁（DEV-160 S6，CLO-35）：照片紀錄、整套狀態、評論、外掛動作 ----

/** 「修改」分頁要的全部資料：照片、整套、評論、檢視（照片的網址、名字、能不能升官方）、能力與外掛動作的狀態紀錄。 */
export interface EditData {
  people: Person[];
  outfits: EditOutfit[];
  photos: EditPhoto[];
  notes: EditNote[];
  caps: Capabilities;
  statuses: StatusRecords;
  /** closet-mine 開著（升成官方要它的程式處理；沒開就不出現「升成官方」，CLO-29）。 */
  mine: boolean;
}

async function listAll(kind: string) {
  return (await api.vault.list({ kind, include_archived: false })).records ?? [];
}

/**
 * 開發家展示間到「修改」分頁的入口（CLO-37）：這位角色還沒採用的照片幾張（跟「修改」分頁同一份紀錄、同一個算法，unadoptedOf；
 * 隱藏版開關關著、「修改」分頁不列的隱藏版照 closet.person 的 hidden_off 不算，S3a）。讀不到就丟錯，頁面不顯示入口（不擋展示間）。
 */
export async function loadUnadopted(persona: string): Promise<Unadopted> {
  const where = { persona };
  const [outfits, photos, person] = await Promise.all([
    api.vault.list({ kind: OUTFIT, where, include_archived: false }),
    api.vault.list({ kind: PHOTO, where, include_archived: false }),
    api.vault.get({ kind: PERSON, id: persona }).catch((e: unknown) => {
      if (isNotFound(e)) return { record: undefined };
      throw e;
    }),
  ]);
  return unadoptedOf(
    persona,
    (outfits.records ?? []).map(toEditOutfit),
    (photos.records ?? []).map(toEditPhoto),
    person.record && !person.record.archived ? [toPerson(person.record)] : [],
  );
}

/** 讀「修改」分頁的資料；外掛動作的狀態紀錄讀不到（那個套件停了）就當沒有，不擋整頁。 */
export async function loadEditData(): Promise<EditData> {
  const [people, outfits, photos, notes, caps, mine] = await Promise.all([
    loadPeople(),
    listAll(OUTFIT),
    listAll(PHOTO),
    listAll(NOTE),
    loadCapabilities(),
    loadMineAvailable(),
  ]);
  const kinds = [...new Set(caps.actions.flatMap((a) => (a.status ? [a.status.kind] : [])))];
  const statuses: StatusRecords = {};
  await Promise.all(
    kinds.map(async (kind) => {
      try {
        statuses[kind] = await listAll(kind);
      } catch (e) {
        // 不擋整頁，但留一行紀錄：狀態字沒出來時才看得出是讀不到（憲法第 7 條）。
        console.warn(`衣櫃「修改」分頁讀不到外掛動作的狀態紀錄 ${kind}，先當沒有`, e);
        statuses[kind] = [];
      }
    }),
  );
  return {
    people,
    outfits: outfits.map(toEditOutfit),
    photos: photos.map(toEditPhoto),
    notes: notes.map(toEditNote),
    caps,
    statuses,
    mine,
  };
}

/** 讀一張照片現在的樣子（按鈕前重讀）；不見了就說清楚。 */
async function freshPhoto(id: string): Promise<EditPhoto> {
  try {
    const r = await api.vault.get({ kind: PHOTO, id });
    if (!r.record || r.record.archived) throw new Error(T_EDIT.photoGone);
    return toEditPhoto(r.record);
  } catch (e) {
    if (isNotFound(e)) throw new Error(T_EDIT.photoGone);
    throw e;
  }
}

/**
 * 採用／取消採用一張照片（closet.photo 的 adopted）。seen 是畫面上那張（檔與紀錄版本）：寫入前重讀，檔或版本不一樣
 * （秘書剛用換圖請求換了新圖、或別的分頁剛改過）就不寫，請主人再看一次（DEV-160 審查 S2：不採用沒看過的圖）；
 * 寫入帶 expected_revision，重讀到寫入之間被改也一樣擋。
 */
export async function setPhotoAdopted(
  id: string,
  adopted: boolean,
  seen: Pick<EditPhoto, "file" | "revision">,
): Promise<void> {
  const r = await api.vault.get({ kind: PHOTO, id }).catch((e: unknown) => {
    if (isNotFound(e)) throw new Error(T_EDIT.photoGone);
    throw e;
  });
  if (!r.record || r.record.archived) throw new Error(T_EDIT.photoGone);
  const cur = toEditPhoto(r.record);
  if (cur.file !== seen.file || cur.revision !== seen.revision)
    throw new Error(T_EDIT.photoChanged);
  try {
    await api.vault.patch({
      kind: PHOTO,
      id,
      set: { adopted },
      unset: [],
      expected_revision: r.record.revision,
    });
  } catch (e) {
    if (isConflict(e)) throw new Error(T_EDIT.photoChanged);
    throw e;
  }
}

/**
 * 一般版／隱藏版（closet.photo 的 hidden；只是狀態，不搬檔）：照 hiddenPatch，撞號就放到最後一張、配的影片一起改，
 * 衣櫃下一輪發佈後每張都對得到圖。影片不能自己改（跟著配的那張半身照）。跟改類型同一個排隊、帶 expected_revision。
 * 不給做的（寫入前用最新的紀錄再判斷一次，不信畫面）：自己的隱藏版改成一般版（source = mine 的隱藏版，或 closet.person
 * 的 owner_hidden；讀不到檢視就當它是，寧可多擋；跟著改的影片也一樣）、全身照改到已經有全身照的那一版（fullTaken）。
 * 這張已經是那一版、但配它的影片還沒跟上（上次寫到一半）時，用這張現在的版本再呼叫一次就把影片補齊（影片卡的「跟上半身」）。
 */
export async function setPhotoHidden(id: string, hidden: boolean): Promise<void> {
  const first = await freshPhoto(id);
  if (first.type === "video") throw new Error(T_EDIT.videoLevelFollows);
  await serial(`photo-order:${first.outfit}`, async () => {
    const r = await api.vault.get({ kind: PHOTO, id }).catch((e: unknown) => {
      if (isNotFound(e)) throw new Error(T_EDIT.photoGone);
      throw e;
    });
    if (!r.record || r.record.archived) throw new Error(T_EDIT.photoGone);
    const cur = toEditPhoto(r.record);
    const same = (
      (
        await api.vault.list({
          kind: PHOTO,
          where: { outfit: cur.outfit },
          include_archived: false,
        })
      ).records ?? []
    ).map(toEditPhoto);
    const { set, videos } = hiddenPatch(same, cur, hidden);
    // 改成一般版：這張與要跟著改的影片都不能是自己的隱藏版（source = mine 的隱藏版，或檢視的 owner_hidden）。
    if (!hidden && (cur.hidden !== hidden || videos.length > 0)) {
      const owner = await ownerHiddenIds(cur.persona);
      const mine = (x: EditPhoto) => (x.hidden && x.source === "mine") || owner.has(x.id);
      if ((cur.hidden && mine(cur)) || same.some((x) => videos.includes(x.id) && mine(x)))
        throw new Error(T_EDIT.ownerHiddenStays);
    }
    if (cur.hidden !== hidden) {
      if (cur.type === "full" && fullTaken(same, cur, hidden)) throw new Error(T_EDIT.fullTaken);
      await api.vault.patch({
        kind: PHOTO,
        id,
        set,
        unset: [],
        expected_revision: r.record.revision,
      });
    }
    for (const v of videos)
      await api.vault.patch({ kind: PHOTO, id: v, set: { hidden }, unset: [] });
  });
}

/** 這位角色的自己的隱藏版編號（closet.person 的 owner_hidden）；檢視讀不到時當每張都是（寧可多擋）。 */
async function ownerHiddenIds(persona: string): Promise<{ has: (id: string) => boolean }> {
  const r = await api.vault.get({ kind: PERSON, id: persona }).catch((e: unknown) => {
    if (isNotFound(e)) return null;
    throw e;
  });
  if (!r?.record || r.record.archived) return { has: () => true };
  const ids = new Set(toPerson(r.record).ownerHidden);
  return { has: (x) => ids.has(x) };
}

/**
 * 改類型（只有半身↔全身；影片不能改）：用最新的紀錄算 order，放到那一類最後一張。
 * 配了影片的半身不能改成全身（影片只配半身照，closet.photo 的 of）。同一套的改類型排隊一個接一個，
 * 寫入帶 expected_revision：連按兩張不會拿到同一個 order，別人同時改了這張就說清楚、不蓋掉。
 */
export async function setPhotoType(id: string, to: "half" | "full"): Promise<void> {
  const first = await freshPhoto(id);
  await serial(`photo-order:${first.outfit}`, async () => {
    const r = await api.vault.get({ kind: PHOTO, id }).catch((e: unknown) => {
      if (isNotFound(e)) throw new Error(T_EDIT.photoGone);
      throw e;
    });
    if (!r.record || r.record.archived) throw new Error(T_EDIT.photoGone);
    const cur = toEditPhoto(r.record);
    if (cur.type === "video" || cur.type === to) return;
    const same = (
      (
        await api.vault.list({
          kind: PHOTO,
          where: { outfit: cur.outfit },
          include_archived: false,
        })
      ).records ?? []
    ).map(toEditPhoto);
    if (to === "full" && hasClips(same, cur)) throw new Error(T_EDIT.retypeHasVideo);
    if (to === "full" && fullTaken(same, cur, cur.hidden)) throw new Error(T_EDIT.fullTaken);
    await api.vault.patch({
      kind: PHOTO,
      id,
      set: { type: to, order: nextOrder(same, cur, to) },
      unset: [],
      expected_revision: r.record.revision,
    });
  });
}

/**
 * 整套上架（active）／下架（retired）／放回開發中（draft，DEV-160 審查 S1：取代以前加一套時的「先放進設計中」）：寫入前重讀；
 * 開發中的要這套每一張照片都採用了才能上架（沿用現有規則），用寫入前重讀的照片紀錄判斷，不信畫面。
 * 放回開發中只給自己的衣服（source = mine、不是基本款，canDraft）：官方的狀態跟著角色包。
 */
export async function setOutfitState(
  record: string,
  to: "active" | "retired" | "draft",
): Promise<void> {
  const r = await api.vault.get({ kind: OUTFIT, id: record }).catch((e: unknown) => {
    if (isNotFound(e)) throw new Error(T_EDIT.outfitGone);
    throw e;
  });
  if (!r.record || r.record.archived) throw new Error(T_EDIT.outfitGone);
  const o = toEditOutfit(r.record);
  if (o.state === to) return;
  if (to === "draft" && !canDraft(o)) throw new Error(T_EDIT.draftMineOnly);
  if (to === "active" && o.state === "draft") {
    const photos = (
      (await api.vault.list({ kind: PHOTO, where: { outfit: record }, include_archived: false }))
        .records ?? []
    ).map(toEditPhoto);
    const left = publishBlocked(o, photos);
    if (left > 0) throw new Error(T_EDIT.publishNotAll(left));
  }
  await api.vault.patch({
    kind: OUTFIT,
    id: record,
    set: { state: to },
    unset: [],
    expected_revision: r.record.revision,
  });
}

/**
 * 「評論」（整套 p = null，或某一張）：①寫一筆 closet.note（state = open、intent = comment；寫不進去就整個停下）
 * ②家裡有啟用中的生圖秘書（art）才傳話給她，沒有就不傳（評論照樣記下，回傳的 art 讓頁面顯示說明，CLO-23）。
 * 評論本身不改採用狀態；秘書真的換了新圖就用換圖請求，那張自動回到還沒採用（closet-mine，CMI-22、CMI-25）。
 */
export async function sendEditComment(
  person: { persona: string; name: string },
  o: EditOutfit,
  p: EditPhoto | null,
  comment: string,
): Promise<PieceNoteResult> {
  const text = comment.trim();
  if (!text) throw new Error(T_EDIT.commentEmpty);
  const { id } = await api.vault.nextId({ kind: NOTE });
  if (!id) throw new Error(T_EDIT.noId);
  await api.vault.put({
    record: { kind: NOTE, id, fields: commentFields(o, p, text), archived: false },
  });
  const art = await loadArtStatus();
  if (artMissing(art)) return { art };
  try {
    await api.bus.send({
      to: NOTIFY_TO,
      kind: "chat",
      body: commentMessage(person, o, p, id, text),
    });
  } catch {
    return { warn: T_EDIT.commentWarn, art };
  }
  return { art };
}

/** 寫一筆升成官方的請求（closet-mine.request type = promote，不用上傳檔），回請求編號；dryRun = true 只預演。 */
export async function sendPromote(record: string, newId: string, dryRun: boolean): Promise<string> {
  const { id } = await api.vault.nextId({ kind: MINE_REQUEST });
  if (!id) throw new Error(T_EDIT.noId);
  await api.vault.put({
    record: {
      kind: MINE_REQUEST,
      id,
      fields: promoteFields(record, newId, dryRun),
      archived: false,
    },
  });
  return id;
}

/** 讀一筆 closet-mine.request（升成官方的結果）；還沒有回 undefined。 */
export async function loadMineRequest(id: string): Promise<PromoteRequest | undefined> {
  try {
    const r = await api.vault.get({ kind: MINE_REQUEST, id });
    return r.record ? toPromoteRequest(r.record) : undefined;
  } catch (e) {
    if (isNotFound(e)) return undefined;
    throw e;
  }
}

/** 按外掛動作：照 actionRequest 寫一筆那個套件的請求；那種資料不能 next_id（given）時自己給編號。衣櫃不處理這筆請求。 */
export async function sendAction(a: ClosetAction, t: ActionTarget): Promise<void> {
  const fields = actionRequest(a, t);
  if (!fields) throw new Error(T_EDIT.actionMissing);
  let id = "";
  try {
    id = (await api.vault.nextId({ kind: a.requestKind })).id ?? "";
  } catch {
    id = "";
  }
  if (!id) id = givenRequestId(a.id, Date.now());
  await api.vault.put({ record: { kind: a.requestKind, id, fields, archived: false } });
}
