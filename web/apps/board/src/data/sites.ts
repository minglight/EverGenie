import { type Api, ApiError, api, type HomeEnv, makeApi } from "@evergenie/sdk-web";
import {
  type QueryClient,
  type QueryKey,
  queryOptions,
  useQueries,
  useQuery,
} from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { useMemo } from "react";

// 看板的多台資料來源（S3，計畫 2026-11-01-board-multi-home；FEN-28）：一個看板直接呼叫各台的 API。
// 主伺服器＝送出這個看板的那台（同源、相對網址）；別台＝主伺服器的 [[web.sites]]，經 CORS 直接連。
// 這裡只提供工具（有哪些台、每台的 api、查詢 key、網址），各頁怎麼合併畫面由各頁決定。

/** 呼叫一台需要的最少資料：查詢 key 用 `key`，網址與 api 用 `base`。`Site` 也是一個 SiteRef。 */
export interface SiteRef {
  /** 這台在看板裡的名字：主伺服器固定是 ""（不用等 get_mode 就能發查詢；網址不帶 ?home）；別台是它的台名（get_mode 的 label）。 */
  key: string;
  /** 網址前綴：主伺服器是 ""（相對網址，跟 S3 之前一樣）；別台是整理過的 origin（https://…，不帶結尾斜線）。 */
  base: string;
}

/**
 * 一台的狀態：ok＝可以用；old＝連得到但版本比這個看板舊（只在那台的區塊提示）；
 * denied＝到得了但被拒（沒允許這個看板，或是 S3 之前的版本）；down＝到不了；checking＝還在試連。
 */
export type SiteState = "ok" | "old" | "denied" | "down" | "checking";

export interface Site extends SiteRef {
  primary: boolean;
  /** get_mode 的 label（owner-machine）；主伺服器還沒讀到、別台連不上時是 ""。 */
  label: string;
  machine: string;
  env: HomeEnv | "";
  version: string;
  /** 那台看板介面的等級（sdk.BoardAPILevel）；連不上是 0。 */
  boardApi: number;
  /** 那台的對外網址（web.public_url）；沒設是 ""。 */
  publicUrl: string;
  /** 設定的顏色（[[web.sites]] color）；沒設是 ""，由畫面照 machine 給預設色。 */
  color: string;
  state: SiteState;
  /**
   * 被拒的原因（給設定頁顯示）：那台伺服器回的 403 訊息（沒設 owner_login、不是主人的裝置…）、
   * 正式／開發不同；讀不到原因（沒回 CORS 標頭，多半是沒允許這個看板或太舊）時是 ""。
   */
  reason: string;
}

/** 主伺服器：所有不指定台的查詢、api、網址都是它。 */
export const PRIMARY: SiteRef = { key: "", base: "" };

/** 一台的 api：主伺服器就是原本的 `api`（同一個物件，測試 mock 照常有效）；別台照 base 做一份並記住。 */
const apis = new Map<string, Api>();
export function apiFor(site: Pick<SiteRef, "base">): Api {
  if (site.base === "") return api;
  let found = apis.get(site.base);
  if (!found) {
    found = makeApi(site.base);
    apis.set(site.base, found);
  }
  return found;
}

/**
 * 查詢 key：別台一律加 `["site", 台名]` 前綴，兩台的同一種資料不會共用快取。
 * 主伺服器不加前綴（跟 S3 之前完全一樣），所以現有的 `invalidateQueries({ queryKey: ["menu"] })` 照常只動主伺服器。
 */
export function siteKey(site: Pick<SiteRef, "key">, ...rest: readonly unknown[]): QueryKey {
  return site.key === "" ? [...rest] : ["site", site.key, ...rest];
}

/**
 * 那台的網址：伺服器給的相對網址（頭像 `/avatars/x.png`、附件 `/share/x`）補成那台的絕對網址；
 * 主伺服器維持相對網址。已經是完整網址（https:、data:、blob:、//）或空字串就原樣回傳。
 */
export function siteHref(site: Pick<SiteRef, "base">, rel: string): string;
export function siteHref(site: Pick<SiteRef, "base">, rel: string | undefined): string | undefined;
export function siteHref(site: Pick<SiteRef, "base">, rel: string | undefined): string | undefined {
  if (!rel || site.base === "") return rel;
  if (/^[a-z][a-z0-9+.-]*:/i.test(rel) || rel.startsWith("//")) return rel;
  return rel.startsWith("/") ? `${site.base}${rel}` : `${site.base}/${rel}`;
}

/** get_mode 讀到的這台是誰（主伺服器的外殼標籤也用這份，`homeQuery`）。 */
export interface SiteMode {
  env: HomeEnv;
  label: string;
  machine: string;
  userName: string;
  version: string;
  boardApi: number;
  publicUrl: string;
}

async function readMode(site: Pick<SiteRef, "base">): Promise<SiteMode> {
  const r = await apiFor(site).system.getMode({});
  return {
    env: r.env ?? "prod",
    label: r.label ?? "",
    machine: r.machine ?? "",
    userName: r.user_name ?? "",
    version: r.version ?? "",
    boardApi: r.board_api ?? 0,
    publicUrl: r.public_url ?? "",
  };
}

// 這個家是誰（標籤 owner-machine、env 正式／開發、版本、看板介面等級；home.toml [runtime]）：
// 外殼的家標籤、開發家的橘色橫條與分頁標題、多台的「版本太舊」都用它。跟 modeQuery 同一個服務但 key 不同：設定頁切模式只失效 modeQuery。
export const homeQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "system.get_mode", "home"),
    queryFn: () => readMode(site),
    staleTime: 5 * 60_000,
    retry: 1,
  });

/** 看板設定的一台（主伺服器 web.get_sites 回的 [[web.sites]]）。 */
export interface SiteConfig {
  url: string;
  color?: string;
}

// 這個看板還看哪些台（主伺服器的 [[web.sites]]，S3-3 的 web.get_sites）。
// 伺服器存的網址已經整理過（platform.NormalizeOrigin）；讀不到（舊版、錯誤）時當成只有主伺服器。
// 設定頁改了台清單之後 invalidate 這個 key（S3-9）。
export const SITES_CONFIG_KEY = ["web.get_sites"] as const;
export const sitesConfigQuery = queryOptions({
  queryKey: SITES_CONFIG_KEY,
  queryFn: async (): Promise<SiteConfig[]> =>
    ((await api.web.getSites({})).sites ?? []).flatMap((s) =>
      s.url ? [{ url: s.url, ...(s.color ? { color: s.color } : {}) }] : [],
    ),
  staleTime: 60_000,
  retry: false,
});

/**
 * 試連一台的結果（還沒跟主伺服器比過介面等級與正式／開發）。連不上時帶 `last`＝這個分頁上次讀到的那台是誰，
 * 斷線時 key 仍是台名（`?home=`、快取、篩選都接得回來），不會變成網址。
 */
export type Probe =
  | { reach: "ok"; mode: SiteMode }
  | { reach: "denied" | "down"; last?: SiteMode; reason?: string };

/** 試連等多久：tailnet 上關機的電腦，瀏覽器可能一分鐘以上才放棄；等不到就當到不了。 */
export const PROBE_TIMEOUT_MS = 8_000;

/** 別台每隔多久自己再試連一次：連不上的回來了不用等使用者切回視窗，ok 的中途斷了也看得出來。 */
export const PROBE_RETRY_MS = 30_000;

class ProbeTimeout extends Error {}

function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(new ProbeTimeout()), ms);
    p.then(
      (v) => {
        clearTimeout(timer);
        resolve(v);
      },
      (e: unknown) => {
        clearTimeout(timer);
        reject(e);
      },
    );
  });
}

/** 這個分頁上次讀到的每台是誰（base → mode）：斷線時還知道台名。 */
const lastMode = new Map<string, SiteMode>();

/**
 * 試連一台：get_mode 讀得到＝ok；伺服器回了讀得到的 403（帶 CORS 標頭）＝被拒；其他讀得到的錯誤（500…）＝到不了。
 * 連線失敗與 unavailable（含代理回的 502、503、504）分不出是「沒允許／太舊（沒回 CORS 標頭，瀏覽器不給讀）」還是「到不了」，
 * 就用 no-cors 抓那台的 manifest：抓得到（不透明回應）＝到得了但被拒；抓不到＝到不了。
 * 已知限制：走 Tailscale 服務時，那台的 serve 停了會由 tailscaled 回 502，no-cors 也抓得到，這裡會判成被拒。
 */
export async function probeSite(base: string, timeoutMs = PROBE_TIMEOUT_MS): Promise<Probe> {
  const failed = (reach: "denied" | "down", reason?: string): Probe => {
    const last = lastMode.get(base);
    return { reach, ...(last ? { last } : {}), ...(reason ? { reason } : {}) };
  };
  try {
    const mode = await withTimeout(readMode({ base }), timeoutMs);
    lastMode.set(base, mode);
    return { reach: "ok", mode };
  } catch (err) {
    if (err instanceof ApiError && err.code === "forbidden") return failed("denied", err.message);
    if (err instanceof ApiError && err.code !== "unavailable") return failed("down");
    if (err instanceof ProbeTimeout) return failed("down");
  }
  try {
    await fetch(`${base}/manifest.webmanifest`, {
      mode: "no-cors",
      cache: "no-store",
      signal: AbortSignal.timeout(timeoutMs),
    });
    return failed("denied");
  } catch {
    return failed("down");
  }
}

export const siteProbeQuery = (base: string) =>
  queryOptions({
    queryKey: ["site-probe", base],
    queryFn: () => probeSite(base),
    staleTime: 30_000,
    retry: false,
    // ok 的台也照樣每 30 秒問一次：那台中途整台不在時，串流只會在 transport 裡默默重試，
    // 要靠這個真的請求失敗才知道它 down（S3-8；不用串流閒置去猜）。主伺服器不經這裡。
    refetchInterval: PROBE_RETRY_MS,
  });

/**
 * 一台能不能用。版本比較只有一個正本：主伺服器自己 get_mode 的 board_api。
 * 主伺服器還沒讀到前別台一律 checking（不知道自己是誰，不能判斷太舊、正式／開發、是不是寫了自己）。
 * 正式看板只看正式家、開發看板只看開發家（計畫設計決定 3、法律第 1.1 條的例外（開發與日常分開））：env 不同當被拒。
 */
export function siteState(probe: Probe | undefined, primary: SiteMode | undefined): SiteState {
  if (!probe || !primary) return "checking";
  if (probe.reach !== "ok") return probe.reach;
  if (probe.mode.env !== primary.env) return "denied";
  if (probe.mode.boardApi < primary.boardApi) return "old";
  return "ok";
}

/** 這台能不能用來讀寫（主伺服器永遠可以；別台只有 ok）。合併的畫面、設定頁、名冊都照這個分。 */
export function siteUsable(site: Pick<Site, "primary" | "state">): boolean {
  return site.primary || site.state === "ok";
}

/** 被拒的原因：伺服器給的訊息優先；正式／開發不同時看板自己說。 */
export function siteReason(probe: Probe | undefined, primary: SiteMode | undefined): string {
  if (!probe || !primary) return "";
  if (probe.reach !== "ok") return probe.reason ?? "";
  if (probe.mode.env !== primary.env)
    return primary.env === "prod" ? "這是開發家，正式看板不看" : "這是正式家，開發看板不看";
  return "";
}

const noMode: SiteMode = {
  env: "prod",
  label: "",
  machine: "",
  userName: "",
  version: "",
  boardApi: 0,
  publicUrl: "",
};

/**
 * 組出看板的台清單：主伺服器永遠在第一個（還沒讀到 get_mode 也在，state 先當 ok）；
 * 別台照設定順序；讀到的台名跟主伺服器或前面的台重複（設定裡寫了自己、寫了兩次）就略過。
 * 別台的 key＝台名（連不上時用這個分頁上次讀到的台名）；從沒連上過（不知道台名）時 key＝它的網址，不會撞、不發查詢。
 */
export function buildSites(
  primaryMode: SiteMode | undefined,
  primaryFailed: boolean,
  configs: readonly SiteConfig[],
  probes: readonly (Probe | undefined)[],
): Site[] {
  const pm = primaryMode ?? noMode;
  const primary: Site = {
    ...PRIMARY,
    primary: true,
    label: pm.label,
    machine: pm.machine,
    env: primaryMode ? pm.env : "",
    version: pm.version,
    boardApi: pm.boardApi,
    publicUrl: pm.publicUrl,
    color: "",
    state: primaryFailed && !primaryMode ? "down" : "ok",
    reason: "",
  };
  const out: Site[] = [primary];
  const seen = new Set<string>([pm.label].filter(Boolean));
  const seenBase = new Set<string>();
  configs.forEach((c, i) => {
    if (!c.url || seenBase.has(c.url)) return;
    seenBase.add(c.url);
    const probe = probes[i];
    const mode = probe?.reach === "ok" ? probe.mode : probe?.last;
    if (mode?.label && seen.has(mode.label)) return;
    if (mode?.label) seen.add(mode.label);
    out.push({
      key: mode?.label || c.url,
      base: c.url,
      primary: false,
      label: mode?.label ?? "",
      machine: mode?.machine ?? "",
      env: mode?.env ?? "",
      version: mode?.version ?? "",
      boardApi: mode?.boardApi ?? 0,
      publicUrl: mode?.publicUrl ?? "",
      color: c.color ?? "",
      state: siteState(probe, primaryMode),
      reason: siteReason(probe, primaryMode),
    });
  });
  return out;
}

const NO_CONFIGS: readonly SiteConfig[] = [];

/** useQueries 的 combine：放在模組層（參考不變），結果沒變時 TanStack 回同一個陣列，useMemo 才不會每次重算。 */
export function dataOf<T>(results: readonly { data: T | undefined }[]): (T | undefined)[] {
  return results.map((r) => r.data);
}

/** 看板看的所有台（主伺服器在第一個）。只有一台時就是 `[主伺服器]`，不會多打任何請求以外的東西。 */
export function useSites(): Site[] {
  const home = useQuery(homeQuery());
  const { data: configs } = useQuery(sitesConfigQuery);
  const list = configs ?? NO_CONFIGS;
  const probes = useQueries({
    queries: list.map((c) => siteProbeQuery(c.url)),
    combine: dataOf,
  });
  return useMemo(
    () => buildSites(home.data, home.isError, list, probes),
    [home.data, home.isError, list, probes],
  );
}

/** 用台的 key 找那台；主伺服器是 ""。找不到（還沒載入、設定拿掉了）回 undefined。 */
export function useSite(key: string): Site | undefined {
  const sites = useSites();
  return sites.find((s) => s.key === key);
}

/**
 * 那台的 api（`makeApi(base)`）。主伺服器（""）永遠有；別台只有 state＝ok 才給，其他（找不到、太舊、被拒、
 * 到不了、試連中）回 undefined——呼叫端要自己處理，不可以退回主伺服器（會把東西寫到錯的家）。
 */
export function useSiteApi(key: string): Api | undefined {
  const site = useSite(key);
  if (key === "") return api;
  return site?.state === "ok" ? apiFor(site) : undefined;
}

/** 網址的 `?home=<台名>` → 是哪一台：沒寫、或寫的是主伺服器自己的台名＝主伺服器。 */
export function resolveHome(sites: readonly Site[], home: string | undefined): Site | undefined {
  const primary = sites[0];
  if (!home || (primary && home === primary.label)) return primary;
  return sites.find((s) => !s.primary && s.key === home);
}

/** 網址 `?home=<台名>` 的 search（router.tsx 的聊天與詳情頁都收這個）。 */
export interface HomeSearch {
  home?: string;
}

/** router 的 validateSearch 用：只收非空字串，其他（沒寫、空的、不是字串）＝主伺服器。 */
export function parseHomeSearch(s: Record<string, unknown>): HomeSearch {
  return { home: typeof s.home === "string" && s.home !== "" ? s.home : undefined };
}

/** 詳情頁與聊天的 `?home=`（router.tsx 的 validateSearch 收進來的）；沒寫是 ""。 */
export function useHomeParam(): string {
  return useSearch({
    strict: false,
    select: (s) => (s as { home?: string }).home ?? "",
  });
}

/**
 * 這一頁看的是哪一台（照 `?home=`）。`site` 找不到時看 `pending`：true＝還在試連（畫轉圈），
 * false＝真的沒有這台（畫「找不到」）。找到的台不一定能用，要看 `site.state`。
 */
export function useHomeSite(): { site: Site | undefined; pending: boolean } {
  const sites = useSites();
  const home = useHomeParam();
  const site = resolveHome(sites, home);
  return { site, pending: !site && homePending(sites) };
}

/**
 * 這一頁看的是哪一台的 key（在場、頁內提醒卡、站內提示用，S3-8）：`?home=` 解析得到就是那台的 key
 * （`?home=<主伺服器自己的台名>`＝""），解析不到（還在試連、沒有這台）照原字串，不會被當成主伺服器。
 */
export function useViewedHome(): string {
  const home = useHomeParam();
  return resolveHome(useSites(), home)?.key ?? home;
}

/** 台清單還沒定下來：主伺服器的 get_mode 還沒讀到，或有別台還在試連。 */
export function homePending(sites: readonly Site[]): boolean {
  return sites.some((s) =>
    s.primary ? s.env === "" && s.state !== "down" : s.state === "checking",
  );
}

/** 連到某台的詳情頁要帶的 search：主伺服器不帶（網址跟 S3 之前一樣），別台帶 `home=<台名>`。 */
export function homeSearch(site: Pick<SiteRef, "key">): { home?: string } {
  return site.key === "" ? {} : { home: site.key };
}

/** 重新試連全部別台（設定頁「立刻試連」、加了一台之後）。 */
export function reprobeSites(client: QueryClient): Promise<void> {
  return client.invalidateQueries({ queryKey: ["site-probe"] });
}

/** 重新試連一台（真的請求被那台拒絕時用，例如串流回了讀得到的錯誤；S3-8）。 */
export function reprobeSite(client: QueryClient, base: string): Promise<void> {
  return client.invalidateQueries({ queryKey: ["site-probe", base] });
}

/**
 * 現在能連的台（主伺服器一律算，跟 S3 之前一樣；別台只有 state＝ok）：即時串流、在場回報、推播訂閱用（S3-8）。
 * 只有一台時就是 `[主伺服器]`。
 */
export function useOkSites(): Site[] {
  const sites = useSites();
  return useMemo(() => sites.filter(siteUsable), [sites]);
}
