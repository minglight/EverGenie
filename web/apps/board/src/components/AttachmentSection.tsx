import { ApiError, upload } from "@evergenie/sdk-web";
import { cn, ImageViewer, Spinner, showToast } from "@evergenie/ui";
import { FileText, Paperclip, X } from "lucide-react";
import { type RefObject, useEffect, useId, useRef, useState } from "react";
import { type SiteRef, siteHref } from "../data/sites";
import { t } from "../i18n/zh-TW";
import { attachmentName, isImageName, namedFile } from "../lib/attachments";
import { useIsDesktop } from "../lib/media";

export interface AttachmentSectionProps {
  /** 單上現在的附件（`/share/<名字>` 連結）。 */
  attachments: readonly string[];
  /** 單在哪台：上傳到那台的 /share/、連結照那台補網址。 */
  site: SiteRef;
  /** 單還沒結束：可以加、拿掉（結束的單照規則不能改）。 */
  open: boolean;
  /** 把已經傳好的連結寫進單（任務單 `add_attachments`、待辦 `add_attachments`）。 */
  onAdd: (urls: string[]) => Promise<unknown>;
  /** 從單上拿掉一個（檔案不刪）。 */
  onRemove: (url: string) => Promise<unknown>;
  /**
   * 只收這個區塊裡的貼上（桌機「今天」右欄：旁邊還有清單與快速輸入，不搶別人的貼上）；
   * 沒給就收整頁的貼上（/tasks/$id 只有這張單）。
   */
  pasteScope?: RefObject<HTMLElement | null>;
}

interface Pending {
  id: number;
  name: string;
}

function errorText(err: unknown): string {
  return err instanceof ApiError || err instanceof Error ? err.message : String(err);
}

// 任務單與待辦的附件（TSK-28、RMD-23，同一個元件）：圖片直接顯示縮圖（點了在這頁看大圖，WEB-132），其他檔案是可以下載的 chip；
// 沒結束的單可以再加（選檔或貼上截圖，傳到那台的 /share/ 再寫進單）與拿掉（只從單上拿掉，檔案不刪；5 秒內可復原）。
export function AttachmentSection({
  attachments: list,
  site,
  open,
  onAdd,
  onRemove,
  pasteScope,
}: AttachmentSectionProps) {
  const headingId = useId();
  const desktop = useIsDesktop();
  const picker = useRef<HTMLInputElement>(null);
  const [pending, setPending] = useState<Pending[]>([]);
  const next = useRef(1);

  const add = async (picked: File[]) => {
    if (picked.length === 0) return;
    const files = picked.map((f) => namedFile(f));
    const rows = files.map((f) => ({ id: next.current++, name: f.name }));
    setPending((cur) => [...cur, ...rows]);
    try {
      // 逐一傳到那台的分享資料夾，全部傳好再一次寫進單（不會只加到一半）。
      const urls: string[] = [];
      for (const f of files) urls.push((await upload(f, site.base)).url);
      await onAdd(urls);
    } catch (err) {
      showToast(t("task.attach.failed", { reason: errorText(err) }));
    } finally {
      setPending((cur) => cur.filter((p) => !rows.some((r) => r.id === p.id)));
    }
  };
  const addRef = useRef(add);
  addRef.current = add;

  // 貼上截圖（⌘V／Ctrl+V）：剪貼簿有檔案才接手（純文字照常貼到輸入框）；別的元件已經處理掉的不管。
  // 有 pasteScope 時：焦點在區塊裡，或焦點不在任何輸入框、最後點的是區塊裡面，才算貼給這張單。
  useEffect(() => {
    if (!open) return;
    let lastInside = false;
    const inside = (n: EventTarget | null) => {
      const scope = pasteScope?.current;
      return Boolean(scope && n instanceof Node && scope.contains(n));
    };
    const onPointer = (e: PointerEvent) => {
      lastInside = inside(e.target);
    };
    const onPaste = (e: ClipboardEvent) => {
      if (e.defaultPrevented) return;
      if (pasteScope) {
        const loose = e.target === document.body || e.target === document.documentElement;
        if (!inside(e.target) && !(loose && lastInside)) return;
      }
      const files = Array.from(e.clipboardData?.files ?? []);
      if (files.length === 0) return;
      e.preventDefault();
      void addRef.current(files);
    };
    document.addEventListener("pointerdown", onPointer, true);
    document.addEventListener("paste", onPaste);
    return () => {
      document.removeEventListener("pointerdown", onPointer, true);
      document.removeEventListener("paste", onPaste);
    };
  }, [open, pasteScope]);

  const remove = async (url: string) => {
    const name = attachmentName(url);
    try {
      await onRemove(url);
      showToast(t("task.attach.removed", { name }), {
        undo: {
          label: t("task.attach.undo"),
          onClick: () =>
            void onAdd([url]).catch((err) =>
              showToast(t("task.attach.failed", { reason: errorText(err) })),
            ),
        },
      });
    } catch (err) {
      showToast(t("task.attach.failed", { reason: errorText(err) }));
    }
  };

  if (!open && list.length === 0) return null;
  const images = list.filter((u) => isImageName(attachmentName(u)));
  const files = list.filter((u) => !isImageName(attachmentName(u)));

  return (
    <section className="flex flex-col gap-2" aria-labelledby={headingId}>
      <div className="flex items-center justify-between gap-2">
        <h2 id={headingId} className="text-sm font-semibold">
          {t("task.section.attachments")}
          {list.length > 0 ? (
            <span className="ml-1 font-normal text-muted">{list.length}</span>
          ) : null}
        </h2>
        {open ? (
          <>
            <input
              ref={picker}
              type="file"
              multiple
              hidden
              data-testid="attach-file"
              onChange={(e) => {
                const picked = Array.from(e.target.files ?? []);
                e.target.value = "";
                void add(picked);
              }}
            />
            <button
              type="button"
              onClick={() => picker.current?.click()}
              className="-mr-2 inline-flex min-h-11 items-center gap-1.5 rounded-full px-3 text-sm font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
            >
              <Paperclip className="size-4" aria-hidden="true" />
              {t("task.attach.add")}
            </button>
          </>
        ) : null}
      </div>
      {list.length === 0 && pending.length === 0 ? (
        <p className="text-sm text-muted">{t("task.attach.empty")}</p>
      ) : null}
      {images.length > 0 ? (
        <ul aria-label={t("task.attach.images")} className="m-0 flex list-none flex-wrap gap-2 p-0">
          {images.map((url) => {
            const name = attachmentName(url);
            return (
              <li key={url} className="relative">
                <ImageViewer
                  src={siteHref(site, url)}
                  name={name}
                  openLabel={t("task.attach.view")}
                  closeLabel={t("task.attach.close")}
                  className="block overflow-hidden rounded-xl border border-border bg-surface-2 p-0"
                >
                  <img
                    src={siteHref(site, url)}
                    alt={name}
                    loading="lazy"
                    className="block h-28 w-auto max-w-[220px] min-w-16 object-cover"
                  />
                </ImageViewer>
                {open ? (
                  <RemoveButton
                    name={name}
                    onClick={() => void remove(url)}
                    className="absolute top-1 right-1 bg-bg/85 shadow-sm backdrop-blur"
                  />
                ) : null}
              </li>
            );
          })}
        </ul>
      ) : null}
      {files.length > 0 || pending.length > 0 ? (
        <ul aria-label={t("task.attach.files")} className="m-0 flex list-none flex-wrap gap-2 p-0">
          {files.map((url) => {
            const name = attachmentName(url);
            return (
              <li
                key={url}
                className="flex h-11 max-w-full items-center gap-2 rounded-xl border border-border bg-surface-2 py-1 pr-0.5 pl-1"
              >
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-surface">
                  <FileText className="size-4 text-muted" strokeWidth={1.8} aria-hidden="true" />
                </span>
                <a
                  href={siteHref(site, url)}
                  download={name}
                  className="max-w-[220px] truncate text-sm text-accent underline"
                >
                  {name}
                </a>
                {open ? (
                  <RemoveButton name={name} onClick={() => void remove(url)} />
                ) : (
                  <span className="w-2" />
                )}
              </li>
            );
          })}
          {pending.map((p) => (
            <li
              key={`pending-${p.id}`}
              data-attachment="uploading"
              className="flex h-11 max-w-full items-center gap-2 rounded-xl border border-border bg-surface-2 py-1 pr-3 pl-1"
            >
              <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-surface">
                <Spinner label={t("task.attach.uploading", { name: p.name })} className="size-4" />
              </span>
              <span className="max-w-[220px] truncate text-sm text-muted">{p.name}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {open ? (
        <p className="text-xs text-muted" data-testid="attach-hint">
          {t(desktop ? "task.attach.hint" : "task.attach.hint.touch")}
        </p>
      ) : null}
    </section>
  );
}

function RemoveButton({
  name,
  onClick,
  className,
}: {
  name: string;
  onClick: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      aria-label={t("task.attach.remove", { name })}
      onClick={onClick}
      className={cn(
        "relative flex size-8 shrink-0 items-center justify-center rounded-full text-muted after:absolute after:-inset-1.5 after:rounded-full hover:text-fg focus-visible:outline-2 focus-visible:outline-focus",
        className,
      )}
    >
      <X className="size-4" strokeWidth={1.8} aria-hidden="true" />
    </button>
  );
}
