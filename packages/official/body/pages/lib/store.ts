// 身體套件讀寫資料的地方：全部走 @evergenie/sdk-web 的 api.vault（D-28，沒有套件自己的 RPC）。
import { api, onLiveChange } from "@evergenie/sdk-web";
import { useCallback, useEffect, useState } from "react";
import { type BodyEntry, type RecordLike, sortBody, toBody } from "./body";

export const BODY = "body.entry";

/** 身體數據：沒刪掉的（日期新→舊）。 */
export async function loadBody(): Promise<BodyEntry[]> {
  const r = await api.vault.list({ kind: BODY, include_archived: false });
  return sortBody((r.records ?? []).map((x: RecordLike) => toBody(x)));
}

/** 刪除＝封存（檔案還在）。 */
export async function archiveBody(id: string): Promise<void> {
  await api.vault.archive({ kind: BODY, id });
}

/**
 * 記某一天的身體數據：同一天已經有記錄就只改有給的欄位（覆蓋），沒有就新增（編號＝日期）。
 * 那天的記錄之前被刪掉（封存）也照樣新增一筆，舊的留在封存裡不動（vault VLT-15，DEV-151）。
 * 新增時被說「這個編號已經有資料了」＝畫面還沒跟上（例如秘書剛幫忙記了同一天）：照「同一天再記就是覆蓋」改成修改。
 */
export async function saveBody(
  date: string,
  fields: Record<string, number | string>,
  existing: boolean,
): Promise<void> {
  if (existing) {
    await api.vault.patch({ kind: BODY, id: date, set: fields });
    return;
  }
  try {
    await api.vault.put({
      record: { kind: BODY, id: date, fields: { date, ...fields }, archived: false },
    });
  } catch (e) {
    if (e instanceof Error && (e as { code?: string }).code === "conflict") {
      await api.vault.patch({ kind: BODY, id: date, set: fields });
      return;
    }
    throw e;
  }
}

export function reasonOf(e: unknown): string {
  return e instanceof Error && e.message ? e.message : "連不上看板服務，請稍後再試";
}

export interface Loaded<T> {
  data: T | undefined;
  error: string;
  reload: () => void;
}

/** 載一次、之後可以 reload；錯誤變成白話字串。 */
export function useLoad<T>(fn: () => Promise<T>): Loaded<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState("");
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    fn()
      .then((d) => {
        if (!live) return;
        setError("");
        setData(d);
      })
      .catch((e: unknown) => {
        if (!live) return;
        console.warn("讀取失敗", e);
        setError(reasonOf(e));
      });
    return () => {
      live = false;
    };
  }, [tick]);
  // 重讀時先清掉錯誤：畫面回到「載入中」，再失敗才又顯示，不會按了「再試一次」像沒反應。
  const reload = useCallback(() => {
    setError("");
    setTick((n) => n + 1);
  }, []);
  // 秘書聽到體重幫你記了：自己重讀，不用重新整理（PKG-49）。
  // 部署前就開著的舊看板外殼沒有 onLiveChange（共用模組是外殼給的）：沒有就不接，頁面照常能用（PKG-49）。
  useEffect(
    () => (typeof onLiveChange === "function" ? onLiveChange("body.", reload) : undefined),
    [reload],
  );
  return { data, error, reload };
}
