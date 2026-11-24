// 運動套件頁面共用的小零件：載入用的 hook。
import { onLiveChange } from "@evergenie/sdk-web";
import { useCallback, useEffect, useState } from "react";
import { reasonOf } from "./store";

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
  // 教練在旁邊的聊天裡補了檢討、改了器材：自己重讀，不用重新整理（PKG-49）。
  // 部署前就開著的舊看板外殼沒有 onLiveChange（共用模組是外殼給的）：沒有就不接，頁面照常能用（PKG-49）。
  useEffect(
    () => (typeof onLiveChange === "function" ? onLiveChange("fitness.", reload) : undefined),
    [reload],
  );
  return { data, error, reload };
}
