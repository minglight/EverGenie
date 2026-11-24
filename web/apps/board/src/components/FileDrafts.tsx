import { ApiError, upload } from "@evergenie/sdk-web";
import { cn, Spinner } from "@evergenie/ui";
import { FileText, Paperclip, RotateCw, X } from "lucide-react";
import { type ClipboardEvent, useCallback, useEffect, useRef, useState } from "react";
import type { SiteRef } from "../data/sites";
import { t } from "../i18n/zh-TW";
import { isImageName, namedFile } from "../lib/attachments";
import { HIT_44 } from "../lib/hit";

// 開單時要附上的檔案（TSK-28）：選了或貼上就先傳到目標那台的 /share/，開單時把連結帶進 `attachments`。
// 傳完之後目標換到別台（換了秘書），開單前在那台再傳一次（附件存在每台自己的 /share/，跟快速輸入一樣）。

export interface FileDraft {
  id: number;
  file: File;
  image: boolean;
  /** 圖片的本機預覽（blob 網址）。 */
  preview?: string;
  /** 傳好了：那台的 `/share/<名字>` 與是哪台（site key）。 */
  url?: string;
  site?: string;
  error?: string;
}

function errorText(err: unknown): string {
  return err instanceof ApiError || err instanceof Error ? err.message : String(err);
}

function previewOf(file: File, image: boolean): string | undefined {
  if (!image || typeof URL.createObjectURL !== "function") return undefined;
  try {
    return URL.createObjectURL(file);
  } catch {
    return undefined;
  }
}

function revoke(preview: string | undefined) {
  if (preview && typeof URL.revokeObjectURL === "function") URL.revokeObjectURL(preview);
}

export interface FileDrafts {
  items: FileDraft[];
  /** 還有沒傳完或傳失敗的：開單要等（或拿掉、重試），附件不會被默默漏掉。 */
  blocked: boolean;
  add: (files: File[]) => void;
  remove: (id: number) => void;
  retry: (id: number) => void;
  /** 貼上：剪貼簿有檔案就當附件（不貼進文字），沒有就照常貼字。 */
  onPaste: (e: ClipboardEvent<HTMLElement>) => void;
  /** 開在 site 的單要帶的連結：傳到別台的在 site 再傳一次。 */
  urlsFor: (site: SiteRef) => Promise<string[]>;
  clear: () => void;
}

export function useFileDrafts(site: SiteRef): FileDrafts {
  const [items, setItems] = useState<FileDraft[]>([]);
  const next = useRef(1);
  const alive = useRef(true);
  const previews = useRef<string[]>([]);
  const siteRef = useRef(site);
  siteRef.current = site;

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      for (const p of previews.current) revoke(p);
    };
  }, []);

  const patch = useCallback((id: number, change: Partial<FileDraft>) => {
    if (!alive.current) return;
    setItems((all) => all.map((d) => (d.id === id ? { ...d, ...change } : d)));
  }, []);

  const send = useCallback(
    async (id: number, file: File) => {
      const to = siteRef.current;
      try {
        const res = await upload(file, to.base);
        patch(id, { url: res.url, site: to.key, error: undefined });
      } catch (err) {
        patch(id, { error: errorText(err) });
      }
    },
    [patch],
  );

  const add = useCallback(
    (picked: File[]) => {
      if (picked.length === 0) return;
      const added = picked.map((raw): FileDraft => {
        const file = namedFile(raw);
        const image = file.type.startsWith("image/") || isImageName(file.name);
        const preview = previewOf(file, image);
        if (preview) previews.current.push(preview);
        return { id: next.current++, file, image, preview };
      });
      setItems((all) => [...all, ...added]);
      void (async () => {
        for (const d of added) await send(d.id, d.file);
      })();
    },
    [send],
  );

  const remove = (id: number) =>
    setItems((all) => {
      const gone = all.find((d) => d.id === id);
      if (gone) revoke(gone.preview);
      return all.filter((d) => d.id !== id);
    });

  const retry = (id: number) => {
    const d = items.find((x) => x.id === id);
    if (!d) return;
    patch(id, { error: undefined });
    void send(id, d.file);
  };

  const onPaste = (e: ClipboardEvent<HTMLElement>) => {
    const files = Array.from(e.clipboardData?.files ?? []);
    if (files.length === 0) return;
    e.preventDefault();
    add(files);
  };

  const urlsFor = async (to: SiteRef): Promise<string[]> => {
    const done = items.filter((d) => d.url);
    return Promise.all(
      done.map(async (d) =>
        d.site === to.key ? (d.url as string) : (await upload(d.file, to.base)).url,
      ),
    );
  };

  const clear = () => {
    for (const d of items) revoke(d.preview);
    previews.current = [];
    setItems([]);
  };

  return {
    items,
    blocked: items.some((d) => !d.url),
    add,
    remove,
    retry,
    onPaste,
    urlsFor,
    clear,
  };
}

const chipBase = "inline-flex items-center rounded-full px-2.5 py-1 text-[13px] leading-snug";
// 膠囊視覺上高 26～28，點擊範圍用 ::after 補到 44（FEN-7）。
const hitArea = `${HIT_44} focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus`;

/** 「附件」膠囊：點了開檔案選擇（可以多選）；放在開單的膠囊列。 */
export function AttachChip({
  drafts,
  className,
  testId = "task-new-file",
}: {
  drafts: FileDrafts;
  className?: string;
  /** 檔案選擇框的 data-testid（手機的抽屜裡另一顆用不同的）。 */
  testId?: string;
}) {
  const picker = useRef<HTMLInputElement>(null);
  return (
    <>
      <input
        ref={picker}
        type="file"
        multiple
        hidden
        data-testid={testId}
        onChange={(e) => {
          const picked = Array.from(e.target.files ?? []);
          e.target.value = ""; // 同一個檔案可以再選一次
          drafts.add(picked);
        }}
      />
      <button
        type="button"
        // 唸出來的名字就是畫面上的字（「附件」「附件 2」）；說明放 title，不蓋掉名字（WCAG 2.5.3）。
        title={t("tasks.new.attach.label")}
        onClick={() => picker.current?.click()}
        className={cn(hitArea, chipBase, "gap-1 bg-surface-2 font-medium text-muted", className)}
      >
        <Paperclip className="size-3.5" aria-hidden="true" />
        {drafts.items.length > 0
          ? t("tasks.new.attach.count", { n: drafts.items.length })
          : t("tasks.new.attach")}
      </button>
    </>
  );
}

/** 要附上的檔案：圖片有縮圖、其他是檔案圖示；傳失敗的可以重試，每個都能拿掉。 */
export function FileDraftList({ drafts, className }: { drafts: FileDrafts; className?: string }) {
  if (drafts.items.length === 0) return null;
  return (
    <ul
      data-unsaved=""
      aria-label={t("tasks.new.attach.list")}
      className={cn("m-0 flex list-none flex-wrap gap-2 p-0", className)}
    >
      {drafts.items.map((d) => {
        const name = d.file.name;
        const state = d.error ? "error" : d.url ? "done" : "uploading";
        return (
          <li
            key={d.id}
            data-attachment={state}
            className={cn(
              "flex h-11 max-w-full items-center gap-2 rounded-xl border bg-surface-2 py-1 pr-0.5 pl-1",
              d.error ? "border-danger" : "border-border",
            )}
          >
            {d.preview ? (
              <img src={d.preview} alt="" className="size-9 shrink-0 rounded-lg object-cover" />
            ) : (
              <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-surface">
                <FileText className="size-4 text-muted" strokeWidth={1.8} aria-hidden="true" />
              </span>
            )}
            <span className="flex min-w-0 flex-col text-xs leading-tight">
              <span className="max-w-[160px] truncate text-fg">{name}</span>
              {d.error ? (
                <span role="alert" className="max-w-[160px] truncate text-danger">
                  {t("tasks.new.attach.failed", { reason: d.error })}
                </span>
              ) : null}
            </span>
            {state === "uploading" ? (
              <Spinner label={t("tasks.new.attach.uploading", { name })} className="size-4" />
            ) : null}
            {d.error ? (
              <button
                type="button"
                aria-label={t("tasks.new.attach.retry", { name })}
                onClick={() => drafts.retry(d.id)}
                className="relative flex size-9 shrink-0 items-center justify-center rounded-full text-danger after:absolute after:-inset-1 after:rounded-full focus-visible:outline-2 focus-visible:outline-focus"
              >
                <RotateCw className="size-4" strokeWidth={1.8} aria-hidden="true" />
              </button>
            ) : null}
            <button
              type="button"
              aria-label={t("tasks.new.attach.remove", { name })}
              onClick={() => drafts.remove(d.id)}
              className="relative flex size-9 shrink-0 items-center justify-center rounded-full text-muted after:absolute after:-inset-1 after:rounded-full hover:text-fg focus-visible:outline-2 focus-visible:outline-focus"
            >
              <X className="size-4" strokeWidth={1.8} aria-hidden="true" />
            </button>
          </li>
        );
      })}
    </ul>
  );
}
