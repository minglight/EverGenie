import {
  type CSSProperties,
  type ReactNode,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { cn } from "../lib/cn";

export interface PersonaMediaProps {
  /** 人名：只用在沒有圖時的首字（影片與圖是裝飾，名字在旁邊的文字，所以不給讀屏軟體）。 */
  name: string;
  /** 圖片網址（同時是影片的 poster）。 */
  src?: string;
  /** 動態影片網址；沒有就只放圖。只有一支時用它；有多支用 `videos`。 */
  video?: string;
  /** 多支動態（同一張圖回到原位的幾種動作）：每次隨機挑一支播（DEV-125）；有它就以它為準。 */
  videos?: string[];
  /** 兩次播放之間隨機等幾秒（含兩端）；預設 15～45 秒。 */
  idleSec?: readonly [number, number];
  /** 伺服器給的秘書顏色：圖也沒有時用它鋪滿。 */
  color?: string;
  colorDark?: string;
  /** 暫停（分頁在背景、角色區收合）；回到 false 再播。 */
  paused?: boolean;
  /** 底部漸層的高度（px），放在上面的白字靠它看得清楚；0 不畫。 */
  scrimHeight?: number;
  className?: string;
  /** 疊在影片上的內容（人名、狀態），自己用 absolute 定位。 */
  children?: ReactNode;
}

const REDUCED = "(prefers-reduced-motion: reduce)";

/** 兩次播放之間隨機等待的預設秒數範圍。 */
export const DEFAULT_IDLE_SEC = [15, 45] as const;
/** 剛出現時第一次播放前等幾秒（短一點，一打開就看到她動）。 */
const FIRST_WAIT_SEC = [1.5, 4] as const;
/** 叫它播之後多久還沒開始播，就放棄這一輪。 */
const PLAY_WATCHDOG_MS = 10_000;

function randomBetween([lo, hi]: readonly [number, number]): number {
  return lo + Math.random() * Math.max(0, hi - lo);
}

function pick<T>(items: readonly T[]): T {
  return items[Math.min(items.length - 1, Math.floor(Math.random() * items.length))] as T;
}

function useReducedMotion(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      if (typeof window.matchMedia !== "function") return () => {};
      const mq = window.matchMedia(REDUCED);
      mq.addEventListener("change", onChange);
      return () => mq.removeEventListener("change", onChange);
    },
    () => typeof window.matchMedia === "function" && window.matchMedia(REDUCED).matches,
    () => false,
  );
}

function saveData(): boolean {
  const conn = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection;
  return conn?.saveData === true;
}

// 秘書的角色畫面：有動態影片而且可以播，就「播一次、停一下、隨機等幾秒再播」（靜音、自動、不全螢幕）；
// 停的時候看到的是靜態圖，所以影片要回到原位才不會跳；有多支就每次隨機挑一支（DEV-125）。
// 沒有影片、使用者要求減少動態、開了省流量、影片載入失敗，都只放圖；圖也沒有就用首字與秘書顏色鋪滿。
export function PersonaMedia({
  name,
  src,
  video,
  videos,
  idleSec = DEFAULT_IDLE_SEC,
  color,
  colorDark,
  paused = false,
  scrimHeight = 120,
  className,
  children,
}: PersonaMediaProps) {
  const reduced = useReducedMotion();
  const [failedVideos, setFailedVideos] = useState<string[]>([]);
  const [failedImage, setFailedImage] = useState<string | null>(null);
  // clip.n：第幾次播放（0＝還沒播過）；url：這次播哪一支。
  const [clip, setClip] = useState<{ url: string; n: number }>({ url: "", n: 0 });
  const [cycle, setCycle] = useState(0); // 每播完（或播不出來）加 1，重新排下一次
  const [showing, setShowing] = useState(false); // 影片是不是正在畫面上（停的時候讓位給靜態圖）
  const ref = useRef<HTMLVideoElement>(null);
  const settledRef = useRef(false); // 這一輪有結果了嗎（開始播、播完或出錯都算；看門狗用）
  const source = videos?.length ? videos : video ? [video] : [];
  const sourceKey = source.join("\n");
  const pool = source.filter((u) => !failedVideos.includes(u));
  const poolKey = pool.join("\n");
  const idleKey = `${idleSec[0]}-${idleSec[1]}`;
  const canPlay = sourceKey !== "" && !reduced && !saveData();
  const playVideo = canPlay && pool.length > 0;
  const showImage = Boolean(src) && failedImage !== src;
  const activeKey = canPlay ? sourceKey : "";
  // 影片從無到有（滑鼠移上去才給影片）或換了一組（換秘書）：全部從頭來，不沿用上一組的狀態與壞掉的清單。
  // biome-ignore lint/correctness/useExhaustiveDependencies: 只看 activeKey 變了沒有
  useEffect(() => {
    settledRef.current = false;
    setClip((c) => (c.n === 0 && c.url === "" ? c : { url: "", n: 0 }));
    setShowing(false);
    setCycle(0);
    setFailedVideos((f) => (f.length ? [] : f));
  }, [activeKey]);

  // 排下一次：等一個隨機秒數再挑一支。暫停（背景分頁、收合）或這一輪還在播時不排（回來接著播，不會被新的計時打斷）。
  // biome-ignore lint/correctness/useExhaustiveDependencies: pool/idleSec 用 key 比內容，不然每次 render 都重排
  useEffect(() => {
    if (!playVideo || paused || showing) return;
    const wait = randomBetween(cycle === 0 ? FIRST_WAIT_SEC : idleSec) * 1000;
    const timer = setTimeout(() => {
      setClip((c) => ({ url: pick(pool), n: c.n + 1 }));
    }, wait);
    return () => clearTimeout(timer);
  }, [playVideo, paused, showing, cycle, poolKey, idleKey]);

  // 輪到了：從頭播這一支；play() 被擋（iOS 低電量）不算錯誤，當作這輪沒播，等下一輪。
  useEffect(() => {
    const el = ref.current;
    if (!el || clip.n === 0) return;
    try {
      el.currentTime = 0;
    } catch {
      // 還沒載入中繼資料時不能設，之後 play() 本來就從頭開始
    }
    settledRef.current = false;
    const result = el.play() as Promise<void> | undefined;
    result?.catch(() => setCycle((c) => c + 1));
    // 看門狗：play() 沒被拒絕卻一直沒開始播（網路卡住、系統暫停）就放棄這一輪，免得從此不再排播放。
    const watchdog = setTimeout(() => {
      if (!settledRef.current) {
        el.pause();
        setCycle((c) => c + 1);
      }
    }, PLAY_WATCHDOG_MS);
    return () => clearTimeout(watchdog);
  }, [clip.n]);

  // 分頁在背景或收合時暫停；回來時如果這一輪還沒播完就接著播（沒在播的由上面的計時重新排）。
  // biome-ignore lint/correctness/useExhaustiveDependencies: showing 只在暫停切換時讀
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (paused) {
      el.pause();
      return;
    }
    if (showing && !el.ended) (el.play() as Promise<void> | undefined)?.catch(() => {});
  }, [paused]);

  const style = {
    "--agent-color": color || undefined,
    "--agent-color-dark": colorDark || undefined,
  } as CSSProperties;
  const kind = playVideo ? "video" : showImage ? "image" : "fallback";
  // 有靜態圖才在停的時候讓位；沒有圖就讓影片停在最後一格，總比空白好。
  const hidden = showImage && !showing;

  return (
    <div
      className={cn("relative overflow-hidden bg-media-bg", className)}
      data-persona-media={kind}
      style={style}
    >
      <span
        aria-hidden="true"
        className="hg-agent absolute inset-0 flex items-center justify-center text-[min(40vw,160px)] font-semibold text-white"
      >
        {Array.from(name)[0] ?? "?"}
      </span>
      {showImage ? (
        <img
          src={src}
          alt=""
          aria-hidden="true"
          onError={() => setFailedImage(src ?? null)}
          className="absolute inset-0 size-full object-cover object-[50%_30%]"
        />
      ) : null}
      {playVideo ? (
        <video
          ref={ref}
          src={pool.includes(clip.url) ? clip.url : pool[0]}
          poster={src}
          muted
          playsInline
          preload="metadata"
          aria-hidden="true"
          tabIndex={-1}
          data-clip={clip.n}
          onPlaying={() => {
            settledRef.current = true;
            setShowing(true);
          }}
          onEnded={() => {
            settledRef.current = true;
            setShowing(false);
            setCycle((c) => c + 1);
          }}
          onError={() => {
            const bad = pool.includes(clip.url) ? clip.url : pool[0];
            settledRef.current = true;
            setShowing(false);
            if (bad) setFailedVideos((f) => (f.includes(bad) ? f : [...f, bad]));
          }}
          className={cn(
            "absolute inset-0 size-full object-cover object-[50%_30%]",
            hidden && "invisible",
          )}
        />
      ) : null}
      {scrimHeight > 0 ? (
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-x-0 bottom-0 bg-linear-to-t from-media-scrim to-transparent"
          style={{ height: scrimHeight }}
        />
      ) : null}
      {children}
    </div>
  );
}
