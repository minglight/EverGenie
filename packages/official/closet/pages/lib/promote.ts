// 「升成官方（公開給所有人）」的純函式與字（CMI-14 的請求、CMI-16 的流程；DEV-160 審查 S1 從 closet-mine 的「衣服」分頁搬到
// 這裡的「修改」分頁，CLO-35）。套件不能互相 import 頁面程式，這份是從 closet-mine pages/lib/mine.ts 原樣搬過來的（那邊已經拿掉）。
// 請求照舊寫 closet-mine 的種類（closet-mine.request type = promote），判斷全部以 closet-mine 的程式為準：
// 只認主人本人、只在開發家、隱藏版不升、每張照片都採用了才升（CMI-14）；頁面只先擋明顯不行的新編號，顯示結果或被拒的原因。
// 讀寫在 store.ts（sendPromote、loadMineRequest），畫面在 promoteParts.tsx。

/** 每 500 毫秒讀一次請求，最多 10 秒。 */
export const POLL_MS = 500;
export const TIMEOUT_MS = 10_000;

export const T = {
  open: "升成官方（公開給所有人）",
  title: "把這套升成官方",
  hint: "先預演看會怎麼做，什麼都不會動；看過再決定。",
  newId: "官方的新編號",
  newIdHint: "小寫英文、數字與減號（例：seaside）；不能用 my- 開頭，也不能叫 base。",
  badId: "新編號要用小寫英文、數字與減號，不能用 my- 開頭，也不能叫 base",
  preview: "先預演",
  previewing: "預演中…",
  willBe: (to: string) => `會變成官方的衣服，編號 ${to}。`,
  files: (n: number) =>
    n > 0
      ? `會把 ${n} 個檔（照片、全身照、動畫）複製到官方的位置。`
      : "沒有要搬的檔（圖已經在官方的位置）。",
  oldRenamed:
    "舊的怎麼處理：原本自己的圖改名留著（檔名後面加 .bak-日期-promote）；原本那筆衣服封存，換成官方的新編號。",
  oldKept: "舊的怎麼處理：原本自己的圖改名留著（檔名後面加 .bak-日期-promote）；編號不變。",
  rotations: (n: number) => `輪流穿的設定有 ${n} 筆會跟著換成新編號。`,
  hidden: (n: number) => `注意：有 ${n} 張自己的隱藏版的照片不升，留在自己的位置。`,
  detail: "看完整的清單（搬哪些檔、怎麼退回）",
  warn: "升成官方＝可以公開給所有人：下一次建角色包時會帶出去（這位角色要在公開名單裡），收不回。",
  confirm: "確定升成官方",
  sending: "正在升成官方…",
  cancel: "先不要",
  done: (to: string) => `已經升成官方了${to ? `，編號 ${to}` : ""}。`,
  rejected: "沒有升成官方：",
  changed: "新編號改過了，要再預演一次",
  pending: "已經送出了，程式還在處理；等一下重新整理這一頁看結果。",
  sendError: "沒送出去：",
  timeout: "已送出，等程式處理（套件沒在跑時會晚一點）",
  unknownReject: "這次沒有成功",
};

const MINE_PREFIX = "my-";
const OUTFIT_ID = /^[a-z0-9][a-z0-9-]*$/;

/** 建議的新編號：去掉上傳時自動給的 my- 開頭；剩下只有數字（例 my-12）就給 outfit-12，主人可以改。 */
export function suggestNewId(outfit: string): string {
  if (!outfit.startsWith(MINE_PREFIX)) return outfit;
  const rest = outfit.slice(MINE_PREFIX.length).replace(/^-+/, "");
  if (rest === "" || /^\d+$/.test(rest)) return `outfit-${rest || "1"}`;
  return rest;
}

/** 新編號合不合格（先在頁面說，省一趟；規則跟程式一樣，最後以程式為準）：合格回空字串。 */
export function checkNewId(id: string): string {
  const t = id.trim();
  if (!OUTFIT_ID.test(t) || t.startsWith(MINE_PREFIX) || t === "base") return T.badId;
  return "";
}

/** 升成官方的請求欄位（closet-mine.request）：不帶上傳檔；dry_run = true 只預演。 */
export function promoteFields(
  record: string,
  newId: string,
  dryRun: boolean,
): Record<string, unknown> {
  const out: Record<string, unknown> = { type: "promote", outfit: record, state: "pending" };
  if (newId.trim()) out.new_id = newId.trim();
  if (dryRun) out.dry_run = true;
  return out;
}

/** 讀回來的 closet-mine.request（只用得到這幾個欄位）。 */
export interface PromoteRequest {
  id: string;
  state: string;
  reason: string;
  result: string;
  /** 程式回填的說明（搬了什麼、怎麼退回），純文字多行。 */
  detail: string;
}

const str = (v: unknown): string => (typeof v === "string" ? v.trim() : "");

export function toPromoteRequest(r: {
  id?: string;
  fields?: Record<string, unknown>;
}): PromoteRequest {
  const f = r.fields ?? {};
  return {
    id: r.id ?? "",
    state: str(f.state),
    reason: str(f.reason),
    result: str(f.result),
    detail: typeof f.detail === "string" ? f.detail : "",
  };
}

export interface PromotePreview {
  /** 會變成的官方編號（<人設>-<新 id>）。 */
  to: string;
  /** 會搬幾個檔。 */
  files: number;
  /** 幾筆輪流穿的設定會跟著換。 */
  rotations: number;
  /** 幾張自己的隱藏版的照片不升（留在 personas/mine/）。 */
  hidden: number;
  /** 編號有沒有換（換了：舊的那筆封存）。 */
  renamed: boolean;
}

/** 把程式回填的預演結果（result＝新編號、detail＝多行說明）整理成頁面要說的幾個數字。 */
export function promotePreview(record: string, result: string, detail: string): PromotePreview {
  const lines = detail.split("\n");
  const undo = lines.findIndex((l) => l.startsWith("要退回"));
  const body = undo < 0 ? lines : lines.slice(0, undo);
  return {
    to: result,
    files: body.filter((l) => l.includes(" → ")).length,
    rotations: body.filter((l) => l.includes("closet.rotation/")).length,
    hidden: body.filter((l) => l.trimStart().startsWith("注意：")).length,
    renamed: result !== "" && result !== record,
  };
}

/** 預演結果的白話幾行（會變成什麼編號、搬哪些圖、舊的怎麼處理）。 */
export function promotePreviewLines(p: PromotePreview): string[] {
  const out = [T.willBe(p.to), T.files(p.files), p.renamed ? T.oldRenamed : T.oldKept];
  if (p.rotations > 0) out.push(T.rotations(p.rotations));
  if (p.hidden > 0) out.push(T.hidden(p.hidden));
  return out;
}
