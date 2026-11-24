import { ArrowUp, FileText, Paperclip, RotateCw, X } from "lucide-react";
import {
  type ChangeEvent,
  type ClipboardEvent,
  type KeyboardEvent,
  type TextareaHTMLAttributes,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { cn } from "../lib/cn";
import { Spinner } from "./Spinner";

const LINE_PX = 24;
const MAX_LINES = 6;
const COARSE = "(pointer: coarse)";

// 觸控裝置：Enter 換行、只靠送出鈕（看輸入裝置，不看寬度）。
function useCoarsePointer(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      if (typeof window.matchMedia !== "function") return () => {};
      const mq = window.matchMedia(COARSE);
      mq.addEventListener("change", onChange);
      return () => mq.removeEventListener("change", onChange);
    },
    () => typeof window.matchMedia === "function" && window.matchMedia(COARSE).matches,
    () => false,
  );
}

export interface UploadedFile {
  url: string;
  name: string;
}

/** 附件相關的文字（給讀屏軟體與按鈕的名稱）；沒給就用繁體中文預設。 */
export interface ComposerAttachLabels {
  attach: string;
  remove: string;
  retry: string;
  uploading: string;
  failed: string;
}

const DEFAULT_ATTACH_LABELS: ComposerAttachLabels = {
  attach: "附加檔案",
  remove: "移除",
  retry: "重試",
  uploading: "上傳中",
  failed: "上傳失敗",
};

export interface ComposerProps {
  value: string;
  onChange: (value: string) => void;
  /**
   * 送出：參數是要送的完整文字（輸入框的字加上每個附件一行的 Markdown）；meta 帶輸入框的原字（沒去掉前後空白）
   * 與附件數，給外殼判斷是不是「/」指令（看板 WEB-83）。
   */
  onSend: (message: string, meta: ComposerSendMeta) => void;
  /** 輸入框的按鍵先給外殼（例如「/」指令選單的方向鍵、Enter）；外殼 preventDefault 了就不再當送出或換行。 */
  onKeyDown?: (e: KeyboardEvent<HTMLTextAreaElement>) => void;
  /** 輸入框的無障礙屬性（外殼在上面開選單時用：combobox、展開、目前選到哪一列）。 */
  inputProps?: Pick<
    TextareaHTMLAttributes<HTMLTextAreaElement>,
    "role" | "aria-expanded" | "aria-controls" | "aria-activedescendant" | "aria-autocomplete"
  >;
  /** 有這個 prop 才出現迴紋針：選了檔案就逐一上傳；上傳函式由外殼注入，ui 套件不直接打 API（REQ-82）。 */
  onUpload?: (file: File) => Promise<UploadedFile>;
  attachLabels?: Partial<ComposerAttachLabels>;
  /** 輸入框的可見標籤（給讀屏軟體）與 placeholder 範例。 */
  label: string;
  placeholder?: string;
  sendLabel: string;
  /** 外殼正在處理上一次送出（例如「/」指令等引擎回覆）：送出鈕停用、轉圈，Enter 也不送（FEN-20）。 */
  busy?: boolean;
  className?: string;
}

export interface ComposerSendMeta {
  raw: string;
  attachments: number;
}

type AttachmentStatus = "uploading" | "done" | "error";

interface Attachment {
  id: number;
  file: File;
  status: AttachmentStatus;
  uploaded?: UploadedFile;
  error?: string;
  /** 圖片的本機預覽（blob 網址）；上傳完成後仍用它，不必再向伺服器要一次。 */
  preview?: string;
}

const IMAGE_EXT = /\.(png|jpe?g|gif|webp|heic|heif|avif|bmp)$/i;

function isImage(file: File): boolean {
  return file.type.startsWith("image/") || IMAGE_EXT.test(file.name);
}

/** 訊息裡的一行附件：圖片 `![原檔名](/share/x)`、其他 `[原檔名](/share/x)`；原檔名裡會弄壞 Markdown 的字元換成底線。 */
export function attachmentLine(originalName: string, url: string, image: boolean): string {
  const label = originalName.replace(/[[\]\\\r\n]/g, "_");
  return `${image ? "!" : ""}[${label}](${url})`;
}

function previewOf(file: File): string | undefined {
  if (!isImage(file) || typeof URL.createObjectURL !== "function") return undefined;
  try {
    return URL.createObjectURL(file);
  } catch {
    return undefined;
  }
}

function revoke(preview: string | undefined) {
  if (preview && typeof URL.revokeObjectURL === "function") URL.revokeObjectURL(preview);
}

// 聊天輸入框：圓角 24、自動長高到 6 行再捲動、字級 16（iOS 聚焦不放大）、右邊 44 的圓形送出鈕（FEN-23、FEN-24）。
export function Composer({
  value,
  onChange,
  onSend,
  onKeyDown: onKeyDownProp,
  inputProps,
  onUpload,
  attachLabels,
  label,
  placeholder,
  sendLabel,
  busy = false,
  className,
}: ComposerProps) {
  const area = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const coarse = useCoarsePointer();
  const text = { ...DEFAULT_ATTACH_LABELS, ...attachLabels };
  const [files, setFiles] = useState<Attachment[]>([]);
  const nextId = useRef(1);
  const alive = useRef(true);
  const previews = useRef<string[]>([]);
  const uploaded = files.filter((f) => f.status === "done");
  // 還在上傳、或上傳失敗還沒處理（重試或拿掉）時不能送，避免附件被默默漏掉。
  const blocked = files.some((f) => f.status !== "done");
  const empty = (value.trim() === "" && uploaded.length === 0) || blocked || busy;

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      for (const p of previews.current) revoke(p);
    };
  }, []);

  const patch = useCallback((id: number, change: Partial<Attachment>) => {
    if (!alive.current) return;
    setFiles((all) => all.map((f) => (f.id === id ? { ...f, ...change } : f)));
  }, []);

  const run = useCallback(
    async (id: number, file: File) => {
      if (!onUpload) return;
      try {
        const result = await onUpload(file);
        patch(id, { status: "done", uploaded: result, error: undefined });
      } catch (err) {
        patch(id, { status: "error", error: err instanceof Error ? err.message : String(err) });
      }
    },
    [onUpload, patch],
  );

  const addFiles = (picked: File[]) => {
    if (picked.length === 0) return;
    const added = picked.map((file): Attachment => {
      const preview = previewOf(file);
      if (preview) previews.current.push(preview);
      return { id: nextId.current++, file, status: "uploading", preview };
    });
    setFiles((all) => [...all, ...added]);
    // 逐一上傳：前一個結束才傳下一個。
    void (async () => {
      for (const a of added) await run(a.id, a.file);
    })();
  };

  const onPick = (e: ChangeEvent<HTMLInputElement>) => {
    const picked = Array.from(e.target.files ?? []);
    e.target.value = ""; // 同一個檔案可以再選一次
    addFiles(picked);
  };

  // 貼上截圖或檔案：有檔案就當附件（沒有 onUpload 時不處理），純文字照常貼。
  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (!onUpload) return;
    const pasted = Array.from(e.clipboardData.files).map((f) =>
      f.name && f.name !== "image.png"
        ? f
        : new File([f], `paste-${Date.now()}.png`, { type: f.type }),
    );
    if (pasted.length === 0) return;
    e.preventDefault();
    addFiles(pasted);
  };

  const retry = (a: Attachment) => {
    patch(a.id, { status: "uploading", error: undefined });
    void run(a.id, a.file);
  };

  const remove = (a: Attachment) => {
    revoke(a.preview);
    previews.current = previews.current.filter((p) => p !== a.preview);
    setFiles((all) => all.filter((f) => f.id !== a.id));
  };

  const send = () => {
    const lines = uploaded.map((a) =>
      attachmentLine(a.file.name, a.uploaded?.url ?? "", isImage(a.file)),
    );
    const message = [value.trim(), ...lines].filter((x) => x !== "").join("\n");
    onSend(message, { raw: value, attachments: uploaded.length });
    for (const a of files) revoke(a.preview);
    previews.current = [];
    setFiles([]);
  };

  // value 變了就重算高度（含送出後清空）。
  // biome-ignore lint/correctness/useExhaustiveDependencies: value 是重算高度的觸發條件
  useLayoutEffect(() => {
    const el = area.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, LINE_PX * MAX_LINES + 20)}px`;
  }, [value]);

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    onKeyDownProp?.(e);
    if (e.defaultPrevented) return;
    if (e.key !== "Enter" || e.shiftKey || coarse) return;
    // 輸入法選字中的 Enter 是確認選字，不是送出。
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    e.preventDefault();
    if (!empty) send();
  };

  return (
    <div
      // 選了附件還沒送：看板換新版時不自動重新整理（WEB-172）。
      data-unsaved={files.length > 0 ? "" : undefined}
      className={cn(
        "flex flex-col rounded-3xl border border-border-strong bg-surface py-1 pr-1 pl-3.5",
        className,
      )}
    >
      {files.length > 0 ? (
        <ul
          aria-label={text.attach}
          className="m-0 flex list-none flex-wrap gap-2 p-0 pt-2 pr-2.5 pb-1"
        >
          {files.map((a) => (
            <AttachmentChip
              key={a.id}
              item={a}
              labels={text}
              onRemove={() => remove(a)}
              onRetry={() => retry(a)}
            />
          ))}
        </ul>
      ) : null}
      <div className="flex items-end gap-2">
        {onUpload ? (
          <>
            <input
              ref={picker}
              type="file"
              multiple
              hidden
              data-testid="composer-file"
              onChange={onPick}
            />
            <button
              type="button"
              aria-label={text.attach}
              onClick={() => picker.current?.click()}
              className="-ml-2 flex size-11 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
            >
              <Paperclip className="size-5" strokeWidth={1.8} aria-hidden="true" />
            </button>
          </>
        ) : null}
        <label htmlFor="hg-composer" className="sr-only">
          {label}
        </label>
        <textarea
          {...inputProps}
          id="hg-composer"
          ref={area}
          rows={1}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          placeholder={placeholder}
          autoComplete="off"
          enterKeyHint={coarse ? "enter" : "send"}
          className="min-h-11 min-w-0 flex-1 resize-none self-center border-0 bg-transparent py-2.5 text-base leading-6 text-fg outline-none placeholder:text-muted"
        />
        <button
          type="button"
          aria-label={sendLabel}
          aria-busy={busy || undefined}
          disabled={empty}
          onClick={() => {
            if (empty) return;
            send();
            area.current?.focus();
          }}
          className={cn(
            "flex size-11 shrink-0 items-center justify-center rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
            empty ? "bg-surface-2 text-muted" : "bg-accent text-accent-fg",
          )}
        >
          {busy ? (
            <Spinner className="size-5" />
          ) : (
            <ArrowUp className="size-5" strokeWidth={2} aria-hidden="true" />
          )}
        </button>
      </div>
    </div>
  );
}

function AttachmentChip({
  item,
  labels,
  onRemove,
  onRetry,
}: {
  item: Attachment;
  labels: ComposerAttachLabels;
  onRemove: () => void;
  onRetry: () => void;
}) {
  const failed = item.status === "error";
  return (
    <li
      data-attachment={item.status}
      className={cn(
        "relative flex h-11 max-w-full items-center gap-2 rounded-xl border bg-surface-2 py-1 pr-1 pl-1",
        failed ? "border-danger" : "border-border",
      )}
    >
      {item.preview ? (
        <img src={item.preview} alt="" className="size-9 shrink-0 rounded-lg object-cover" />
      ) : (
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-surface">
          <FileText className="size-4 text-muted" strokeWidth={1.8} aria-hidden="true" />
        </span>
      )}
      <span className="flex min-w-0 flex-col text-xs leading-tight">
        <span className="max-w-[140px] truncate text-fg">{item.file.name}</span>
        {failed ? (
          <span role="alert" className="max-w-[140px] truncate text-danger">
            {labels.failed}：{item.error}
          </span>
        ) : null}
      </span>
      {item.status === "uploading" ? (
        <Spinner label={`${labels.uploading} ${item.file.name}`} className="size-4" />
      ) : null}
      {/* 按鈕看起來 36px，點得到的範圍用 ::after 往外擴到 44px（UXC-5，P6 收尾）。 */}
      {failed ? (
        <button
          type="button"
          aria-label={`${labels.retry} ${item.file.name}`}
          onClick={onRetry}
          className="relative flex size-9 shrink-0 items-center justify-center rounded-full after:absolute after:-inset-1 after:rounded-full text-danger focus-visible:outline-2 focus-visible:outline-focus"
        >
          <RotateCw className="size-4" strokeWidth={1.8} aria-hidden="true" />
        </button>
      ) : null}
      <button
        type="button"
        aria-label={`${labels.remove} ${item.file.name}`}
        onClick={onRemove}
        className="relative flex size-9 shrink-0 items-center justify-center rounded-full after:absolute after:-inset-1 after:rounded-full text-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-focus"
      >
        <X className="size-4" strokeWidth={1.8} aria-hidden="true" />
      </button>
    </li>
  );
}
