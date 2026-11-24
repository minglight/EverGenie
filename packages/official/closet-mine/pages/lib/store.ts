// closet-mine 頁面讀寫資料的地方：全部走 @evergenie/sdk-web（D-28，沒有套件自己的 RPC）。
// 上傳走 upload()（檔案放進家的附件資料夾，第 1 版 runtime/share/、第 2 版 media/share/），再寫一筆 closet-mine.request（pending），由套件的程式驗證後複製進 personas/mine/（D-30）。
import { api, upload } from "@evergenie/sdk-web";
import { CAPABILITIES, toSupports } from "./caps";
import {
  adultOn,
  buildCharacters,
  type MineCharacter,
  type MinePerson,
  type MineRequest,
  type OutfitRec,
  type PhotoRec,
  requestFields,
  type SendFields,
  toOutfitRec,
  toPerson,
  toPhotoRec,
  toRequest,
} from "./mine";

export const PERSON = "closet.person";
export const REQUEST = "closet-mine.request";
export const PHOTO = "closet.photo";
export const OUTFIT = "closet.outfit";

export async function loadPeople(): Promise<MinePerson[]> {
  const r = await api.vault.list({ kind: PERSON, include_archived: false });
  return (r.records ?? []).map(toPerson);
}

/** 所有角色：衣櫃的 closet.person ＋ 角色庫的卡（擔任哪個職位、上線了沒）。 */
export async function loadCharacters(): Promise<MineCharacter[]> {
  const [people, cards] = await Promise.all([loadPeople(), api.personas.list({})]);
  return buildCharacters(people, cards.personas ?? []);
}

/** 衣櫃的「自己的隱藏版」開關是不是開著（讀不到當關：選項完全不出現）。 */
export async function loadAdultOn(): Promise<boolean> {
  try {
    const r = await api.package.listExtensions({ package: "closet" });
    return adultOn(r.values);
  } catch {
    return false;
  }
}

/**
 * 這個家支援什麼（衣櫃寫的 closet.capabilities/main 的 supports，CLO-32）：頁面只照它決定開發家才有的東西，
 * 不問是哪種家。還沒寫、讀不到都當什麼都不支援（fail closed）。
 */
export async function loadSupports(): Promise<string[]> {
  try {
    return toSupports((await api.vault.get({ kind: CAPABILITIES, id: "main" })).record);
  } catch (e) {
    if (isNotFound(e)) return [];
    throw e;
  }
}

/** 一位角色的照片紀錄與衣服紀錄，含封存的（每張照片的鈕與「刪掉的」要用，CMI-24）。 */
export async function loadRecords(
  persona: string,
): Promise<{ photos: PhotoRec[]; outfits: OutfitRec[] }> {
  const where = { persona };
  const [photos, outfits] = await Promise.all([
    api.vault.list({ kind: PHOTO, where, include_archived: true }),
    api.vault.list({ kind: OUTFIT, where, include_archived: true }),
  ]);
  return {
    photos: (photos.records ?? []).map(toPhotoRec),
    outfits: (outfits.records ?? []).map(toOutfitRec),
  };
}

function isNotFound(e: unknown): boolean {
  return e instanceof Error && (e as { code?: string }).code === "not_found";
}

export function reasonOf(e: unknown): string {
  return e instanceof Error && e.message ? e.message : "連不上看板服務，請稍後再試";
}

/** 寫一筆請求（有檔案就先上傳），回請求編號。刪、拿回、改點數不帶檔（file = null）。 */
export async function sendRequest(
  file: File | null,
  fields: Omit<SendFields, "upload">,
): Promise<string> {
  const up = file ? (await upload(file)).url : undefined;
  const { id } = await api.vault.nextId({ kind: REQUEST });
  if (!id) throw new Error("沒有拿到新的編號");
  await api.vault.put({
    record: {
      kind: REQUEST,
      id,
      fields: requestFields({ ...fields, upload: up }),
      archived: false,
    },
  });
  return id;
}

export async function loadRequest(id: string): Promise<MineRequest | undefined> {
  try {
    const r = await api.vault.get({ kind: REQUEST, id });
    return r.record ? toRequest(r.record) : undefined;
  } catch (e) {
    if (isNotFound(e)) return undefined;
    throw e;
  }
}
