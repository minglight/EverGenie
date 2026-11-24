import type {
  BackupGetStatusResponse,
  HomeBackupFile,
  HomeRestoreResult,
} from "@evergenie/sdk-web";
import type { TextKey } from "../../i18n/zh-TW";

// 整個家的備份與還原（DEV-153）：畫面的純函式。規則（收什麼、留幾份、能不能還原）都在伺服器（FEN-N3）。

/** 伺服器的「還沒有」是時間零值（0001-01-01）。 */
export function hasTime(iso: string | undefined): iso is string {
  return !!iso && !iso.startsWith("0001-");
}

/** 備份種類的白話。 */
export function kindKey(kind: string | undefined): TextKey {
  switch (kind) {
    case "daily":
      return "settings.backup.kind.daily";
    case "before-restore":
      return "settings.backup.kind.beforeRestore";
    default:
      return "settings.backup.kind.manual";
  }
}

const dayTime = new Intl.DateTimeFormat("zh-TW", {
  year: "numeric",
  month: "long",
  day: "numeric",
  weekday: "short",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

/** 「2026年11月16日 週一 10:30」：預覽與清單要讓人一眼看出是哪一天的備份。 */
export function dayTimeText(iso: string | undefined): string {
  if (!hasTime(iso)) return "";
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? "" : dayTime.format(at);
}

/** 還原清單：有名字、有時間的，新的在前。 */
export function restorable(files: readonly HomeBackupFile[] | undefined): HomeBackupFile[] {
  return (files ?? [])
    .filter((f) => f.name && hasTime(f.created_at))
    .slice()
    .sort((a, b) => b.created_at.localeCompare(a.created_at));
}

/**
 * 還原送出後等服務重開：`before` 是送出前那一次還原結果的時間（沒有就空字串）。
 * 服務回來了、沒有還原在等、而且結果是新的一筆，才算做完，回那筆結果；其他回 null（繼續等）。
 */
export function restoreFinished(
  before: string,
  now: BackupGetStatusResponse | undefined,
): HomeRestoreResult | null {
  if (!now || now.restoring || !now.last_restore) return null;
  return now.last_restore.at !== before ? now.last_restore : null;
}

/** 這台電腦上的備份一共幾份、佔多大。 */
export function totalOf(files: readonly HomeBackupFile[] | undefined): {
  count: number;
  bytes: number;
} {
  const list = files ?? [];
  return { count: list.length, bytes: list.reduce((sum, f) => sum + (f.size ?? 0), 0) };
}

/** 備份佔了多少才提醒（10 GB）。 */
export const BIG_BACKUP_BYTES = 10 * 2 ** 30;

/**
 * 備份佔的空間要不要提醒：一共超過 10 GB，或已經比那顆硬碟剩下的還多（再做幾份就會滿）。
 * `free` 是 0 表示伺服器不知道剩多少，只看總量。
 */
export function usageIsBig(totalBytes: number, free: number | undefined): boolean {
  if (totalBytes <= 0) return false;
  return totalBytes >= BIG_BACKUP_BYTES || (!!free && free > 0 && totalBytes >= free);
}

/** 還原後會不見的捷徑：最多列 `max` 個，其餘只說還有幾個。 */
export function lostLinksShown(
  paths: readonly string[] | undefined,
  max = 8,
): { shown: string[]; more: number } {
  const list = paths ?? [];
  return { shown: list.slice(0, max), more: Math.max(0, list.length - max) };
}

/** 沒收進備份的東西：最多列三個名字，其餘說「等」。 */
export function skippedText(paths: readonly string[]): string {
  const head = paths.slice(0, 3).join("、");
  return paths.length > 3 ? `${head} 等` : head;
}
