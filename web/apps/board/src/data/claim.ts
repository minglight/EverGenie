import type { Notification } from "@evergenie/sdk-web";

// 認領職位的通知（SEC-302 S9、REQ-346）：核心（pkg）發的「這個職位還沒有人負責」，`claim` 是職位 id。
// 主人按「好」由核心套用；「之後再說」只記下來；其他（「換一個」或打了別的字）＝他要自己挑，直接開設定頁那張卡。

export const CLAIM_LATER = "之後再說";

/** 這則是不是核心發的認領通知（只有 source 是 package 才帶 claim）。 */
export function claimOf(n: Pick<Notification, "source" | "claim"> | undefined): string {
  return n?.source === "package" && n.claim ? n.claim : "";
}

/** 設定頁那張認領卡的連結。 */
export function claimLink(claim: string) {
  return { to: "/settings/agents" as const, search: { claim } };
}

/** 回答之後要不要開設定頁那張卡：「好…」（核心套用）與「之後再說」不開，其他開。 */
export function claimFollowUp(
  n: Pick<Notification, "source" | "claim"> | undefined,
  text: string,
): ReturnType<typeof claimLink> | null {
  const claim = claimOf(n);
  const answer = text.trim();
  if (!claim || answer.startsWith("好") || answer === CLAIM_LATER) return null;
  return claimLink(claim);
}
