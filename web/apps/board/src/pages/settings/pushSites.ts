import { ApiError } from "@evergenie/sdk-web";
import { apiFor, type Site } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import type { SubscriptionKeys } from "./push";

// 一支裝置收所有台的推播（S3-8、計畫設計決定 7）：瀏覽器只有一個訂閱（用這個看板＝主伺服器的公開金鑰訂），
// 同一個訂閱寫進每一台（各台自己的 settings/push.toml）。訂閱只認一把金鑰，所以先比對那台的 push_key：
// 不一樣的台送來的推播，推播服務會拒收——那台標「金鑰不一樣、收不到」，不寫進去。

/**
 * 別台的推播：on＝寫進去了、會收到；mismatch＝那台的推播金鑰跟這個看板不一樣，收不到；
 * missing＝那台沒設推播金鑰；unreachable＝那台現在不能用（連不上、被拒、太舊）或讀不到金鑰；failed＝那台沒記下來。
 */
export type SitePushState = "on" | "mismatch" | "missing" | "unreachable" | "failed";

export interface SitePush {
  state: SitePushState;
  /** 伺服器的白話原因（failed、unreachable 時有）。 */
  reason?: string;
}

const reasonOf = (err: unknown) => (err instanceof ApiError ? err.message : "");

/**
 * 把這台裝置的訂閱寫進一台別台：先看那台能不能用、比對 push_key（照原字串比），一樣才 subscribe_push。
 * 主伺服器不在這裡（DevicesSettings 本來就處理，失敗要整個退掉）。
 */
export async function subscribeSite(
  site: Site,
  keys: SubscriptionKeys,
  primaryKey: string,
): Promise<SitePush> {
  if (site.state !== "ok") return { state: "unreachable", reason: site.reason };
  const a = apiFor(site);
  let theirs: string;
  try {
    theirs = (await a.notify.pushKey({})).public_key ?? "";
  } catch (err) {
    // 那台沒設金鑰：notify.push_key 回「推播金鑰還沒設定」（跟 DevicesSettings 認主伺服器的同一句）。
    if (err instanceof ApiError && err.message === t("settings.devices.keyMissing.server"))
      return { state: "missing" };
    return { state: "unreachable", reason: reasonOf(err) };
  }
  if (theirs === "") return { state: "missing" };
  if (theirs !== primaryKey) return { state: "mismatch" };
  try {
    await a.notify.subscribePush(keys);
    return { state: "on" };
  } catch (err) {
    return { state: "failed", reason: reasonOf(err) };
  }
}

/** 每台別台都寫一次（彼此不等、互不影響）；回傳台的 key → 結果。 */
export async function subscribeOtherSites(
  sites: readonly Site[],
  keys: SubscriptionKeys,
  primaryKey: string,
): Promise<Record<string, SitePush>> {
  const others = sites.filter((s) => !s.primary);
  const results = await Promise.all(others.map((s) => subscribeSite(s, keys, primaryKey)));
  return Object.fromEntries(others.map((s, i) => [s.key, results[i] as SitePush]));
}

/** 關閉推播：每台能用的別台都把這個 endpoint 刪掉（盡力而為；那台現在連不上就留著，瀏覽器已經退訂，推播服務會拒收）。 */
export async function unsubscribeOtherSites(
  sites: readonly Site[],
  endpoint: string,
): Promise<void> {
  await Promise.all(
    sites
      .filter((s) => !s.primary && s.state === "ok")
      .map((s) =>
        apiFor(s)
          .notify.unsubscribePush({ endpoint })
          .catch(() => {}),
      ),
  );
}
