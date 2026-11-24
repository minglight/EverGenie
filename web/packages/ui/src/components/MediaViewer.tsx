import { Dialog } from "@base-ui/react/dialog";
import { ChevronLeft, ChevronRight, X } from "lucide-react";
import { useEffect, useRef } from "react";

export interface MediaItem {
  /** 圖或影片的網址。 */
  src: string;
  /** 名字：圖的 alt、大圖的標題（螢幕閱讀器唸的）。 */
  name?: string;
  /** 不給就看副檔名：`.mp4`、`.webm`、`.mov`、`.m4v` 是影片，其餘當圖。 */
  type?: "image" | "video";
}

export interface MediaViewerProps {
  /** 可以看的圖與影片；多於一項時可以左右換。 */
  items: MediaItem[];
  /** 現在看第幾項；`null` ＝ 關著。 */
  index: number | null;
  /** 換一項或關掉（`null`）。 */
  onIndex: (index: number | null) => void;
  /** 右上角關閉鈕唸出來的字。 */
  closeLabel?: string;
  prevLabel?: string;
  nextLabel?: string;
}

const VIDEO = /\.(mp4|webm|mov|m4v)(?:[?#].*)?$/i;

/** 這一項是不是影片：有寫 `type` 照它，沒寫看副檔名。 */
export function isVideoItem(item: MediaItem): boolean {
  return item.type ? item.type === "video" : VIDEO.test(item.src);
}

// 開著的看大圖有幾個（正常只有一個）。StrictMode 會把 effect 收掉再跑一次，靠它分辨「真的關了」與「馬上又開」。
let holders = 0;
const MARK = "hgMediaViewer";

function marked(): boolean {
  const s: unknown = window.history.state;
  return typeof s === "object" && s !== null && (s as Record<string, unknown>)[MARK] === true;
}

/**
 * 開著時多疊一筆歷史（同一個網址），手機的上一頁／返回手勢就是關掉大圖、不是離開頁面。
 * 用 ✕、背景、Esc 關的，事後把那一筆退掉，歷史不多一筆。
 * 那一筆帶著原本的 `history.state`（看板路由的鍵不變，套件頁不重掛，FEN-29），只把路由的第幾筆加一。
 */
function useBackCloses(open: boolean, onClose: () => void) {
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    if (!open) return;
    holders++;
    if (!marked()) {
      const prev = (window.history.state ?? {}) as Record<string, unknown>;
      const next: Record<string, unknown> = { ...prev, [MARK]: true };
      if (typeof prev.__TSR_index === "number") next.__TSR_index = prev.__TSR_index + 1;
      window.history.pushState(next, "");
    }
    const onPop = () => {
      if (!marked()) close.current();
    };
    window.addEventListener("popstate", onPop);
    return () => {
      window.removeEventListener("popstate", onPop);
      holders--;
      // 等一拍：StrictMode 的假卸載馬上會再掛回來，那時不能退。
      window.setTimeout(() => {
        if (holders === 0 && marked()) window.history.back();
      }, 0);
    };
  }, [open]);
}

/**
 * 看大圖（圖片與影片）：在這頁疊一層，右上角 ✕、點背景、Esc、手機的上一頁都是關掉、回到原來的位置。
 * 看板與套件頁面要讓人把圖或影片放大看，一律用這個，不開新分頁（加到主畫面的看板沒有分頁列，開了退不回來；FEN-30）。
 *
 * 用法：`const [at, setAt] = useState<number | null>(null)`，縮圖 `onClick={() => setAt(i)}`，
 * 旁邊放 `<MediaViewer items={[{ src, name }]} index={at} onIndex={setAt} />`。
 */
export function MediaViewer({
  items,
  index,
  onIndex,
  closeLabel = "關閉",
  prevLabel = "上一張",
  nextLabel = "下一張",
}: MediaViewerProps) {
  const at =
    index === null || items.length === 0 ? null : Math.min(Math.max(index, 0), items.length - 1);
  const open = at !== null;
  useBackCloses(open, () => onIndex(null));
  const item = at === null ? undefined : items[at];
  const many = items.length > 1;
  const step = (by: number) => {
    if (at === null || !many) return;
    onIndex((at + by + items.length) % items.length);
  };
  const name = item?.name ?? "";
  const nav =
    "absolute top-1/2 flex size-11 -translate-y-1/2 items-center justify-center rounded-full bg-media-scrim text-on-media focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-on-media";
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onIndex(null);
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-50 bg-media-bg/90" />
        <Dialog.Popup
          aria-modal="true"
          data-testid="media-viewer"
          className="fixed inset-0 z-50 flex items-center justify-center pt-[max(env(safe-area-inset-top),16px)] pr-[max(env(safe-area-inset-right),16px)] pb-[max(env(safe-area-inset-bottom),16px)] pl-[max(env(safe-area-inset-left),16px)] outline-none"
          onClick={(e) => {
            if (e.target === e.currentTarget) onIndex(null);
          }}
          onKeyDown={(e) => {
            if (e.key === "ArrowLeft") step(-1);
            else if (e.key === "ArrowRight") step(1);
          }}
        >
          <Dialog.Title className="sr-only">{name || closeLabel}</Dialog.Title>
          {item === undefined ? null : isVideoItem(item) ? (
            // biome-ignore lint/a11y/useMediaCaption: 角色的動畫、秘書分享的影片沒有字幕
            <video
              key={item.src}
              data-testid="media-viewer-video"
              src={item.src}
              aria-label={name}
              controls
              autoPlay
              loop
              playsInline
              className="max-h-full max-w-full rounded-lg bg-media-bg"
            />
          ) : (
            <img
              key={item.src}
              data-testid="media-viewer-img"
              src={item.src}
              alt={name}
              className="max-h-full max-w-full rounded-lg object-contain"
            />
          )}
          {many ? (
            <>
              <button
                type="button"
                aria-label={prevLabel}
                data-testid="media-viewer-prev"
                onClick={() => step(-1)}
                className={`${nav} left-[max(env(safe-area-inset-left),12px)]`}
              >
                <ChevronLeft className="size-6" strokeWidth={2} aria-hidden="true" />
              </button>
              <button
                type="button"
                aria-label={nextLabel}
                data-testid="media-viewer-next"
                onClick={() => step(1)}
                className={`${nav} right-[max(env(safe-area-inset-right),12px)]`}
              >
                <ChevronRight className="size-6" strokeWidth={2} aria-hidden="true" />
              </button>
              <span
                data-testid="media-viewer-count"
                className="absolute bottom-[max(env(safe-area-inset-bottom),12px)] left-1/2 -translate-x-1/2 rounded-full bg-media-scrim px-3 py-1 text-sm text-on-media"
              >
                {(at ?? 0) + 1} / {items.length}
              </span>
            </>
          ) : null}
          <Dialog.Close
            aria-label={closeLabel}
            data-testid="media-viewer-close"
            className="absolute top-[max(env(safe-area-inset-top),12px)] right-[max(env(safe-area-inset-right),12px)] flex size-11 items-center justify-center rounded-full bg-media-scrim text-on-media focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-on-media"
          >
            <X className="size-6" strokeWidth={2} aria-hidden="true" />
          </Dialog.Close>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
