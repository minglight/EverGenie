import { useEffect, useState } from "react";

/** 目前時間，每 intervalMs 更新一次（讓「3 分鐘前」自己往後走）；intervalMs 是 0 就不更新（不需要倒數的畫面不要白白重畫）。 */
export function useNow(intervalMs = 60_000): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    if (!(intervalMs > 0)) return;
    const id = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}
