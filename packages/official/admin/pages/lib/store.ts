// 開發工坊頁面讀寫資料的地方：讀 admin 程式寫好的 admin.release／admin.devhome（只有 current 這筆），
// 要它馬上重算就寫一張 admin.request（pending），程式處理完回填 done；開單走一般的 api.tasks（給 admin 秘書）。
// 沒有套件自己的 RPC（D-28）。
import { api } from "@evergenie/sdk-web";
import {
  ADMIN_AGENT,
  createAndStart,
  type DevHome,
  type Release,
  type StartedTask,
  type TaskLike,
  toDevHome,
  toRelease,
} from "./release";

export const RELEASE = "admin.release";
export const DEVHOME = "admin.devhome";
export const REQUEST = "admin.request";

function isNotFound(e: unknown): boolean {
  return e instanceof Error && (e as { code?: string }).code === "not_found";
}

export function reasonOf(e: unknown): string {
  return e instanceof Error && e.message ? e.message : "連不上看板服務，請稍後再試";
}

async function current(kind: string) {
  try {
    return (await api.vault.get({ kind, id: "current" })).record ?? null;
  } catch (e) {
    if (isNotFound(e)) return null;
    throw e;
  }
}

export async function loadRelease(): Promise<Release | null> {
  return toRelease(await current(RELEASE));
}

export async function loadDevHome(): Promise<DevHome | null> {
  return toDevHome(await current(DEVHOME));
}

/** 送一張重算請求，回它的編號。 */
export async function sendRefresh(target: "all" | "release" | "devhome"): Promise<string> {
  const { id } = await api.vault.nextId({ kind: REQUEST });
  if (!id) throw new Error("沒有拿到新的編號");
  await api.vault.put({
    record: {
      kind: REQUEST,
      id,
      fields: { type: "refresh", target, state: "pending" },
      archived: false,
    },
  });
  return id;
}

/** 請求的狀態：pending／done／rejected（reason 是被拒的原因）；找不到回 undefined。 */
export async function loadRequest(
  id: string,
): Promise<{ state: string; reason: string } | undefined> {
  try {
    const f = (await api.vault.get({ kind: REQUEST, id })).record?.fields ?? {};
    return {
      state: typeof f.state === "string" ? f.state : "",
      reason: typeof f.reason === "string" ? f.reason : "",
    };
  } catch (e) {
    if (isNotFound(e)) return undefined;
    throw e;
  }
}

export interface Desk {
  /** admin 秘書在名冊上的名字；沒有這位（admin 職位沒開）是空字串。 */
  name: string;
  /** admin 秘書還沒結束的單（找同一種單在不在跑用）。 */
  open: TaskLike[];
}

export async function loadDesk(): Promise<Desk> {
  const roster = await api.web.getRoster({});
  const me = (roster.agents ?? []).find((a) => a.id === ADMIN_AGENT && a.enabled);
  if (!me) return { name: "", open: [] };
  const r = await api.tasks.list({
    agent: ADMIN_AGENT,
    statuses: ["idea", "doing", "waiting_user", "waiting_external"],
    ideas_only: false,
    include_archived: false,
  });
  return { name: me.name || ADMIN_AGENT, open: r.tasks ?? [] };
}

/** 開一張給 admin 秘書的任務單並開工（主人已在畫面內確認過，見 createAndStart）。 */
export function openTask(title: string, body: string): Promise<StartedTask> {
  return createAndStart(api.tasks, { title, body, agent: ADMIN_AGENT });
}
