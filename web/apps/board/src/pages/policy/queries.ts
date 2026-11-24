import type { PolicyRequest, PolicyStatus } from "@evergenie/sdk-web";
import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { apiFor, PRIMARY, type SiteRef, siteKey } from "../../data/sites";

// 權限申請（Q7）：待審／已授權／紀錄三個檢視。
export type PolicyView = "pending" | "applied" | "history";
export const policyViews: PolicyView[] = ["pending", "applied", "history"];
const policyStatuses: Record<PolicyView, PolicyStatus[]> = {
  pending: ["pending"],
  applied: ["applied"],
  history: ["rejected", "failed", "revoked", "expired"],
};

// 每台一份（S3-7b）：key 經 siteKey 帶台名，主伺服器跟 S3 之前一樣是 ["policy", view]。
export const policyQuery = (view: PolicyView, site: SiteRef = PRIMARY) =>
  queryOptions({
    queryKey: siteKey(site, "policy", view),
    queryFn: async (): Promise<PolicyRequest[]> =>
      (await apiFor(site).policy.list({ statuses: policyStatuses[view] })).requests ?? [],
    placeholderData: keepPreviousData,
    staleTime: 10_000,
    // 不輪詢：申請一變 vault.changed 就重抓（REQ-389）。
  });
