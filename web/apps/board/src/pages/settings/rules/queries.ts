import {
  api,
  type RulesDoc,
  type RulesGetResponse,
  type RulesListResponse,
  type RulesProposal,
  type RulesVersion,
  type RulesVersionResponse,
} from "@evergenie/sdk-web";
import { keepPreviousData, queryOptions } from "@tanstack/react-query";

// 規矩頁（S7、D-44）的資料。不輪詢：版本、提案一變 vault.changed 就重抓（data/live.ts 的 rules_version、rules_proposal → ["rules"]）；
// 在電腦上直接改檔不會有事件，進頁與回前景時照常重抓，存檔時版本不對會回衝突。

export const rulesListQuery = () =>
  queryOptions({
    queryKey: ["rules", "list"],
    queryFn: (): Promise<RulesListResponse> => api.rules.list({}),
    staleTime: 10_000,
  });

export const rulesDocQuery = (doc: string) =>
  queryOptions({
    queryKey: ["rules", "doc", doc],
    queryFn: (): Promise<RulesGetResponse> => api.rules.get({ doc }),
    staleTime: 10_000,
  });

export const rulesHistoryQuery = (doc: string) =>
  queryOptions({
    queryKey: ["rules", "history", doc],
    queryFn: async (): Promise<RulesVersion[]> => (await api.rules.history({ doc })).versions ?? [],
    staleTime: 10_000,
  });

export const rulesVersionQuery = (id: string, against: "" | "current") =>
  queryOptions({
    queryKey: ["rules", "version", id, against],
    queryFn: (): Promise<RulesVersionResponse> => api.rules.version({ id, against }),
    placeholderData: keepPreviousData,
    staleTime: 10_000,
  });

export const rulesPendingQuery = () =>
  queryOptions({
    queryKey: ["rules", "proposals", "pending"],
    queryFn: async (): Promise<RulesProposal[]> =>
      (await api.rules.proposals({ statuses: ["pending"] })).proposals ?? [],
    staleTime: 10_000,
  });

export type { RulesDoc };
