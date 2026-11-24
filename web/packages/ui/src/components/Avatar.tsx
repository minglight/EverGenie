import { type CSSProperties, useEffect, useState } from "react";
import { cn } from "../lib/cn";

export interface AvatarProps {
  name: string;
  /** 伺服器給的秘書顏色（亮／深）；沒有就用 --muted。 */
  color?: string;
  colorDark?: string;
  /** 圖片網址；沒有或載入失敗（404）時退回名字的圓（見 initialsOf）。 */
  src?: string;
  /** 直徑（px）。 */
  size?: number;
  /** 工作中：外圈光圈動畫。 */
  pulse?: boolean;
  /** 等你：右上角黃點。 */
  waiting?: boolean;
  className?: string;
}

// 載入成功過的圖片網址：元件重新掛上（切分頁、清單重畫）時直接顯示，不再從透明淡入，頭像才不會每次都閃一下（REQ-155）。
const loadedSrc = new Set<string>();

// 載入失敗過的圖片網址與時間：這段時間內重新掛上不再抓（清單重畫、切頁不會一直打 404 洗主控台，DEV-157）；
// 過了就再試一次（角色包下載好、換了衣服之後圖會出現）。伺服器沒有圖時本來就不給網址，這裡只是保險。
const failedSrc = new Map<string, number>();
const RETRY_MS = 5 * 60 * 1000;

function failedRecently(src: string): boolean {
  const at = failedSrc.get(src);
  if (at === undefined) return false;
  if (Date.now() - at < RETRY_MS) return true;
  failedSrc.delete(src);
  return false;
}

// 小於這個直徑（px）只放一個字：兩個字會小到看不清；這麼小的頭像旁邊一定有名字。
const TWO_CHAR_MIN = 32;

/**
 * 沒有圖時圓裡的字（DEV-157）：中文（含日韓）名取前兩個字（「阿基里斯」→「阿基」），同一個字開頭的角色也分得出來；
 * 英文名有兩個字以上取各字首字母（Mary Jane → MJ），只有一個字取第一個字母（大寫）。size 小於 32 一律一個字。
 */
export function initialsOf(name: string, size = 56): string {
  const trimmed = name.trim();
  const chars = Array.from(trimmed);
  if (chars.length === 0) return "?";
  const first = (chars[0] ?? "?").toUpperCase();
  if (size < TWO_CHAR_MIN) return first;
  if (/^[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/u.test(trimmed)) {
    return chars.slice(0, 2).join("");
  }
  const words = trimmed.split(/\s+/).filter(Boolean);
  if (words.length >= 2) {
    return (
      (Array.from(words[0] ?? "")[0] ?? "") + (Array.from(words[1] ?? "")[0] ?? "")
    ).toUpperCase();
  }
  return first;
}

// 頭像字一律白色；圓的顏色是秘書自己的（inline style 的 CSS 變數是 token 規則的例外）。
export function Avatar({
  name,
  color,
  colorDark,
  src,
  size = 26,
  pulse,
  waiting,
  className,
}: AvatarProps) {
  const [loaded, setLoaded] = useState(() => (src ? loadedSrc.has(src) : false));
  const [failed, setFailed] = useState(() => (src ? failedRecently(src) : false));
  useEffect(() => {
    setLoaded(src ? loadedSrc.has(src) : false);
    setFailed(src ? failedRecently(src) : false);
  }, [src]);
  const initials = initialsOf(name, size);
  const twoChars = Array.from(initials).length > 1;
  const style = {
    width: size,
    height: size,
    // 兩個字時縮小，左右才放得下。
    fontSize: Math.round(size * (twoChars ? 0.32 : 0.4)),
    "--agent-color": color || undefined,
    "--agent-color-dark": colorDark || undefined,
  } as CSSProperties;
  return (
    <span
      className={cn(
        "hg-agent relative flex shrink-0 items-center justify-center rounded-full font-semibold text-white",
        pulse && "hg-pulse",
        className,
      )}
      style={style}
      role="img"
      aria-label={name}
    >
      {/* 圖載入好就把字拿掉：透明背景的圖不會透出字來（DEV-157）。 */}
      {src && loaded ? null : (
        <span aria-hidden="true" className="leading-none whitespace-nowrap">
          {initials}
        </span>
      )}
      {src && !failed ? (
        <img
          src={src}
          alt=""
          aria-hidden="true"
          onLoad={() => {
            loadedSrc.add(src);
            failedSrc.delete(src);
            setLoaded(true);
          }}
          onError={() => {
            loadedSrc.delete(src);
            failedSrc.set(src, Date.now());
            setLoaded(false);
            setFailed(true);
          }}
          className={cn(
            "absolute inset-0 size-full rounded-full object-cover",
            loaded ? "opacity-100" : "opacity-0",
          )}
        />
      ) : null}
      {waiting ? (
        <span
          className="absolute -top-px -right-px box-border size-[15px] rounded-full border-[3px] border-bg bg-attention"
          data-dot="waiting"
          aria-hidden="true"
        />
      ) : null}
    </span>
  );
}
