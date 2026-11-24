import { onStreamsChange, streamsDown } from "@evergenie/sdk-web";
import { useEffect, useState, useSyncExternalStore } from "react";

/**
 * 串流連不上要持續多久才說（DEV-156 U-21）：畫面從背景回來、伺服器關掉串流時會斷一下又馬上接回來（1 秒內），
 * 那種不算；比這個久才是真的連不上。
 */
export const OFFLINE_AFTER_MS = 2_500;

/**
 * 看板服務現在是不是連不上（頁首那條「現在連不上，顯示的是剛才的內容」）。只看即時更新的串流
 * （transport 本來就在自己重連），不另外輪詢；連回來馬上變 false。
 */
export function useOffline(): boolean {
  const down = useSyncExternalStore(onStreamsChange, streamsDown, () => false);
  const [shown, setShown] = useState(false);
  useEffect(() => {
    if (!down) {
      setShown(false);
      return;
    }
    const timer = setTimeout(() => setShown(true), OFFLINE_AFTER_MS);
    return () => clearTimeout(timer);
  }, [down]);
  return down && shown;
}
