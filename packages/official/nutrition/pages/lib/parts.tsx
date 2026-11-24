// 三個頁面共用的小零件：載入用的 hook、目標進度條。
import { onLiveChange } from "@evergenie/sdk-web";
import { useCallback, useEffect, useState } from "react";
import { fmt, progress } from "./nutrition";
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
  // 秘書在旁邊的聊天裡記了一餐、另一個分頁改了目標：自己重讀，不用重新整理（PKG-49）。
  // 部署前就開著的舊看板外殼沒有 onLiveChange（共用模組是外殼給的）：沒有就不接，頁面照常能用（PKG-49）。
  useEffect(
    () => (typeof onLiveChange === "function" ? onLiveChange("nutrition.", reload) : undefined),
    [reload],
  );
  return { data, error, reload };
}

/** 熱量／蛋白質對目標的進度條；超過目標用警示色，不罵人。target 沒設（<= 0）就只顯示數字。 */
export function GoalBar({
  label,
  value,
  target,
  unit,
  testId,
}: {
  label: string;
  value: number;
  target: number;
  unit: string;
  testId?: string;
}) {
  const p = progress(value, target);
  const pct = p ? Math.min(100, Math.round(p.ratio * 100)) : 0;
  return (
    <div
      data-testid={testId}
      data-over={p?.over ? "true" : "false"}
      className="flex flex-col gap-1"
    >
      <div className="flex items-baseline justify-between gap-2 text-sm">
        <span className="font-medium">{label}</span>
        <span className="text-muted">
          <span className="font-semibold text-fg">{fmt(value)}</span>
          {p ? ` / ${fmt(target)} ${unit}` : ` ${unit}`}
        </span>
      </div>
      {p ? (
        <>
          <div
            role="progressbar"
            aria-label={label}
            aria-valuemin={0}
            aria-valuemax={target}
            aria-valuenow={Math.round(value)}
            className="h-2.5 w-full overflow-hidden rounded-full bg-surface-2"
          >
            <div
              className={`h-full rounded-full ${p.over ? "bg-attention" : "bg-accent"}`}
              style={{ width: `${pct}%` }}
            />
          </div>
          <div className="text-xs text-muted">
            {p.over ? `比目標多 ${fmt(-p.left)} ${unit}` : `還有 ${fmt(p.left)} ${unit}的空間`}
          </div>
        </>
      ) : null}
    </div>
  );
}
