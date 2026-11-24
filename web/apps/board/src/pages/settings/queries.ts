import { ApiError, api } from "@evergenie/sdk-web";
import { keepPreviousData, type QueryClient, queryOptions } from "@tanstack/react-query";
import { setKnowledge } from "../../data/knowledge";
import { retryUnlessMissing } from "../../data/queries";
import { apiFor, PRIMARY, SITES_CONFIG_KEY, type SiteRef, siteKey } from "../../data/sites";

// 設定頁（含套件、角色庫、權限現況、機器狀態）專用的查詢；別的頁面也要用的留在 data/queries.ts。

export const trustQuery = queryOptions({
  queryKey: ["trust"],
  queryFn: async () => (await api.web.getTrust({})).policy ?? {},
  staleTime: 15_000,
});

// 同時進行的工作對話上限（settings/sessions.toml 的 work_max，SES-73、WEB-186）：預設值與範圍由伺服器給，畫面不另外寫死。
export const workMaxQuery = queryOptions({
  queryKey: ["work-max"],
  queryFn: () => api.system.getWorkMax({}),
  staleTime: 30_000,
});

// 通知偏好（通知分頁、簡單／完整切換用）。
export const notifyPrefsQuery = queryOptions({
  queryKey: ["notify-prefs"],
  queryFn: async () => (await api.notify.getPrefs({})).prefs ?? null,
  staleTime: 30_000,
});

export const healthQuery = queryOptions({
  queryKey: ["health"],
  queryFn: async () => (await api.web.getHealth({})).items ?? [],
  staleTime: 5_000,
  refetchInterval: 30_000,
});

export const pushKeyQuery = queryOptions({
  queryKey: ["push-key"],
  queryFn: () => api.notify.pushKey({}),
  staleTime: 10_000,
  retry: false,
});

// ---- 設定頁補齊（P6-2b）----

/** 設定頁「秘書與職位」：名冊上全部秘書（含停用）加上可選的帳號。 */
export const agentSettingsQuery = queryOptions({
  queryKey: ["web.get_agents"],
  queryFn: () => api.web.getAgents({}),
  staleTime: 10_000,
});

/** 權限現況：預設等級與每位秘書的等級（system.trust）。 */
export const systemTrustQuery = queryOptions({
  queryKey: ["system.trust"],
  queryFn: () => api.system.trust({}),
  staleTime: 10_000,
});

export const menuPrefsQuery = queryOptions({
  queryKey: ["web.get_menu_prefs"],
  queryFn: async () => (await api.web.getMenuPrefs({})).prefs ?? {},
  staleTime: 10_000,
});

// 從網址裝的套件有沒有新版（REQ-282）；逐一問來源網站，所以不頻繁重抓。
export const packageOutdatedQuery = queryOptions({
  queryKey: ["package.outdated"],
  queryFn: () => api.package.outdated({}),
  staleTime: 60_000,
});

export const packageExtensionsQuery = queryOptions({
  queryKey: ["package.list_extensions"],
  queryFn: () => api.package.listExtensions({}),
  staleTime: 10_000,
});

export const preflightQuery = queryOptions({
  queryKey: ["system.preflight"],
  queryFn: async () => (await api.system.preflight({})).checks ?? [],
  staleTime: 30_000,
});

export const usageQuery = queryOptions({
  queryKey: ["system.usage"],
  queryFn: async () => (await api.system.usage({})).accounts ?? [],
  staleTime: 30_000,
});

export const accountsQuery = queryOptions({
  queryKey: ["system.accounts"],
  queryFn: async () => {
    const r = await api.system.accounts({});
    return { accounts: r.accounts ?? [], engines: r.engines ?? [] };
  },
  staleTime: 10_000,
});

// 夜班清單（P6-6）：vault 的 note system/night-queue；還沒有這個檔案就是 null。
export const nightQueueQuery = queryOptions({
  queryKey: ["night-queue"],
  queryFn: async () => {
    try {
      return (await api.vault.get({ kind: "note", id: "system/night-queue" })).record ?? null;
    } catch (err) {
      if (err instanceof ApiError && err.code === "not_found") return null;
      throw err;
    }
  },
  staleTime: 10_000,
});

export const backupsQuery = queryOptions({
  queryKey: ["system.backups"],
  queryFn: () => api.system.backups({}),
  staleTime: 30_000,
});

// 角色庫（P6-5a、REQ-130）：全部的卡、一張卡（含內文與整份檔案）、說話層強度。寫入後整組 ["personas"] 一起失效。
export const personasQuery = queryOptions({
  queryKey: ["personas", "list"],
  queryFn: async () => (await api.personas.list({})).personas ?? [],
  placeholderData: keepPreviousData,
  staleTime: 10_000,
});

export const personaQuery = (slug: string) =>
  queryOptions({
    queryKey: ["personas", "get", slug],
    queryFn: () => api.personas.get({ slug }),
    staleTime: 0,
    retry: retryUnlessMissing,
  });

export const personaStyleQuery = queryOptions({
  queryKey: ["personas", "style"],
  queryFn: () => api.personas.getStyle({}),
  staleTime: 10_000,
});

// 測口吻的結果（P6-5b）：有一份在跑就每 3 秒再讀一次。
export const personaTestsQuery = (slug: string) =>
  queryOptions({
    queryKey: ["personas", "tests", slug],
    queryFn: async () => (await api.personas.testResults({ slug, limit: 10 })).runs ?? [],
    staleTime: 0,
    refetchInterval: (query) =>
      query.state.data?.some((r) => r.status === "running") ? 3000 : false,
  });

export const invalidatePersonas = (client: QueryClient) =>
  Promise.all([
    client.invalidateQueries({ queryKey: ["personas"] }),
    client.invalidateQueries({ queryKey: ["web.get_agents"] }),
    client.invalidateQueries({ queryKey: ["roster"] }),
  ]);

/**
 * 請一位角色擔任一個職位、開始上班（WEB-47）：套件樣板建好這位秘書（package.sync）再啟用。
 * 「職位與秘書」的認領卡、設定精靈、新家的歡迎頁（WEB-176）都走這一個，不另做一套。
 */
export async function hireSecretary(
  fn: { pkg: string; id: string },
  persona: string,
  /** 主人勾的可選知識（DEV-161）；沒給就用預設。建好紀錄之後、啟用之前存，她第一次開對話就帶對。 */
  knowledge?: string[],
) {
  await api.package.sync({ id: fn.pkg, agent: fn.id, persona, apply: true });
  if (knowledge !== undefined) await setKnowledge(fn.id, knowledge);
  await api.agents.setField({ id: fn.id, field: "enabled", value: "true" });
}

/** 請完秘書之後要重讀的資料：名冊、角色庫、套件清單。 */
export const invalidateHired = (client: QueryClient) =>
  Promise.all(
    [agentSettingsQuery.queryKey, ["roster"], ["personas"], ["package.list"]].map((queryKey) =>
      client.invalidateQueries({ queryKey }),
    ),
  );

/**
 * 新家剛請到第一位秘書（歡迎頁，WEB-176）：快取裡「名冊是空的」已經不對了，直接丟掉再重讀，不留著當舊資料。
 * 丟掉之後名冊是「還沒讀到」：外殼照常畫看板（welcomeGate）、今天頁不轉去設定精靈；就算這次重讀失敗也不會被舊的空名冊彈回歡迎頁。
 * 不等重讀完成。
 */
export function forgetEmptyRoster(client: QueryClient) {
  for (const queryKey of [agentSettingsQuery.queryKey, ["roster"]]) {
    void client.resetQueries({ queryKey });
  }
  for (const queryKey of [["personas"], ["package.list"]]) {
    void client.invalidateQueries({ queryKey });
  }
}

/** 一台的「誰可以連進來」（web.get_sites 的 owner_login、allow_boards）。 */
export interface WebTrust {
  ownerLogin: string;
  allowBoards: string[];
}

// 設定 → 電腦（S3-9）：這台（可以改）或別台（只顯示）的信任設定。key 接在 SITES_CONFIG_KEY 後面，
// 主伺服器改了台清單或信任設定 invalidate SITES_CONFIG_KEY 時這份一起重抓。
export const webTrustQuery = (site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, ...SITES_CONFIG_KEY, "trust"),
    queryFn: async (): Promise<WebTrust> => {
      const r = await apiFor(site).web.getSites({});
      return { ownerLogin: r.owner_login ?? "", allowBoards: r.allow_boards ?? [] };
    },
    staleTime: 30_000,
    retry: false,
  });
