import type { Nudge } from "@evergenie/sdk-web";
import { useEffect, useRef } from "react";
import { useDocumentVisible } from "../lib/visible";
import { apiFor, PRIMARY, type SiteRef } from "./sites";
import { createStore, useStore } from "./store";

// 不吵的通知（P7-6、D-34）：看板在前景時每 30 秒告訴伺服器「我正在看哪一頁」，伺服器據此決定
// 「還在等你」的提醒要出現在哪裡（頁內卡片、站內提示或手機推播），並把還在顯示的提醒回給我們。
// 這裡不自己判斷要不要顯示，只照伺服器給的清單畫（CLAUDE：伺服器算好，前端不重算）。
//
// 多台（S3-8）：每台能用的台都報（看板開著，每台都該知道「主人在看板前」，提醒才出在看板、不推到手機）；
// 正在看的那一筆（`?home=` 那台，沒寫是主伺服器）所屬的台拿到真正的路徑與討論串，其他台拿 ELSEWHERE——
// 秘書與單號兩台會撞（兩台都有 code-sec-12），不能讓 A 以為主人在看 A 的同名討論串。

export const PRESENCE_EVERY_MS = 30_000;

/** 報給「不是正在看的那台」的路徑：看板開著，但不在那台的任何一張單或討論串。 */
export const ELSEWHERE = "/";

/** 看板上要顯示的一則提醒，記著是哪一台的（`site`＝台的 key，主伺服器是 ""）。 */
export interface SiteNudge extends Nudge {
  site: string;
}

const nudges = createStore<readonly SiteNudge[]>([]);
/** 每台最近一次回報拿到的提醒（台的 key → 清單）。 */
const bySite = new Map<string, readonly SiteNudge[]>();

/** 目前還沒收掉、要在看板上顯示的提醒（每台回報時一併回傳，照台的順序接起來）。 */
export function useNudges(): readonly SiteNudge[] {
  return useStore(nudges);
}

/** 這個瀏覽器分頁的編號：同一個人可以同時開手機與桌機，伺服器分開記。 */
function newClientId(): string {
  const rand = () => Math.random().toString(36).slice(2, 10);
  try {
    const seen = sessionStorage.getItem("eg.client");
    if (seen) return seen;
    const id = `c-${rand()}${rand()}`;
    sessionStorage.setItem("eg.client", id);
    return id;
  } catch {
    return `c-${rand()}${rand()}`;
  }
}
const client = newClientId();

/** 報給一台的內容。 */
export interface PresenceTarget {
  site: SiteRef;
  path: string;
  thread: string;
}

/** 每台要報什麼：`viewed`（正在看的那台的 key）拿真的路徑與討論串，其他台拿 ELSEWHERE。 */
export function presenceTargets(
  path: string,
  thread: string,
  viewed: string,
  sites: readonly SiteRef[],
): PresenceTarget[] {
  return sites.map((site) =>
    site.key === viewed ? { site, path, thread } : { site, path: ELSEWHERE, thread: "" },
  );
}

let targets: PresenceTarget[] = [{ site: PRIMARY, path: "/", thread: "" }];
let reporting = false;

function publish(): void {
  nudges.set(targets.flatMap((t) => bySite.get(t.site.key) ?? []));
}

async function reportTo(t: PresenceTarget, away: boolean): Promise<void> {
  try {
    const res = await apiFor(t.site).notify.presence({
      client,
      path: t.path,
      thread: t.thread,
      away,
    });
    // 回來的時候已經切到背景（清單清掉了）或那台已經拿掉：不要把舊回應填回去。
    if (away || !reporting || !targets.some((x) => x.site.key === t.site.key)) return;
    bySite.set(
      t.site.key,
      (res.nudges ?? []).map((n) => ({ ...n, site: t.site.key })),
    );
    publish();
  } catch {
    // 連不上就算了：下一次（30 秒後或換頁）會再報；不打擾使用者。那台上次的提醒先留著。
  }
}

async function report(away: boolean, only?: string): Promise<void> {
  await Promise.all(
    targets.filter((t) => only === undefined || t.site.key === only).map((t) => reportTo(t, away)),
  );
}

/** 馬上重報一次（通知串流說提醒有變動時用），拿到最新的清單。site＝只報那一台（不給就全部）。 */
export function pingPresence(site?: string): void {
  if (reporting) void report(false, site);
}

/**
 * 按「知道了」：先從畫面拿掉，再告訴那則提醒所屬的台；其他地方（手機推播、別的分頁）由那台一起收。
 * site 沒給就照畫面上的提醒找；那台已經不在清單（拿掉了、連不上）就只從畫面拿掉，不送到別台。
 */
export async function resolveNudge(id: string, site?: string): Promise<void> {
  const found = nudges.get().find((n) => n.id === id && (site === undefined || n.site === site));
  const key = site ?? found?.site ?? PRIMARY.key;
  const drop = (list: readonly SiteNudge[]) => list.filter((n) => !(n.id === id && n.site === key));
  bySite.set(key, drop(bySite.get(key) ?? []));
  nudges.set(drop(nudges.get()));
  const target = targets.find((t) => t.site.key === key);
  if (!target) return;
  try {
    await apiFor(target.site).notify.resolveNudge({ id });
  } finally {
    pingPresence(key);
  }
}

/** 提醒推播的 tag：`nudge-…`，別台的是 `<台>|nudge-…`（sw.js 的 tagFor）。一般訊息的 tag 是 `M-<n>`，不算。 */
export function isNudgeTag(tag: string): boolean {
  return tag.startsWith("nudge-") || tag.includes("|nudge-");
}

/**
 * 手機通知列上的提醒推播收掉（法律第 5 條「問題沒了，通知跟著消失」，DEV-123）：不補推「已處理」之後，
 * 只有看板打開時收得到。還在等的事看板上看得到，所以不分還等不等、提醒類的都收；一般訊息不動。
 */
export async function closeNudgePushes(): Promise<void> {
  try {
    const reg = await navigator.serviceWorker?.getRegistration();
    const open = (await reg?.getNotifications()) ?? [];
    for (const n of open) if (isNudgeTag(n.tag)) n.close();
  } catch {
    // 不是每個瀏覽器都有這些 API：留著也無妨，點了一樣開那個對話。
  }
}

/**
 * 外殼用：看板在前景時回報在看哪裡；切到背景或關掉分頁時告訴伺服器「不在了」。
 * viewed＝正在看的那一筆所屬的台（`?home=` 解析出來的 key，主伺服器 ""）；sites＝要報的台（能用的台，主伺服器在內）。
 */
export function usePresenceReporter(
  path: string,
  thread: string | undefined,
  viewed = PRIMARY.key,
  sites: readonly SiteRef[] = [PRIMARY],
): void {
  const visible = useDocumentVisible();
  // 台清單換成一個字串比較：呼叫端每次給新陣列也不會重報。
  const sig = sites.map((s) => `${s.key}\n${s.base}`).join("\n\n");
  const sitesRef = useRef(sites);
  sitesRef.current = sites;
  // biome-ignore lint/correctness/useExhaustiveDependencies: sig 代表 sites 的內容（sitesRef 拿最新的那份）
  useEffect(() => {
    const before = targets;
    targets = presenceTargets(path, thread ?? "", viewed, sitesRef.current);
    for (const key of [...bySite.keys()]) {
      if (!targets.some((t) => t.site.key === key)) bySite.delete(key);
    }
    // 拿掉的台（不再能用、設定刪掉）告訴它一聲「不在了」，不然它要等 90 秒才知道；連不上就算了。
    for (const t of before) {
      if (!targets.some((x) => x.site.key === t.site.key)) void reportTo(t, true);
    }
    publish();
    if (!visible) {
      reporting = false;
      void report(true);
      bySite.clear();
      nudges.set([]);
      return;
    }
    reporting = true;
    void report(false);
    const timer = setInterval(() => void report(false), PRESENCE_EVERY_MS);
    return () => clearInterval(timer);
  }, [path, thread, visible, viewed, sig]);
  useEffect(() => {
    if (visible) void closeNudgePushes();
  }, [visible]);
  useEffect(() => {
    // 分頁關掉：用 keepalive 讓這一次請求能在頁面消失後送完；失敗就等 90 秒過期。每台各送一次。
    // 跨站也要 keepalive：S3-0b 量過，pagehide 時只有帶 keepalive 的 POST 送得到別台。
    const onHide = () => {
      for (const t of targets) {
        void fetch(`${t.site.base}/api/notify/presence`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ client, away: true }),
          keepalive: true,
        }).catch(() => {});
      }
    };
    window.addEventListener("pagehide", onHide);
    return () => window.removeEventListener("pagehide", onHide);
  }, []);
}

/**
 * 這一頁是這則提醒的「那張單／那個討論串」嗎：是的話畫成頁內卡片，不是的畫成站內提示。
 * viewed＝這一頁看的是哪一台（`?home=` 的 key，主伺服器 ""）：別台同名的單或討論串不算。
 */
export function nudgeIsHere(
  n: Pick<SiteNudge, "session" | "task"> & { site?: string },
  pathname: string,
  viewed = PRIMARY.key,
): boolean {
  if ((n.site ?? PRIMARY.key) !== viewed) return false;
  const path = pathname.split("?")[0] ?? "";
  return (
    (!!n.session && path === `/chat/${n.session}`) || (!!n.task && path === `/tasks/${n.task}`)
  );
}
