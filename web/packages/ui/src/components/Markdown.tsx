import { FileText } from "lucide-react";
import { type AnchorHTMLAttributes, createContext, type ReactNode, useContext } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "../lib/cn";
import { ImageViewer } from "./ImageViewer";

// 跟伺服器 /share/ 以圖片 inline 送的副檔名同一份（`internal/base/app/board/server.go` 的 imageTypes）；別的格式瀏覽器拿到的是下載檔，畫不出縮圖。
const IMAGE = /\.(png|jpe?g|gif|webp)$/i;

/** 分享檔（`/share/<名字>`）照副檔名算不算圖：是的畫縮圖、點了看大圖，其他是下載連結。聊天與任務單、待辦的附件共用。 */
export function isShareImage(name: string): boolean {
  return IMAGE.test(name);
}
const VIDEO = /\.(mp4|webm|mov)$/i;
const SHARE_PREFIX = "/share/";
// notify.share 存檔的名字是 <ULID>-<原檔名>、看板上傳的是 <YYYYMMDD-HHMMSS>-<4 碼十六進位>-<原檔名>；畫面上只留原檔名。
const ULID_PREFIX = /^(?:[0-9A-Z]{26}|\d{8}-\d{6}-[0-9a-f]{4})-/;
// 裸網址：/share/<名字>（名字只有 [A-Za-z0-9._-] 與中日韓文字，見 notify.share）。
const BARE_SHARE = /\/share\/[\p{L}\p{N}._-]+/gu;

/** href 是 /share/<名字> 就回相對路徑（絕對網址也一樣：這個看板自己就伺服 /share），不是就回 null。 */
export function shareHref(href: string | undefined): string | null {
  if (!href) return null;
  let path = href;
  if (/^https?:\/\//i.test(href)) {
    try {
      path = new URL(href).pathname;
    } catch {
      return null;
    }
  }
  return path.startsWith(SHARE_PREFIX) && path.length > SHARE_PREFIX.length ? path : null;
}

function shareName(path: string): string {
  let name = path.slice(SHARE_PREFIX.length);
  try {
    name = decodeURIComponent(name);
  } catch {
    // 名字不是合法的百分比編碼：照原樣顯示。
  }
  return name.replace(ULID_PREFIX, "");
}

interface Node {
  type: string;
  value?: string;
  url?: string;
  children?: Node[];
}

// remark 外掛：文字裡裸寫的 /share/<名字> 變成連結（連結、行內程式碼、程式碼區塊裡的不動）。
function remarkBareShare() {
  const walk = (node: Node) => {
    if (!node.children) return;
    const next: Node[] = [];
    for (const child of node.children) {
      if (child.type === "text" && typeof child.value === "string") {
        next.push(...splitShare(child.value));
        continue;
      }
      if (child.type !== "link" && child.type !== "linkReference" && child.type !== "inlineCode") {
        walk(child);
      }
      next.push(child);
    }
    node.children = next;
  };
  return (tree: Node) => walk(tree);
}

function splitShare(text: string): Node[] {
  const out: Node[] = [];
  let last = 0;
  for (const m of text.matchAll(BARE_SHARE)) {
    const at = m.index ?? 0;
    if (at > last) out.push({ type: "text", value: text.slice(last, at) });
    out.push({
      type: "link",
      url: encodeURI(m[0]),
      children: [{ type: "text", value: m[0] }],
    });
    last = at + m[0].length;
  }
  if (last === 0) return [{ type: "text", value: text }];
  if (last < text.length) out.push({ type: "text", value: text.slice(last) });
  return out;
}

// 分享檔在哪台（看板連多台，S3）：別台的對話裡 /share/<名字> 要補成那台的 origin；主伺服器是 ""（相對網址，跟以前一樣）。
const ShareBase = createContext("");

function ShareLink({ path: rel }: { path: string }) {
  const name = shareName(rel);
  const base = useContext(ShareBase);
  const path = base ? `${base}${rel}` : rel;
  if (IMAGE.test(name)) return <ImageViewer src={path} name={name} />;
  if (VIDEO.test(name)) {
    // 分享的影片直接播（可以按播放、拖時間）；下面一行小字是原檔名，點了在這頁放大看（不開新分頁，FEN-30）。
    return (
      <span className="my-1 inline-flex max-w-full flex-col items-start gap-1 align-top">
        {/* biome-ignore lint/a11y/useMediaCaption: 秘書分享的檔案沒有字幕 */}
        <video
          controls
          playsInline
          preload="metadata"
          src={path}
          aria-label={name}
          className="block max-h-80 w-auto max-w-[240px] rounded-xl border border-border bg-media-bg"
        />
        <ImageViewer
          src={path}
          name={name}
          type="video"
          openLabel="放大看："
          className="inline-flex min-h-11 max-w-[240px] items-center p-0 text-left text-xs text-muted underline underline-offset-2"
        >
          <span className="truncate">{name}</span>
        </ImageViewer>
      </span>
    );
  }
  return (
    // hg-allow-new-tab: 圖片影片以外的分享檔（下載用；圖片與影片在上面走 ImageViewer）
    <a
      href={path}
      target="_blank"
      rel="noopener noreferrer"
      className="my-1 inline-flex min-h-9 max-w-full items-center gap-1.5 rounded-full border border-border bg-surface px-3 text-sm text-fg no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
    >
      <FileText className="size-4 shrink-0 text-muted" strokeWidth={1.8} aria-hidden="true" />
      <span className="truncate">{name}</span>
    </a>
  );
}

function Anchor({ href, children }: AnchorHTMLAttributes<HTMLAnchorElement> & { node?: unknown }) {
  const share = shareHref(href);
  if (share) return <ShareLink path={share} />;
  return (
    // hg-allow-new-tab: 站外連結（分享檔在上面已經分出去了）
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-accent underline underline-offset-2 break-words"
    >
      {children}
    </a>
  );
}

// ![說明](/share/x.png) 也算分享的圖；別的圖片來源不載入，只留說明文字（不替外部網址發請求）。
function Img({ src, alt }: { src?: string | Blob; alt?: string }) {
  const share = typeof src === "string" ? shareHref(src) : null;
  if (share) return <ShareLink path={share} />;
  return alt ? <span className="text-muted">{alt}</span> : null;
}

const components: Components = {
  a: Anchor,
  img: Img,
  p: ({ children }) => <p className="my-2 first:mt-0 last:mb-0 break-words">{children}</p>,
  ul: ({ children }) => <ul className="my-2 list-disc pl-5 first:mt-0 last:mb-0">{children}</ul>,
  ol: ({ children }) => <ol className="my-2 list-decimal pl-5 first:mt-0 last:mb-0">{children}</ol>,
  li: ({ children }) => <li className="my-0.5">{children}</li>,
  h1: ({ children }) => <h3 className="mt-3 mb-1 text-lg font-bold">{children}</h3>,
  h2: ({ children }) => <h3 className="mt-3 mb-1 text-[17px] font-bold">{children}</h3>,
  h3: ({ children }) => <h3 className="mt-3 mb-1 text-base font-bold">{children}</h3>,
  h4: ({ children }) => <h4 className="mt-2 mb-1 text-base font-semibold">{children}</h4>,
  h5: ({ children }) => <h5 className="mt-2 mb-1 text-base font-semibold">{children}</h5>,
  h6: ({ children }) => <h6 className="mt-2 mb-1 text-base font-semibold">{children}</h6>,
  blockquote: ({ children }) => (
    <blockquote className="my-2 border-l-[3px] border-border-strong pl-3 text-muted">
      {children}
    </blockquote>
  ),
  hr: () => <hr className="my-3 border-border" />,
  pre: ({ children }) => (
    <pre className="hg-scroll my-2 overflow-x-auto rounded-xl bg-surface-2 p-3 font-mono text-[13px] leading-normal">
      {children}
    </pre>
  ),
  code: ({ children, className }) =>
    className ? (
      <code className={className}>{children}</code>
    ) : (
      <code className="rounded-md bg-surface-2 px-1.5 py-0.5 font-mono text-[0.9em]">
        {children}
      </code>
    ),
  table: ({ children }) => (
    <div className="hg-scroll my-2 max-w-full overflow-x-auto">
      <table className="border-collapse text-sm">{children}</table>
    </div>
  ),
  // 每格至少 4.5em（約三個字）：手機上旁邊有一欄很長時，「項目」「數量」不會被擠成一字一行（WEB-120）。
  th: ({ children }) => (
    <th className="min-w-[4.5em] border border-border bg-surface-2 px-2.5 py-1 text-left font-semibold">
      {children}
    </th>
  ),
  td: ({ children }) => (
    <td className="min-w-[4.5em] border border-border px-2.5 py-1">{children}</td>
  ),
};

export interface MarkdownProps {
  children: string;
  className?: string;
  /**
   * /share/<名字> 屬於哪台：那台的 origin（`https://…`，不帶結尾斜線）。不給＝這個看板自己（相對網址）。
   * 訊息裡存的永遠是相對路徑（那台的秘書照自己的家讀），只有顯示時補上。
   */
  shareBase?: string;
}

// 聊天裡 AI 訊息的 Markdown：不開 raw HTML；連結一律開新分頁；/share/<名字> 顯示縮圖或下載 chip（REQ-34）。
export function Markdown({ children, className, shareBase = "" }: MarkdownProps): ReactNode {
  return (
    <div className={cn("min-w-0 text-[15px] leading-[1.6] break-words", className)}>
      <ShareBase.Provider value={shareBase}>
        <ReactMarkdown remarkPlugins={[remarkGfm, remarkBareShare]} components={components}>
          {children}
        </ReactMarkdown>
      </ShareBase.Provider>
    </div>
  );
}
