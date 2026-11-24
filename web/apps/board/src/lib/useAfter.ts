import { useEffect, useState } from "react";

/** 掛上去 ms 毫秒之後變 true（「等太久了，先把其他東西畫出來」用，WEB-175）。 */
export function useAfter(ms: number): boolean {
  const [done, setDone] = useState(false);
  useEffect(() => {
    const timer = setTimeout(() => setDone(true), ms);
    return () => clearTimeout(timer);
  }, [ms]);
  return done;
}
