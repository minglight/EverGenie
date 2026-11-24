import { type ReactNode, useState } from "react";
import { cn } from "../lib/cn";
import { MediaViewer } from "./MediaViewer";

export interface ImageViewerProps {
  /** 圖（或影片）的網址（已經補好哪台的 origin）。 */
  src: string;
  /** 名字：縮圖的 alt、大圖的標題。 */
  name: string;
  /** 影片傳 "video"；不給看副檔名。 */
  type?: "image" | "video";
  /** 縮圖；不給就畫預設的 240px 縮圖。 */
  children?: ReactNode;
  /** 縮圖按鈕的 class（不給用聊天裡的樣子）。 */
  className?: string;
  /** 縮圖按鈕唸出來的字（名字接在後面）；看板傳 i18n 的字。 */
  openLabel?: string;
  /** 大圖右上角關閉鈕唸出來的字。 */
  closeLabel?: string;
}

// 一張縮圖＋看大圖：點縮圖在這頁疊一層（`MediaViewer`），右上角 ✕、點背景、Esc、手機的上一頁關掉。
// 不開新分頁：加到主畫面的看板沒有分頁列，開了就退不回來（WEB-132、FEN-30）。聊天、訊息、任務單與待辦的附件共用。
export function ImageViewer({
  src,
  name,
  type,
  children,
  className,
  openLabel = "看大圖：",
  closeLabel = "關閉",
}: ImageViewerProps) {
  const [at, setAt] = useState<number | null>(null);
  return (
    <>
      <button
        type="button"
        aria-label={`${openLabel}${name}`}
        onClick={() => setAt(0)}
        className={cn(
          "cursor-zoom-in focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
          className ?? "my-1 inline-block max-w-full rounded-xl p-0 align-top",
        )}
      >
        {children ?? (
          <img
            src={src}
            alt={name}
            loading="lazy"
            className="block max-h-80 w-auto max-w-[240px] rounded-xl border border-border"
          />
        )}
      </button>
      <MediaViewer
        items={[{ src, name, type }]}
        index={at}
        onIndex={setAt}
        closeLabel={closeLabel}
      />
    </>
  );
}
