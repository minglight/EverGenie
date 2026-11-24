import { type RefObject, type UIEvent, useCallback, useRef, useState } from "react";

// 手機的秘書篩選列（頭像一排，約 90px）往下捲時收起來、往上捲或回到頂端時再出現（2026-11-02 稽核：標題＋頭像固定在上面，
// 手機一個畫面少了快兩成）。標題列不動。換頁時一律先出現。

/** 往下或往上捲超過這麼多才換，手指微微抖動不會一直閃。 */
const STEP = 8;
/** 離頂端這麼近一律出現。 */
const TOP = 24;

/** 下一個狀態：`hidden` 是現在收起來了沒，`lastY`／`y` 是上一次與這一次的捲動位置。 */
export function nextHidden(hidden: boolean, lastY: number, y: number): boolean {
  if (y <= TOP) return false;
  if (y > lastY + STEP) return true;
  if (y < lastY - STEP) return false;
  return hidden;
}

/** 收起來之後，目前的位置離新的捲動底部至少要留這麼多（16px：手指慣性在 200ms 收合過渡裡還會再多滑一點）。 */
const MARGIN = 2 * STEP;
/** 還沒量到篩選列高度時先用的估計值（約 90px）。 */
const BAR_GUESS = 90;

/**
 * 現在收起來，之後 main 變高（高度多 `barH`）、捲動範圍縮成 `range - barH`：目前位置 `y` 要還在範圍內並留一點空，
 * 不然瀏覽器會把 scrollTop 往回夾，程式當成「往上捲」又展開，展開範圍又變大，一直跳（DEV-111：任務單只有 4 張時）。
 */
export function canCollapse(range: number, scrollTop: number, barH: number): boolean {
  return range - barH >= scrollTop + MARGIN;
}

/** 把 scrollTop 夾進 [0, range]：iOS 頂端／底部回彈時會超出，回彈回來不能算成往上捲。 */
export function clampY(y: number, range: number): number {
  return Math.min(Math.max(y, 0), Math.max(range, 0));
}

export function useHideOnScroll(resetKey: string, barRef?: RefObject<HTMLElement | null>) {
  const [hidden, setHidden] = useState(false);
  const hiddenRef = useRef(false);
  const lastY = useRef(0);
  // 篩選列展開時的高度：量內層的 scrollHeight（overflow-hidden，不受收合過渡影響）；沒量到就沿用上一次（起始用估計值）。
  const barH = useRef(BAR_GUESS);
  // 換頁（resetKey 變了）一律先出現；在 render 時比對，不用多一個 effect。
  const [key, setKey] = useState(resetKey);
  if (key !== resetKey) {
    setKey(resetKey);
    setHidden(false);
    hiddenRef.current = false;
    lastY.current = 0;
  }
  const onScroll = useCallback(
    (e: UIEvent<HTMLElement>) => {
      const el = e.currentTarget;
      const range = el.scrollHeight - el.clientHeight;
      const y = clampY(el.scrollTop, range);
      if (!hiddenRef.current) {
        const h = barRef?.current?.scrollHeight;
        if (h) barH.current = h;
      }
      // 收起後範圍縮短，貼底的 scrollTop 會被瀏覽器夾回 range（慣性快甩時）：那不是使用者往上捲，維持收起。
      if (hiddenRef.current && y >= range - 1 && y < lastY.current) {
        lastY.current = y;
        return;
      }
      let next = nextHidden(hiddenRef.current, lastY.current, y);
      // 只擋「要收起來」：收了之後範圍不夠就維持展開（已經收起來的不用重判，展開不會讓範圍變小；前提是頁面內容高度不跟著 main 高度縮放）。
      if (next && !hiddenRef.current && !canCollapse(range, y, barH.current)) next = false;
      // 只有真的動夠多才更新基準，慢慢捲也算得到方向。
      if (next !== hiddenRef.current || Math.abs(y - lastY.current) > STEP) lastY.current = y;
      if (next !== hiddenRef.current) {
        hiddenRef.current = next;
        setHidden(next);
      }
    },
    [barRef],
  );
  return { hidden, onScroll };
}
