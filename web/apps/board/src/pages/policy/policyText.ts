import {
  type ActionClass,
  ApiError,
  type PolicyRequest,
  type PolicyStatus,
} from "@evergenie/sdk-web";
import type { PillTone } from "../../components/Pill";
import { type TextKey, t } from "../../i18n/zh-TW";

export const statusKey = {
  pending: "policy.status.pending",
  applied: "policy.status.applied",
  rejected: "policy.status.rejected",
  failed: "policy.status.failed",
  revoked: "policy.status.revoked",
  expired: "policy.status.expired",
} as const satisfies Record<PolicyStatus, TextKey>;

export const statusTone: Record<PolicyStatus, PillTone> = {
  pending: "attention",
  applied: "ok",
  rejected: "muted",
  failed: "danger",
  revoked: "muted",
  expired: "muted",
};

const actionKey: Partial<Record<ActionClass, TextKey>> = {
  read: "policy.action.read",
  write: "policy.action.write",
  commit: "policy.action.commit",
  push: "policy.action.push",
  delete: "policy.action.delete",
};

export function actionText(a: ActionClass | undefined): TextKey {
  return (a && actionKey[a]) || "policy.action.other";
}

/** 改後核准的範圍只能改窄：新範圍要是原範圍本身或底下的資料夾（原範圍空＝任何地方）。 */
export function isNarrower(next: string, original: string | undefined): boolean {
  const n = next.trim().replace(/\/+$/, "");
  if (n === "") return true;
  const o = (original ?? "").replace(/\/+$/, "");
  if (o === "") return true;
  return n === o || n.startsWith(`${o}/`);
}

/**
 * 核准／拒絕之後那張卡會從「待審」消失，用一句 toast 說結果；套用失敗要講明（POL-14）。
 * 伺服器沒回狀態（舊版）回 null，不提示。
 */
export function decidedToastKey(q: PolicyRequest | undefined): TextKey | null {
  switch (q?.status) {
    case "applied":
      return "policy.toast.applied";
    case "rejected":
      return "policy.toast.rejected";
    case "failed":
      return "policy.toast.failed";
    case "revoked":
      return "policy.toast.revoked";
    default:
      return null;
  }
}

/**
 * 送出失敗的 toast（POL-15）：失敗後清單會重抓，那張要是已經在別處決定或撤銷了，卡片整張離開，
 * 寫在卡片裡的紅字會跟著不見，所以原因放 toast，留 10 秒夠讀完。
 */
export function failedToast(err: unknown): { text: string; durationMs: number } {
  return {
    text: t("answer.failed", { reason: err instanceof ApiError ? err.message : String(err) }),
    durationMs: 10000,
  };
}

/** 新的在前（伺服器已排好；這裡保險再排一次）。 */
export function newestFirst(list: PolicyRequest[]): PolicyRequest[] {
  return [...list].sort((a, b) => (b.created_at ?? "").localeCompare(a.created_at ?? ""));
}
