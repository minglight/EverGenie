import { useCallback, useMemo } from "react";
import { t } from "../i18n/zh-TW";
import { agentKey, primaryOf, splitAgentKey, useSiteAgents } from "./agents";
import { createStore, useStore } from "./store";

// 秘書篩選（REQ-155）：今天、聊天清單、待辦、任務單、訊息五頁共用同一個選擇，一次只選一位（2026-11-02 起）。
// 值是秘書的 agentKey 陣列（最多一個）（主伺服器＝職位 id，別台＝「台名/職位 id」，兩台同名秘書不撞），空陣列＝全部。
// 存 localStorage（方便類的資料，FEN-5 允許），不放網址。

export const AGENT_FILTER_KEY = "eg.agentFilter.v2";
/** S3 之前的格式：職位 id 陣列（只有主伺服器）。讀到就轉成 v2（主伺服器的 key 就是職位 id），寫回 v2 後刪掉。 */
export const AGENT_FILTER_KEY_V1 = "eg.agentFilter.v1";

/** 讀回來的字串 → key 陣列；壞掉、不是陣列、讀不到都當「全部」。 */
export function parseStored(raw: string | null): string[] {
  if (!raw) return [];
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value)) return [];
    return [...new Set(value.filter((x): x is string => typeof x === "string" && x !== ""))];
  } catch {
    return [];
  }
}

/**
 * 照台、再照名冊順序排好，丟掉名冊裡沒有的 key。`rosters` 是每台（台的 key → 那台名冊的 agentKey，照順序）；
 * 那台的名冊還沒載入（不在 rosters 裡，或是空的）時，屬於它的 key 原樣保留、排在最後——別台暫時連不上不會把篩選弄丟。
 */
export function normalizePicked(
  keys: readonly string[],
  rosters: ReadonlyMap<string, readonly string[]>,
): string[] {
  const unique = [...new Set(keys)];
  const known: string[] = [];
  for (const roster of rosters.values()) {
    for (const k of roster) if (unique.includes(k)) known.push(k);
  }
  const pending = unique.filter((k) => {
    const roster = rosters.get(splitAgentKey(k).site);
    return !roster || roster.length === 0;
  });
  return [...known, ...pending];
}

/**
 * `picked` 空＝全部都算；有選時，沒有秘書（空）或不在選擇裡的資料不算。
 * `agentId` 是 `site` 那台的職位 id（預設主伺服器）；同一位角色的幾個職位算在同一張卡。
 */
export function matchAgent(picked: readonly string[], agentId?: string, site = ""): boolean {
  if (picked.length === 0) return true;
  return (
    agentId !== undefined &&
    agentId !== "" &&
    picked.includes(agentKey(site, primaryOf(agentId, site)))
  );
}

/** 每位秘書在這份資料裡有幾筆，key 是 agentKey（沒有秘書的資料不算；同一位角色的幾個職位算在同一張卡）。 */
export function countByAgent<T>(
  items: readonly T[],
  agentOf: (item: T) => string | undefined,
  site = "",
): Record<string, number> {
  const out: Record<string, number> = {};
  for (const item of items) {
    const raw = agentOf(item);
    if (!raw) continue;
    const key = agentKey(site, primaryOf(raw, site));
    out[key] = (out[key] ?? 0) + 1;
  }
  return out;
}

function readStored(): string[] {
  try {
    const v2 = localStorage.getItem(AGENT_FILTER_KEY);
    if (v2 !== null) return parseStored(v2);
    const v1 = localStorage.getItem(AGENT_FILTER_KEY_V1);
    if (v1 === null) return [];
    // v1 的職位 id 都是主伺服器的，主伺服器的 agentKey 就是職位 id：照抄、寫成 v2、刪掉 v1。
    const migrated = parseStored(v1);
    localStorage.setItem(AGENT_FILTER_KEY, JSON.stringify(migrated));
    localStorage.removeItem(AGENT_FILTER_KEY_V1);
    return migrated;
  } catch {
    return [];
  }
}

function writeStored(keys: readonly string[]) {
  try {
    localStorage.setItem(AGENT_FILTER_KEY, JSON.stringify(keys));
  } catch {
    // 寫不進去只是少了「記住」，這次的選擇還是照樣生效。
  }
}

const sameKeys = (a: readonly string[], b: readonly string[]) =>
  a.length === b.length && a.every((k, i) => k === b[i]);

export const agentFilterStore = createStore<string[]>(readStored());

/**
 * 這個分頁的使用者改了幾次篩選（toggle／only／clear 各加一；別的分頁改的經 storage 事件進來不算）。
 * 聊天「篩選換人→對話跟著換」只看它，免得兩個分頁互相把對方的對話拉走（WEB-19）。
 */
export const agentFilterLocalPicks = createStore<number>(0);

/** 從 localStorage 重新讀一次（其他分頁改了；測試也用它）。 */
export function reloadAgentFilter() {
  const next = readStored();
  if (!sameKeys(next, agentFilterStore.get())) agentFilterStore.set(next);
}

/**
 * 設定頁拿掉一台時，篩選裡那台的秘書一起清掉（S3-9）。平常那台連不上時不清（normalizePicked 會保留，免得暫時斷線弄丟選擇），
 * 只有真的從台清單拿掉才清。主伺服器（""）不清。
 */
export function forgetSiteInAgentFilter(site: string) {
  if (site === "") return;
  const cur = agentFilterStore.get();
  const next = cur.filter((k) => splitAgentKey(k).site !== site);
  if (next.length === cur.length) return;
  agentFilterStore.set(next);
  writeStored(next);
}

if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key === AGENT_FILTER_KEY || e.key === null) reloadAgentFilter();
  });
}

export interface AgentFilter {
  /**
   * 選中那一位在每台的 agentKey（照台、照名冊順序）；空陣列＝全部。兩台都有同一個職位時是兩個 key（篩選合成一個頭像，
   * 作者 2026-11-02），matchAgent／件數照舊用它。只有一台時就是職位 id。
   */
  picked: string[];
  /** 選中的那一位（主伺服器那台優先的 agentKey）；沒選是 ""。要「一位」的地方（待辦負責人、她的首頁）用它。 */
  one: string;
  /** 這張頭像（任何一台的 agentKey）是不是選中的那位。 */
  isOn: (key: string) => boolean;
  toggle: (key: string) => void;
  /** 只看這一位（今天的秘書卡片：點了進她的首頁）。 */
  only: (key: string) => void;
  clear: () => void;
}

/** 每台啟用中的秘書（看板上的卡，合併過）→ agentKey，照名冊順序。 */
function useRosterKeys(): Map<string, string[]> {
  const sites = useSiteAgents();
  return useMemo(
    () =>
      new Map(
        sites.map(({ site, list }) => [
          site.key,
          list.filter((a) => a.enabled && a.id).map((a) => agentKey(site.key, a.id ?? "")),
        ]),
      ),
    [sites],
  );
}

/**
 * 同一個職位在每台的 key（篩選一個頭像＝兩台的她）：照台、照名冊順序；`key` 自己那台的名冊還沒到時原樣保留。
 * 空字串回空陣列。
 */
export function sameFeatureKeys(
  key: string,
  rosters: ReadonlyMap<string, readonly string[]>,
): string[] {
  if (key === "") return [];
  const { id } = splitAgentKey(key);
  const out: string[] = [];
  for (const roster of rosters.values()) {
    for (const k of roster) if (splitAgentKey(k).id === id) out.push(k);
  }
  if (!out.includes(key)) {
    const roster = rosters.get(splitAgentKey(key).site);
    if (!roster || roster.length === 0) out.push(key);
  }
  return out;
}

/**
 * 存的 key → 選中那一位在每台的 key。先照名冊順序挑第一位；她存的那台沒有她了（停用、改名）但別台還有同一個職位時，
 * 照樣是別台的她（不會悄悄回到全部、頭像卻還在）。
 */
export function pickedOf(
  stored: readonly string[],
  rosters: ReadonlyMap<string, readonly string[]>,
): string[] {
  for (const key of [...normalizePicked(stored, rosters), ...stored]) {
    const keys = sameFeatureKeys(key, rosters);
    if (keys.length > 0) return keys;
  }
  return [];
}

export function useAgentFilter(): AgentFilter {
  const rosters = useRosterKeys();
  const stored = useStore(agentFilterStore);
  // 一次只看一位（作者 2026-11-02「不要多選，不好切人」）：以前存的多選只留第一位；兩台同一個職位算同一位。
  const picked = useMemo(() => pickedOf(stored, rosters), [stored, rosters]);
  // 主伺服器那台優先（別台的名冊先到時 picked 的順序會先是別台，不能拿第一個）。
  const one = picked.find((k) => splitAgentKey(k).site === "") ?? picked[0] ?? "";

  const commit = useCallback((next: string[]) => {
    agentFilterLocalPicks.set(agentFilterLocalPicks.get() + 1);
    agentFilterStore.set(next);
    writeStored(next);
  }, []);
  // 點別人＝換成只看她；點已選的那位（哪一台的頭像都算）＝回到全部。
  const toggle = useCallback(
    (key: string) => {
      const cur = pickedOf(agentFilterStore.get(), rosters);
      commit(cur.includes(key) ? [] : [key]);
    },
    [rosters, commit],
  );
  const only = useCallback((key: string) => commit([key]), [commit]);
  const clear = useCallback(() => commit([]), [commit]);
  const isOn = useCallback((key: string) => picked.includes(key), [picked]);
  return { picked, one, isOn, toggle, only, clear };
}

/** 選中的人名，頓號隔開（「只看：…」與空狀態的句子用）。別台的人名也照那台的名冊；兩台同一位只寫一次。 */
export function usePickedNames(picked: readonly string[]): string {
  const sites = useSiteAgents();
  const names = picked.map((key) => {
    const { site, id } = splitAgentKey(key);
    const found = sites.find((s) => s.site.key === site);
    return found ? found.view(id).name : id;
  });
  return [...new Set(names)].join(t("filter.names.sep"));
}
