// 「修改」分頁的零件（DEV-160 S6，CLO-35）：最上面的篩選、一套一塊（塊頭是整套的狀態與動作）、一張一張的照片卡、
// 評論（寫 closet.note、傳話給生圖秘書）與評論紀錄、外掛動作的鈕。純函式與字在 edit.ts，讀寫在 store.ts。
// 每個按鈕寫完都叫 onChanged 重讀。採用、一般／隱藏、半身／全身都能再按回來；整套上架、下架、放回開發中（自己的衣服）
// 也都能再改回來，沒有刪東西，所以都不另外問確定（FEN-21）。只有「升成官方」收不回：先預演、寫明收不回再確定（promoteParts.tsx）。
import { Button, MediaViewer, Segmented } from "@evergenie/ui";
import { useEffect, useRef, useState } from "react";
import { type ActionTarget, actionRequest, type ClosetAction, statusLabel } from "./caps";
import {
  canDraft,
  clipLagsBehind,
  type EditNote,
  type EditOutfit,
  type EditPhoto,
  type Filters,
  filtersItem,
  fullTaken,
  hasClips,
  noteStateLabel,
  notesOfOutfit,
  notesOfPhoto,
  noteTime,
  type OutfitBlock,
  photoLabel,
  publishBlocked,
  type StatusRecords,
  stateAction,
  stateLabel,
  statusOf,
  T,
  TYPE_LABEL,
} from "./edit";
import { PromoteOutfit } from "./promoteParts";
import {
  type ArtStatus,
  artMissing,
  loadArtStatus,
  type PieceNoteResult,
  reasonOf,
  sendAction,
  sendEditComment,
  setOutfitState,
  setPhotoAdopted,
  setPhotoHidden,
  setPhotoType,
} from "./store";

/** 沒有生圖秘書時連去看的地方（設定 → 職位與秘書）；開發家才有這頁，不給「加一位」的捷徑。 */
const AGENTS_HREF = "/settings/agents";

const field =
  "w-full rounded-lg border border-border bg-surface p-2 text-sm font-normal text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";
const chip = "rounded-full px-2 py-0.5 text-xs font-medium";
const toggle = (on: boolean) =>
  `inline-flex min-h-11 shrink-0 items-center whitespace-nowrap rounded-full border px-3 text-sm font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus ${
    on ? "border-accent bg-accent-soft text-accent" : "border-border bg-surface text-fg"
  }`;

// ---- 篩選 ----

/**
 * 頁面最上面：選角色（這一頁的角色｜所有角色）＋一排切換鈕（官方、開發中、顯示隱藏版，再接外掛登記的篩選鈕），
 * 每顆各自開關、預設都關；手機上一排放不下就橫向捲，不折成兩行。
 */
export function FilterBar({
  personaName,
  filters,
  actions,
  showHidden,
  onChange,
}: {
  personaName: string;
  filters: Filters;
  actions: ClosetAction[];
  /** 有列得出來的隱藏版照片（hasListableHidden）：沒有（隱藏版開關關著）就不出「顯示隱藏版」（S3a）。 */
  showHidden: boolean;
  onChange: (item: string) => void;
}) {
  const set = (next: Partial<Filters>) => onChange(filtersItem({ ...filters, ...next }));
  const flip = (id: string) =>
    set({
      actions: filters.actions.includes(id)
        ? filters.actions.filter((x) => x !== id)
        : [...filters.actions, id],
    });
  const toggles: { key: string; label: string; on: boolean; flip: () => void }[] = [
    {
      key: "official",
      label: T.official,
      on: filters.official,
      flip: () => set({ official: !filters.official }),
    },
    { key: "wip", label: T.wip, on: filters.wip, flip: () => set({ wip: !filters.wip }) },
    ...(showHidden
      ? [
          {
            key: "hidden",
            label: T.showHidden,
            on: filters.hidden,
            flip: () => set({ hidden: !filters.hidden }),
          },
        ]
      : []),
    ...actions.map((a) => ({
      key: `a:${a.id}`,
      label: a.filter?.label ?? a.label,
      on: filters.actions.includes(a.id),
      flip: () => flip(a.id),
    })),
  ];
  // 一排放不下時右邊淡出，提示還有鈕（跟 Segmented 的捲動模式同一個做法，WEB-113）；外掛篩選晚一點才讀到、字變了要重算。
  const row = useRef<HTMLFieldSetElement>(null);
  const [more, setMore] = useState(false);
  const sig = toggles.map((t) => t.label).join("|");
  useEffect(() => {
    const el = row.current;
    if (!el) return undefined;
    const update = () => setMore(el.scrollLeft + el.clientWidth < el.scrollWidth - 2);
    update();
    el.addEventListener("scroll", update, { passive: true });
    window.addEventListener("resize", update);
    return () => {
      el.removeEventListener("scroll", update);
      window.removeEventListener("resize", update);
    };
  }, [sig]);
  return (
    <div data-testid="edit-filters" className="flex min-w-0 flex-col gap-2">
      <Segmented
        ariaLabel={T.scopeGroup}
        value={filters.all ? "all" : "one"}
        onChange={(v) => set({ all: v === "all" })}
        options={[
          { value: "one", label: personaName },
          { value: "all", label: T.allPersonas },
        ]}
        className="self-start"
      />
      <fieldset
        ref={row}
        aria-label={T.filterGroup}
        data-testid="edit-filter-row"
        data-more={more ? "" : undefined}
        className={`hg-scroll -mx-1 m-0 flex min-w-0 gap-2 overflow-x-auto border-0 px-1 py-1 ${
          more ? "[mask-image:linear-gradient(to_right,#000_calc(100%-40px),transparent)]" : ""
        }`}
      >
        {toggles.map((t) => (
          <button
            key={t.key}
            type="button"
            data-testid="edit-filter"
            data-filter={t.key}
            aria-pressed={t.on}
            onClick={t.flip}
            className={toggle(t.on)}
          >
            {t.label}
          </button>
        ))}
      </fieldset>
    </div>
  );
}

// ---- 評論 ----

function NoArtHint({ status }: { status: "off" | "none" }) {
  return (
    <p
      data-testid="edit-no-art"
      data-art={status}
      className="rounded-lg bg-surface-2 p-2 text-xs text-fg"
    >
      {T.noArt}
      {T.noArtKept}{" "}
      <a
        href={AGENTS_HREF}
        className="inline-flex min-h-11 items-center font-medium text-fg underline"
      >
        {T.noArtLink}
      </a>
    </p>
  );
}

interface Commenting {
  open: boolean;
  text: string;
  busy: boolean;
  msg: string;
  art: ArtStatus;
  setText: (s: string) => void;
  start: () => void;
  cancel: () => void;
  submit: () => Promise<void>;
}

/** 一個「評論」：打開時先看名冊（沒有生圖秘書馬上說明，送出時會再看一次），送出後收起、重讀。 */
function useCommenting(
  send: (text: string) => Promise<PieceNoteResult>,
  onChanged: () => void,
): Commenting {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [art, setArtState] = useState<ArtStatus>("unknown");
  // 每次改 art 都換一個號碼；晚到的查詢號碼對不上就丟掉，不蓋掉比較新的結果。
  const artSeq = useRef(0);
  const setArt = (s: ArtStatus) => {
    artSeq.current++;
    setArtState(s);
  };
  return {
    open,
    text,
    busy,
    msg,
    art,
    setText,
    start: () => {
      setOpen(true);
      setMsg("");
      const seq = ++artSeq.current;
      void loadArtStatus().then((s) => {
        if (artSeq.current === seq) setArtState(s);
      });
    },
    cancel: () => {
      setOpen(false);
      setArt("unknown");
    },
    submit: async () => {
      setBusy(true);
      setMsg("");
      try {
        const r = await send(text);
        setText("");
        setOpen(false);
        setArt(r.art);
        setMsg(r.warn ?? "");
        onChanged();
      } catch (e) {
        setMsg(reasonOf(e));
      } finally {
        setBusy(false);
      }
    },
  };
}

function CommentEditor({ c, testId }: { c: Commenting; testId: string }) {
  return (
    <>
      {c.open ? (
        <div data-testid={`${testId}-editor`} className="flex flex-col gap-1.5">
          <label className="flex flex-col gap-1 text-[13px] font-semibold">
            {T.commentLabel}
            <textarea
              data-testid={`${testId}-text`}
              rows={3}
              value={c.text}
              placeholder={T.commentPlaceholder}
              onChange={(e) => c.setText(e.target.value)}
              className={field}
            />
          </label>
          <div className="flex flex-wrap gap-2">
            <Button
              data-testid={`${testId}-send`}
              variant="primary"
              disabled={c.busy || c.text.trim() === ""}
              onClick={() => void c.submit()}
            >
              {T.commentSend}
            </Button>
            <Button variant="ghost" disabled={c.busy} onClick={c.cancel}>
              {T.commentCancel}
            </Button>
          </div>
        </div>
      ) : null}
      {artMissing(c.art) ? <NoArtHint status={c.art} /> : null}
      {c.msg ? (
        <p role="alert" className="text-xs text-danger">
          {c.msg}
        </p>
      ) : null}
    </>
  );
}

/** 評論紀錄（新的在前）：時間、內容、處理狀態、秘書回填的結果。沒有就不畫。 */
function NoteList({ notes, testId }: { notes: EditNote[]; testId: string }) {
  if (notes.length === 0) return null;
  return (
    <details data-testid={testId} className="text-xs" open={notes.some((n) => n.state === "open")}>
      <summary className="inline-flex min-h-11 cursor-pointer items-center font-medium text-muted">
        {T.notesTitle(notes.length)}
      </summary>
      <ul className="m-0 flex list-none flex-col gap-2 p-0">
        {notes.map((n) => (
          <li
            key={n.id}
            data-testid="edit-note"
            data-note={n.id}
            data-state={n.state}
            className="flex flex-col gap-0.5 rounded-lg bg-surface-2 p-2"
          >
            <span className="flex flex-wrap items-center gap-1.5 text-muted">
              <span>{noteTime(n.at)}</span>
              <span
                className={`${chip} ${n.state === "open" ? "bg-accent-soft text-accent" : "bg-surface text-muted"}`}
              >
                {noteStateLabel(n.state)}
              </span>
            </span>
            <span className="break-words text-fg">{n.comment}</span>
            {n.result ? (
              <span data-testid="edit-note-result" className="break-words text-muted">
                {T.noteResult}
                {n.result}
              </span>
            ) : null}
          </li>
        ))}
      </ul>
    </details>
  );
}

// ---- 外掛動作 ----

/** 一個登記的動作：一顆鈕（照 actionRequest 寫請求），旁邊是狀態的白話（有登記狀態、而且有值才顯示）。 */
function ActionButton({
  action,
  target,
  statuses,
  onChanged,
}: {
  action: ClosetAction;
  target: ActionTarget;
  statuses: StatusRecords;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const status = statusLabel(action, statusOf(action, statuses, target));
  // 缺了要帶的欄位（例：這套還沒有編號）就不給按（caps.ts 的約定）。
  const ready = actionRequest(action, target) !== null;
  const run = async () => {
    setBusy(true);
    setMsg("");
    try {
      await sendAction(action, target);
      onChanged();
    } catch (e) {
      setMsg(T.actionFailed + reasonOf(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      <Button
        data-testid="edit-action"
        data-action={action.id}
        disabled={busy || !ready}
        title={ready ? undefined : T.actionMissing}
        onClick={() => void run()}
      >
        {action.label}
      </Button>
      {status ? (
        <span
          data-testid="edit-action-status"
          data-action={action.id}
          className={`${chip} bg-surface-2 text-fg`}
        >
          {status}
        </span>
      ) : null}
      {msg ? (
        <span role="alert" className="text-xs text-danger">
          {msg}
        </span>
      ) : null}
    </span>
  );
}

// ---- 一張照片 ----

/** 縮圖：點了在這頁看大圖（共用的 MediaViewer，FEN-30）；對不到網址時是佔位（檔名＋類型），不能點。 */
function Thumb({ photo, url, onZoom }: { photo: EditPhoto; url: string; onZoom: () => void }) {
  const box = "aspect-square w-28 shrink-0 overflow-hidden rounded-xl bg-surface-2 sm:w-32";
  if (!url) {
    return (
      <div
        data-testid="edit-photo-placeholder"
        title={T.noUrl}
        className={`${box} flex flex-col items-center justify-center gap-1 p-2 text-center text-xs text-muted`}
      >
        <span className="font-medium text-fg">{TYPE_LABEL[photo.type]}</span>
        <span className="line-clamp-3 break-all">{photo.file || photo.id}</span>
      </div>
    );
  }
  return (
    <button
      type="button"
      data-testid="edit-photo-zoom"
      aria-label={`${T.zoom}：${photoLabel(photo)}`}
      onClick={onZoom}
      className={`${box} relative block cursor-zoom-in p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus`}
    >
      {photo.type === "video" ? (
        <video
          data-testid="edit-photo-video"
          src={url}
          muted
          playsInline
          preload="metadata"
          className="size-full object-cover object-top"
        />
      ) : (
        <img
          data-testid="edit-photo-img"
          src={url}
          alt={photoLabel(photo)}
          loading="lazy"
          className={`size-full ${photo.type === "full" ? "object-contain" : "object-cover object-top"}`}
        />
      )}
      {photo.type === "video" ? (
        <span
          aria-hidden="true"
          className="absolute right-1 bottom-1 rounded-full bg-surface px-1.5 text-xs text-fg"
        >
          ▶
        </span>
      ) : null}
    </button>
  );
}

function PhotoCard({
  person,
  outfit,
  photo,
  url,
  notes,
  paired,
  ownerHidden,
  siblings,
  actions,
  statuses,
  onZoom,
  onChanged,
}: {
  person: { persona: string; name: string };
  outfit: EditOutfit;
  photo: EditPhoto;
  url: string;
  notes: EditNote[];
  /** 這張半身配了影片：不能改成全身（影片只配半身照）。 */
  paired: boolean;
  /** 自己的隱藏版（closet.person 的 owner_hidden）：不給改成一般版。 */
  ownerHidden: boolean;
  /** 同一套的全部照片（判斷那一版是不是已經有全身照，fullTaken）。 */
  siblings: readonly EditPhoto[];
  actions: ClosetAction[];
  statuses: StatusRecords;
  onZoom: () => void;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const c = useCommenting((text) => sendEditComment(person, outfit, photo, text), onChanged);
  const run = async (work: () => Promise<void>) => {
    setBusy(true);
    setErr("");
    try {
      await work();
      onChanged();
    } catch (e) {
      setErr(T.failed + reasonOf(e));
    } finally {
      setBusy(false);
    }
  };
  const target = { persona: person.persona, outfit: outfit.record, photo: photo.id };
  const lagsBehind = clipLagsBehind(siblings, photo);
  return (
    <li
      data-testid="edit-photo"
      data-photo={photo.id}
      data-type={photo.type}
      data-hidden={photo.hidden ? "true" : "false"}
      data-adopted={photo.adopted ? "true" : "false"}
      className="flex min-w-0 flex-col gap-2 rounded-xl border border-border p-3"
    >
      <div className="flex min-w-0 gap-3">
        <Thumb photo={photo} url={url} onZoom={onZoom} />
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <span className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
            {photoLabel(photo)}
            <span
              data-testid="edit-photo-state"
              className={`${chip} ${photo.adopted ? "bg-surface-2 text-muted" : "bg-accent-soft text-accent"}`}
            >
              {photo.adopted ? T.adopted : T.notAdopted}
            </span>
          </span>
          {photo.type === "video" && lagsBehind ? (
            // 上次改一般／隱藏寫到一半、影片沒跟上：給一顆「跟上半身」（用那張半身現在的版本再寫一次，只改影片）。
            <span className="flex flex-wrap items-center gap-2 text-xs text-muted">
              <span data-testid="edit-photo-level-lags">
                {photo.hidden ? T.levelHidden : T.levelStd}・{T.clipLags}
              </span>
              <Button
                data-testid="edit-photo-level-sync"
                disabled={busy}
                onClick={() => void run(() => setPhotoHidden(lagsBehind.id, lagsBehind.hidden))}
              >
                {T.clipSync}
              </Button>
            </span>
          ) : photo.type === "video" ? (
            // 影片只跟同一版的半身照一起公開：一般／隱藏跟著配的那張改（hiddenPatch），這裡只寫是哪一版。
            <span data-testid="edit-photo-level-follows" className="text-xs text-muted">
              {photo.hidden ? T.levelHidden : T.levelStd}・{T.levelFollows}
            </span>
          ) : photo.hidden && ownerHidden ? (
            // 自己的隱藏版改成一般版就不再跟著「自己的隱藏版」開關（CLO-18）：不給改，寫一行說明。
            <span data-testid="edit-photo-owner-hidden" className="text-xs text-muted">
              {T.levelHidden}・{T.ownerHiddenShort}
            </span>
          ) : photo.type === "full" && fullTaken(siblings, photo, !photo.hidden) ? (
            // 另一版已經有全身照：一版只公開一張，改過去就永遠是佔位。
            <span data-testid="edit-photo-level-full-taken" className="text-xs text-muted">
              {photo.hidden ? T.levelHidden : T.levelStd}・{T.otherFullTakenShort}
            </span>
          ) : (
            <>
              <Segmented
                ariaLabel={T.levelGroup}
                value={photo.hidden ? "hidden" : "std"}
                onChange={(v) => {
                  if ((v === "hidden") !== photo.hidden && !busy)
                    void run(() => setPhotoHidden(photo.id, v === "hidden"));
                }}
                options={[
                  { value: "std", label: T.levelStd },
                  { value: "hidden", label: T.levelHidden },
                ]}
                className="self-start"
              />
              {photo.source === "mine" && !photo.hidden ? (
                // 自己的照片改成隱藏版就是自己的隱藏版（CLO-18），這頁不給改回：先說。
                <span data-testid="edit-photo-owner-hint" className="text-xs text-muted">
                  {T.ownerToHiddenHint}
                </span>
              ) : null}
            </>
          )}
          {photo.type === "video" ? (
            <span className="text-xs text-muted">{T.video}</span>
          ) : photo.type === "half" && paired ? (
            <span data-testid="edit-photo-paired" className="text-xs text-muted">
              {T.half}・{T.videoPaired}
            </span>
          ) : photo.type === "half" && fullTaken(siblings, photo, photo.hidden) ? (
            <span data-testid="edit-photo-full-taken" className="text-xs text-muted">
              {T.half}・{T.fullTakenShort}
            </span>
          ) : (
            <Segmented
              ariaLabel={T.typeGroup}
              value={photo.type}
              onChange={(v) => {
                if (v !== photo.type && !busy) void run(() => setPhotoType(photo.id, v));
              }}
              options={[
                { value: "half" as const, label: T.half },
                { value: "full" as const, label: T.full },
              ]}
              className="self-start"
            />
          )}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          data-testid="edit-photo-adopt"
          variant={photo.adopted ? "secondary" : "primary"}
          disabled={busy}
          onClick={() => void run(() => setPhotoAdopted(photo.id, !photo.adopted, photo))}
        >
          {photo.adopted ? T.unadopt : T.adopt}
        </Button>
        <Button data-testid="edit-photo-comment" disabled={busy || c.open} onClick={c.start}>
          {T.comment}
        </Button>
        {actions.map((a) => (
          <ActionButton
            key={a.id}
            action={a}
            target={target}
            statuses={statuses}
            onChanged={onChanged}
          />
        ))}
      </div>
      <CommentEditor c={c} testId="edit-photo-comment" />
      {err ? (
        <p role="alert" className="text-xs text-danger">
          {err}
        </p>
      ) : null}
      <NoteList notes={notes} testId="edit-photo-notes" />
    </li>
  );
}

// ---- 一套 ----

/**
 * 一套一塊：塊頭是名稱、整套的狀態（開發中／上架中／已下架、官方／我的、採用了幾張）與動作（上架／下架、放回開發中、評論、
 * 外掛動作），「我的」可以升時接著升成官方的流程（預演→確認→送，DEV-160 審查 S1 從 closet-mine 的「衣服」分頁搬來），
 * 整套的評論紀錄；下面一張一張照片卡。
 */
export function OutfitCard({
  person,
  block,
  photoUrl,
  ownerHidden,
  promotable,
  notes,
  outfitActions,
  photoActions,
  statuses,
  heading,
  onChanged,
  onPromoted,
}: {
  person: { persona: string; name: string };
  block: OutfitBlock;
  photoUrl: (p: EditPhoto) => string;
  /** 這張是不是自己的隱藏版（isOwnerHidden）。 */
  ownerHidden: (p: EditPhoto) => boolean;
  /** 可以升成官方（衣櫃算好的 promotable，而且 closet-mine 開著）：塊裡出現「升成官方」。 */
  promotable: boolean;
  notes: readonly EditNote[];
  outfitActions: ClosetAction[];
  photoActions: ClosetAction[];
  statuses: StatusRecords;
  heading: "h3" | "h4";
  onChanged: () => void;
  /** 升成官方正式送出的結果（給整個分頁最上面顯示：升了之後這一塊會換成官方的那筆）。 */
  onPromoted: (text: string, ok: boolean) => void;
}) {
  const { outfit: o, photos, all } = block;
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [zoom, setZoom] = useState<number | null>(null);
  const c = useCommenting((text) => sendEditComment(person, o, null, text), onChanged);
  const left = publishBlocked(o, all);
  const next = stateAction(o.state);
  const urls = photos.map(photoUrl);
  const viewable = photos.flatMap((p, i) => (urls[i] ? [{ p, url: urls[i] as string }] : []));
  const items = viewable.map(({ p, url }) => ({
    src: url,
    name: `${o.name} ${photoLabel(p)}`,
    type: p.type === "video" ? ("video" as const) : ("image" as const),
  }));
  const Heading = heading;
  const done = all.filter((p) => p.adopted).length;
  const changeState = async (to: "active" | "retired" | "draft") => {
    setBusy(true);
    setErr("");
    try {
      await setOutfitState(o.record, to);
      onChanged();
    } catch (e) {
      setErr(T.failed + reasonOf(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section
      data-testid="edit-outfit"
      data-record={o.record}
      data-state={o.state}
      data-source={o.source}
      className="flex min-w-0 flex-col gap-3 rounded-[18px] border border-border bg-surface p-4"
    >
      <div className="flex min-w-0 flex-col gap-2">
        <Heading className="m-0 break-words text-base font-semibold">{o.name}</Heading>
        <span className="flex flex-wrap items-center gap-1.5">
          <span
            data-testid="edit-outfit-state"
            className={`${chip} ${o.state === "draft" ? "bg-accent-soft text-accent" : "bg-surface-2 text-fg"}`}
          >
            {stateLabel(o.state)}
          </span>
          <span data-testid="edit-outfit-source" className={`${chip} bg-surface-2 text-fg`}>
            {o.source === "mine" ? T.sourceMine : T.sourceOfficial}
          </span>
          {all.length > 0 ? (
            <span className="text-xs text-muted">{T.adoptedCount(done, all.length)}</span>
          ) : null}
        </span>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            data-testid="edit-outfit-state-toggle"
            data-to={next.to}
            variant={next.to === "active" ? "primary" : "secondary"}
            disabled={busy || left > 0}
            onClick={() => void changeState(next.to)}
          >
            {next.label}
          </Button>
          {canDraft(o) ? (
            <Button
              data-testid="edit-outfit-to-draft"
              variant="secondary"
              disabled={busy}
              onClick={() => void changeState("draft")}
            >
              {T.toDraft}
            </Button>
          ) : null}
          <Button data-testid="edit-outfit-comment" disabled={busy || c.open} onClick={c.start}>
            {T.comment}
          </Button>
          {outfitActions.map((a) => (
            <ActionButton
              key={a.id}
              action={a}
              target={{ persona: o.persona, outfit: o.record }}
              statuses={statuses}
              onChanged={onChanged}
            />
          ))}
        </div>
        {left > 0 ? (
          <p data-testid="edit-outfit-publish-need" className="text-xs text-muted">
            {T.publishNeed(left)}
            {block.offLeft > 0 ? T.publishNeedOff(block.offLeft) : ""}
          </p>
        ) : null}
        {err ? (
          <p role="alert" className="text-xs text-danger">
            {err}
          </p>
        ) : null}
        {o.source === "mine" && promotable ? (
          <PromoteOutfit key={o.record} record={o.record} outfit={o.outfit} onDone={onPromoted} />
        ) : null}
        <CommentEditor c={c} testId="edit-outfit-comment" />
        <NoteList notes={notesOfOutfit(notes, o.record)} testId="edit-outfit-notes" />
      </div>
      {all.length === 0 ? <p className="text-sm text-muted">{T.outfitNoPhotos}</p> : null}
      {photos.length > 0 ? (
        <ul className="m-0 grid list-none grid-cols-1 gap-3 p-0 md:grid-cols-2">
          {photos.map((p, i) => (
            <PhotoCard
              key={p.id}
              person={person}
              outfit={o}
              photo={p}
              url={urls[i] ?? ""}
              notes={notesOfPhoto(notes, p)}
              paired={hasClips(block.all, p)}
              ownerHidden={ownerHidden(p)}
              siblings={block.all}
              actions={photoActions}
              statuses={statuses}
              onZoom={() => setZoom(viewable.findIndex((v) => v.p.id === p.id))}
              onChanged={onChanged}
            />
          ))}
        </ul>
      ) : null}
      {block.hiddenOut > 0 ? (
        <p data-testid="edit-outfit-hidden-out" className="text-xs text-muted">
          {T.hiddenOut(block.hiddenOut)}
        </p>
      ) : null}
      <MediaViewer items={items} index={zoom} onIndex={setZoom} />
    </section>
  );
}
