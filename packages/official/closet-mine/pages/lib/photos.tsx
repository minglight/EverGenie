// 衣服分頁一套的每張照片、「刪掉的」（DEV-160 S5c，CMI-24）：照衣櫃的照片紀錄（closet.photo）畫，每張可以換、刪，
// 半身照可以加動畫；刪掉的（整套或單張）列在總覽最下面，可以拿回來。頁面只寫請求（CMI-N4），做不做得到由程式判斷，拒絕的原因照實顯示。
import { Button, MediaViewer } from "@evergenie/ui";
import { type ChangeEvent, useEffect, useRef, useState } from "react";
import { checkFile, type DeletedItem, MAX_CLIPS, type PhotoGroup, type PhotoItem, T } from "./mine";
import { card, ResultLine, type Sender, Thumb } from "./parts";

const ACCEPT_IMAGE = "image/png,image/jpeg,image/webp";

/**
 * 按「刪掉」之後的確認（不用 confirm()，FEN-N6）：說要刪什麼，兩顆鈕。出現時焦點移到「不要」（取代了原本的鈕，焦點不能掉；
 * 預設在不會刪東西的那顆）；送出中「刪」那顆寫「處理中…」（FEN-20）。
 */
export function ConfirmLine({
  text,
  yes,
  busy,
  onYes,
  onNo,
  testId,
}: {
  text: string;
  yes: string;
  busy: boolean;
  onYes: () => void;
  onNo: () => void;
  testId: string;
}) {
  const box = useRef<HTMLFieldSetElement>(null);
  useEffect(() => {
    box.current?.querySelector<HTMLButtonElement>("[data-confirm-no]")?.focus();
  }, []);
  return (
    <fieldset
      ref={box}
      data-testid={testId}
      className="m-0 flex min-w-0 flex-col gap-2 rounded-xl border border-border bg-surface p-3"
    >
      <legend className="float-left mb-2 w-full p-0 text-sm">{text}</legend>
      <div className="clear-left flex flex-wrap gap-2">
        <Button variant="danger" disabled={busy} onClick={onYes} data-testid={`${testId}-yes`}>
          {busy ? T.sending : yes}
        </Button>
        <Button data-confirm-no variant="secondary" disabled={busy} onClick={onNo}>
          {T.removeNo}
        </Button>
      </div>
    </fieldset>
  );
}

/** 一套要送的照片請求共同的部分（哪位、哪套、隱藏版與否）。 */
export interface PhotoTarget {
  persona: string;
  /** 衣服 id。 */
  outfit: string;
  /** 「放進自己的隱藏版」有畫才有值（CMI-13）：每張照片照它自己是哪一版送；沒畫就不送。 */
  adultShown: boolean;
}

/**
 * 一套的照片（一版一組）：半身照一張一列（縮圖、第幾張、換一張、加動畫、刪掉），它的動畫接在底下，最後是全身照。
 * 點縮圖看大圖（FEN-30）。只有我的照片有鈕（官方的不能換、刪）。
 */
export function PhotoList({
  record,
  name,
  groups,
  target,
  sender,
  locked,
}: {
  record: string;
  name: string;
  groups: PhotoGroup[];
  target: PhotoTarget;
  sender: Sender;
  /** 還沒兌換（只看得到剪影）：縮圖沒有圖，說一句。 */
  locked: boolean;
}) {
  const [zoom, setZoom] = useState<number | null>(null);
  const items = groups.flatMap((g) => [
    ...g.halves.flatMap((h) => [h, ...h.clips]),
    ...(g.full ? [g.full] : []),
  ]);
  const shots = items.filter((p) => p.url !== "");
  const open = (p: PhotoItem) => {
    const i = shots.findIndex((s) => s.id === p.id);
    if (i >= 0) setZoom(i);
  };
  const empty = groups.every((g) => g.halves.length === 0 && !g.full);
  // 刪掉一張做完那一列就不見了：結果改顯示在「照片」標題底下（listAt）。
  const listAt = `photos:${record}`;
  return (
    <section data-testid={`mine-photos-${record}`} className="flex flex-col gap-2">
      <h3 className="text-sm font-semibold">{T.photosTitle}</h3>
      <ResultLine sender={sender} at={listAt} />
      <MediaViewer
        items={shots.map((p) => ({
          src: p.url,
          name: `${name} ${p.label}`,
          type: p.type === "video" ? "video" : "image",
        }))}
        index={zoom}
        onIndex={setZoom}
      />
      {locked ? <p className="text-xs text-muted">{T.photosLocked}</p> : null}
      {empty ? <p className="text-sm text-muted">{T.photosNone}</p> : null}
      {groups.map((g) => (
        <div
          key={g.hidden ? "h" : "std"}
          data-testid={`mine-photo-group-${g.hidden ? "hidden" : "std"}`}
          className="flex flex-col gap-1.5"
        >
          {g.hidden ? (
            <h4 className="text-xs font-semibold text-muted">{T.photoHiddenTitle}</h4>
          ) : null}
          <ul className="flex flex-col gap-1.5">
            {g.halves.map((h) => (
              <PhotoRow
                key={h.id}
                item={h}
                target={target}
                sender={sender}
                onOpen={open}
                listAt={listAt}
              />
            ))}
            {g.full ? (
              <PhotoRow
                item={g.full}
                target={target}
                sender={sender}
                onOpen={open}
                listAt={listAt}
              />
            ) : null}
          </ul>
        </div>
      ))}
    </section>
  );
}

function PhotoRow({
  item,
  target,
  sender,
  onOpen,
  listAt,
  clip = false,
}: {
  item: PhotoItem;
  target: PhotoTarget;
  sender: Sender;
  /** 看大圖（點縮圖或 ▶）。 */
  onOpen: (p: PhotoItem) => void;
  /** 這套照片清單的結果行（刪掉做完顯示在那裡）。 */
  listAt: string;
  clip?: boolean;
}) {
  const [asking, setAsking] = useState(false);
  const replaceRef = useRef<HTMLInputElement>(null);
  const clipRef = useRef<HTMLInputElement>(null);
  const isVideo = item.type === "video";
  const adult = target.adultShown ? item.hidden : undefined;
  const reqType = item.type === "half" ? "photo" : item.type;
  // 這一列的結果行：換、加動畫的處理中與結果、刪掉的處理中與失敗都在這列（UXC-9）。
  const rowAt = `photo:${item.id}`;

  const pick = async (e: ChangeEvent<HTMLInputElement>, mode: "replace" | "clip") => {
    const file = e.target.files?.[0] ?? null;
    e.target.value = "";
    if (!file) return;
    const video = mode === "clip" || isVideo;
    const bad = checkFile(file, video ? "video" : "image");
    if (bad) {
      sender.fail(bad, rowAt);
      return;
    }
    await sender.send(
      file,
      {
        type: mode === "clip" ? "video" : reqType,
        persona: target.persona,
        outfit: target.outfit,
        photoId: item.id,
        clip: mode === "clip" ? item.clips.length + 1 : undefined,
        adult,
      },
      { at: rowAt },
    );
  };
  const remove = async () => {
    const out = await sender.send(
      null,
      { type: "remove_photo", persona: target.persona, photoId: item.id },
      { doneText: T.doneRemoved, at: rowAt, doneAt: listAt },
    );
    if (out.kind !== "waiting") setAsking(false);
  };

  return (
    <li
      data-testid="mine-photo"
      data-photo={item.id}
      data-type={item.type}
      data-hidden={item.hidden}
      className={`flex flex-col gap-2 rounded-xl bg-surface p-2 ${clip ? "ml-6" : ""}`}
    >
      {/* 縮圖在左；右邊上面是「第幾張」、下面是鈕（手機 390 寬鈕自己換行，不把字擠成一字一行）。 */}
      <div className="flex min-w-0 items-start gap-2">
        {isVideo ? (
          <button
            type="button"
            onClick={() => onOpen(item)}
            disabled={item.url === ""}
            aria-label={`${T.zoom}：${item.label}`}
            className="inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-lg border border-border px-3 text-sm focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50"
          >
            ▶
          </button>
        ) : (
          <button
            type="button"
            data-testid="mine-photo-zoom"
            onClick={() => onOpen(item)}
            disabled={item.url === ""}
            aria-label={`${T.zoom}：${item.label}`}
            className="size-14 shrink-0 cursor-zoom-in rounded-xl p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-default"
          >
            <Thumb url={item.url} testId="mine-photo-thumb" />
          </button>
        )}
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="whitespace-nowrap text-sm font-medium">{item.label}</span>
          {item.mine ? (
            <div className="flex flex-wrap gap-1.5">
              <Button
                variant="secondary"
                disabled={sender.busy}
                onClick={() => replaceRef.current?.click()}
                data-testid="mine-photo-replace"
              >
                {isVideo ? T.replaceVideo : T.replace}
              </Button>
              {item.type === "half" && item.clips.length < MAX_CLIPS ? (
                <Button
                  variant="secondary"
                  title={T.addClipHint}
                  disabled={sender.busy}
                  onClick={() => clipRef.current?.click()}
                  data-testid="mine-photo-add-clip"
                >
                  {T.addClipTo}
                </Button>
              ) : null}
              {item.removable ? (
                <Button
                  variant="ghost"
                  disabled={sender.busy}
                  onClick={() => setAsking(true)}
                  data-testid="mine-photo-remove"
                >
                  {T.removePhoto}
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>
      <ResultLine sender={sender} at={rowAt} />
      {asking ? (
        <ConfirmLine
          testId="mine-photo-confirm"
          text={
            item.clips.length > 0
              ? T.removeAskClips(item.label, item.clips.length)
              : T.removeAsk(item.label)
          }
          yes={T.removeYes}
          busy={sender.busy}
          onYes={() => void remove()}
          onNo={() => setAsking(false)}
        />
      ) : null}
      {item.mine ? (
        <>
          <input
            ref={replaceRef}
            data-testid={`mine-replace-input-${item.id}`}
            type="file"
            accept={isVideo ? "video/mp4" : ACCEPT_IMAGE}
            className="sr-only"
            tabIndex={-1}
            onChange={(e) => void pick(e, "replace")}
          />
          {item.type === "half" ? (
            <input
              ref={clipRef}
              data-testid={`mine-clip-input-${item.id}`}
              type="file"
              accept="video/mp4"
              className="sr-only"
              tabIndex={-1}
              onChange={(e) => void pick(e, "clip")}
            />
          ) : null}
        </>
      ) : null}
      {item.clips.length > 0 ? (
        <ul className="flex flex-col gap-1.5">
          {item.clips.map((c) => (
            <PhotoRow
              key={c.id}
              item={c}
              target={target}
              sender={sender}
              onOpen={onOpen}
              listAt={listAt}
              clip
            />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

/** 「拿回來」的結果行（頁面放在「刪掉的」後面：拿回最後一項那塊就不見了，那行還在）。 */
export const DELETED_AT = "deleted";

/** 總覽最下面的「刪掉的」：整套與單張，一項一列，「拿回來」送 restore_outfit／restore_photo。沒有就不畫。 */
export function DeletedList({
  persona,
  items,
  sender,
}: {
  persona: string;
  items: DeletedItem[];
  sender: Sender;
}) {
  if (items.length === 0) return null;
  const restore = (d: DeletedItem) =>
    void sender.send(
      null,
      d.kind === "outfit"
        ? { type: "restore_outfit", persona, outfit: d.id }
        : { type: "restore_photo", persona, photoId: d.id },
      { doneText: T.doneRestored, at: DELETED_AT },
    );
  return (
    <section data-testid="mine-deleted" className={card}>
      <div>
        <h3 className="text-base font-semibold">{T.deletedTitle}</h3>
        <p className="text-sm text-muted">{T.deletedHint}</p>
      </div>
      <ul className="flex flex-col gap-1.5">
        {items.map((d) => (
          <li
            key={`${d.kind}:${d.id}`}
            data-testid="mine-deleted-item"
            data-kind={d.kind}
            data-id={d.id}
            className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-xl bg-surface-2 px-3 py-1.5"
          >
            <span className="min-w-0 break-words text-sm">{d.label}</span>
            <Button
              variant="secondary"
              disabled={sender.busy}
              onClick={() => restore(d)}
              data-testid="mine-deleted-restore"
            >
              {T.restore}
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}
