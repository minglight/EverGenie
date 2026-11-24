import { ALL_KINDS, type BusSubscribeResponse, emitVaultChanged } from "@evergenie/sdk-web";
import { type QueryClient, type QueryKey, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { liveKeys } from "./queries";
import { apiFor, PRIMARY, reprobeSite, type SiteRef, siteKey, useOkSites } from "./sites";
import { pingPresence } from "./viewing";

const DEBOUNCE_MS = 500;

/** 別台的串流被那台拒絕（讀得到的錯誤，例如 403）之後，隔多久再開一次（同時重新試連那台）。 */
export const REOPEN_MS = 10_000;
/** 連續被拒時越等越久（10、30、60 秒，之後每 60 秒），最多等這麼久。 */
export const REOPEN_MAX_MS = 60_000;
const REOPEN_STEPS_MS = [REOPEN_MS, 30_000, REOPEN_MAX_MS];
/** 第 n 次連續被拒（從 1 算）之後等多久再開。 */
export function reopenDelay(failures: number): number {
  return REOPEN_STEPS_MS[Math.max(0, failures - 1)] ?? REOPEN_MAX_MS;
}

/** 看板訂的 bus 主題（REQ-389）：記錄變了（vault.changed）。後端只開放這一個。 */
export const LIVE_TOPICS = ["vault.changed"];

// 記錄種類 → 要重抓的畫面資料（沒開的 query 不會真的去抓）。主伺服器的 key；別台加 `["site", 台名]` 前綴。
const kindKeys: Record<string, QueryKey[]> = {
  task: [["tasks"], ["task"], ["overview"], ["menu"]],
  reminder: [["reminders"], ["reminder"], ["overview"], ["menu"]],
  goal: [["goals"], ["goal"], ["overview"], ["menu"]],
  policy_request: [["policy"], ["menu"]],
  rules_version: [["rules"]],
  rules_proposal: [["rules"]],
};

/** bus 事件要重抓哪些 query（REQ-389）：記錄看種類；其他的不理。site＝事件是哪一台的（預設主伺服器）。 */
export function keysForBusEvent(ev: BusSubscribeResponse, site: SiteRef = PRIMARY): QueryKey[] {
  if (ev.topic === "vault.changed") {
    const kind = typeof ev.payload?.kind === "string" ? ev.payload.kind : "";
    return (kindKeys[kind] ?? []).map((k) => siteKey(site, ...k));
  }
  return [];
}

/** vault.changed 轉成瀏覽器裡的事件，給套件頁面的 `onVaultChanged`（PKG-49）；其他主題不轉。 */
export function forwardVaultChange(ev: BusSubscribeResponse): void {
  if (ev.topic !== "vault.changed") return;
  const kind = typeof ev.payload?.kind === "string" ? ev.payload.kind : "";
  const id = typeof ev.payload?.id === "string" ? ev.payload.id : undefined;
  if (kind) emitVaultChanged({ kind, id });
}

const onSite = (site: SiteRef, keys: readonly QueryKey[]) => keys.map((k) => siteKey(site, ...k));

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal.aborted) return resolve();
    const done = () => {
      clearTimeout(timer);
      signal.removeEventListener("abort", done);
      resolve();
    };
    const timer = setTimeout(done, ms);
    signal.addEventListener("abort", done);
  });
}

/**
 * 看板的即時更新，每台各自一組（S3-8）：一台開一組串流（討論串、通知、記錄變化），事件只重抓那台的 key，
 * 所以某台斷線只影響那台。台清單變了（加一台、某台變得不能用、又回來）只開關那一台，其他台的串流不動。
 */
class LiveHub {
  private timer: ReturnType<typeof setTimeout> | undefined;
  private readonly pending = new Map<string, QueryKey>();
  private readonly running = new Map<string, AbortController>();
  /** 開過的台：關掉之後又開（那台回來了），先把那台的資料全部重抓一次（斷掉時漏掉的）。 */
  private readonly opened = new Set<string>();

  constructor(private readonly client: QueryClient) {}

  queue(keys: readonly QueryKey[]) {
    for (const key of keys) this.pending.set(JSON.stringify(key), key);
    clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      const due = [...this.pending.values()];
      this.pending.clear();
      for (const key of due) void this.client.invalidateQueries({ queryKey: key });
    }, DEBOUNCE_MS);
  }

  sync(sites: readonly SiteRef[]) {
    const want = new Map<string, SiteRef>(sites.map((s) => [`${s.key}\n${s.base}`, s]));
    for (const [id, ctl] of this.running) {
      if (!want.has(id)) {
        ctl.abort();
        this.running.delete(id);
      }
    }
    for (const [id, site] of want) {
      if (this.running.has(id)) continue;
      const ctl = new AbortController();
      this.running.set(id, ctl);
      if (this.opened.has(id)) this.refreshSite(site);
      this.opened.add(id);
      this.watch(site, ctl.signal);
    }
  }

  stop() {
    for (const ctl of this.running.values()) ctl.abort();
    this.running.clear();
    clearTimeout(this.timer);
  }

  private refreshSite(site: SiteRef) {
    this.queue(onSite(site, [...liveKeys, ...Object.values(kindKeys).flat()]));
    pingPresence(site.key);
  }

  private watch(site: SiteRef, signal: AbortSignal) {
    const a = apiFor(site);
    const refresh = () => this.queue(onSite(site, liveKeys));
    const refreshBus = () => {
      this.queue(onSite(site, Object.values(kindKeys).flat()));
      // 斷掉的那段（手機鎖屏、切到背景）秘書記的東西：套件頁面也全部重讀一次（PKG-49）。
      if (site.key === PRIMARY.key) emitVaultChanged({ kind: ALL_KINDS });
    };
    // 通知串流斷過：斷掉時可能漏了提醒的變動，除了重抓也重報一次那台的在場。
    const refreshNotify = () => {
      refresh();
      pingPresence(site.key);
    };
    this.keep(site, signal, refresh, () => a.session.watchThreads({}, refresh, signal, refresh));
    this.keep(site, signal, refreshNotify, () =>
      a.notify.watch(
        {},
        (ev) => {
          refresh();
          // 「還在等你」的提醒有變動（新的、換了地方、別處回答收掉了）：馬上重報一次在場（只報那台），拿最新的清單。
          if (ev.nudge) pingPresence(site.key);
        },
        signal,
        refreshNotify,
      ),
    );
    this.keep(site, signal, refreshBus, () =>
      a.bus.subscribe(
        { topics: LIVE_TOPICS },
        (ev) => {
          const keys = keysForBusEvent(ev, site);
          if (keys.length > 0) this.queue(keys);
          // 套件頁面（飲食、運動、身體…）讀的是主伺服器的 vault：轉給它們自己重讀（PKG-49）。
          if (site.key === PRIMARY.key) forwardVaultChange(ev);
        },
        signal,
        refreshBus,
      ),
    );
  }

  /**
   * 串流自己會重連（伺服器每 1 秒心跳、3 秒沒收到東西就重連；連不上 1、2、5 秒之後每 10 秒再試，transport.ts），
   * 重連成功就把它管的全部重抓一次。這裡只處理「被那台拒絕」（讀得到的錯誤，串流不會自己重試）：
   * 主伺服器照 S3 之前安靜不吵（靠進頁與回前景的重抓撐著）；別台是真的請求失敗，重新試連那台
   * （試連的結果決定那台能不能用、這組串流要不要關），還能用就隔一段時間再開（`reopenDelay`：10、30、60 秒），開之前先重抓一次。
   * 那台中途整台不在（網路錯誤）transport 會自己默默重試，這裡收不到；靠 sites.ts 每 30 秒的試連發現它 down、把這組串流關掉。
   * 「連不上／太舊」只看真的請求失敗與 get_mode，不看串流多久沒資料。
   */
  private keep(site: SiteRef, signal: AbortSignal, refresh: () => void, open: () => Promise<void>) {
    void (async () => {
      let failures = 0;
      for (let first = true; !signal.aborted; first = false) {
        if (!first) refresh();
        const started = Date.now();
        try {
          await open();
        } catch {
          // 被拒絕：下面決定要不要再開。
        }
        if (signal.aborted || site.key === PRIMARY.key) return;
        // 開了一陣子才被拒是新的狀況，從頭算；一開就被拒（例如那台不提供這條）就越等越久，不每 10 秒重抓整台。
        failures = Date.now() - started >= REOPEN_MAX_MS ? 1 : failures + 1;
        void reprobeSite(this.client, site.base);
        await sleep(reopenDelay(failures), signal);
      }
    })();
  }
}

/**
 * 畫面的即時更新（定案 10、REQ-389、S3-8）：討論串、通知的串流與 bus 的記錄變化一有事件，就（去抖 500ms）重抓對應的資料。
 * 顯示「狀態」的資料（任務、待辦、權限申請）不再靠 15／30 秒輪詢，全靠這裡。每台能用的台（`useOkSites`）各自一組串流。
 */
export function useLiveUpdates() {
  useLiveSites(useOkSites());
}

/** 對指定的台開即時更新（`useLiveUpdates` 用；測試直接給台清單）。 */
export function useLiveSites(sites: readonly SiteRef[]) {
  const client = useQueryClient();
  const [hub, setHub] = useState<LiveHub | null>(null);
  useEffect(() => {
    const h = new LiveHub(client);
    setHub(h);
    return () => h.stop();
  }, [client]);
  useEffect(() => {
    hub?.sync(sites);
  }, [hub, sites]);
}

export function invalidateLive(client: QueryClient, site: SiteRef = PRIMARY) {
  for (const key of liveKeys) {
    void client.invalidateQueries({ queryKey: siteKey(site, ...key) });
  }
}

/** 重抓全部相關的資料，等到抓完才回來（動作做完之後用）。site＝哪一台的（預設主伺服器）。 */
export async function refreshAll(client: QueryClient, site: SiteRef = PRIMARY): Promise<void> {
  await Promise.all(
    liveKeys.map((key) => client.invalidateQueries({ queryKey: siteKey(site, ...key) })),
  );
}
