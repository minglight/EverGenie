/** 「上一頁」沒有站內歷史可退時要去的地方（通知、書籤、新分頁直接進來）。 */
export interface BackTarget {
  to: string;
  search?: Record<string, unknown>;
}

const LIST_OF_DETAIL = /^\/(chat|tasks|reminders|goals|personas)\/[^/]+$/;

/**
 * 這個畫面的上一層；今天頁是最上層，回 null（不畫上一頁）。
 * 每一層都比現在更靠近今天，所以沒有歷史時一路按上一頁一定會停在今天，不會繞圈（FEN-29）。
 */
export function backTarget(
  pathname: string,
  search: Record<string, unknown>,
  opts: { settingsHome?: boolean } = {},
): BackTarget | null {
  const path = pathname.replace(/\/+$/, "");
  if (path === "") return null;
  if (path === "/tasks/jobs") return { to: search.job ? "/tasks/jobs" : "/tasks" };
  if (path.startsWith("/messages/")) {
    return { to: "/messages", search: { show: search.show, lane: search.lane } };
  }
  // 設定的各頁上一層是設定首頁的清單（手機，DEV-156 U-12）；桌機的 /settings 會轉到第一頁，所以桌機回今天。
  if (path.startsWith("/settings/")) return opts.settingsHome ? { to: "/settings" } : { to: "/" };
  const detail = LIST_OF_DETAIL.exec(path);
  if (detail) return { to: `/${detail[1]}` };
  return { to: "/" };
}
