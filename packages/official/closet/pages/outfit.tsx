import {
  Button,
  EmptyState,
  LoadFailed,
  MediaViewer,
  PersonaMedia,
  Segmented,
  Spinner,
} from "@evergenie/ui";
import { useEffect, useRef, useState } from "react";
import { canEdit, canEditTab } from "./lib/caps";
import {
  detailTagsOf,
  editTabHref,
  findOutfit,
  idFromSearch,
  lockedHintOf,
  mineOutfitHref,
  type Outcome,
  type Person,
  type PersonOutfit,
  requestOutcome,
  seesAll,
  showMineLink,
  shownOnDetail,
  T,
  unlockButton,
  viewerItems,
  wardrobeHref,
} from "./lib/closet";
import { OutfitImage, RotateSwitch, useLoad, useRotation, useWear, WearControl } from "./lib/parts";
import {
  loadCapabilities,
  loadMineAvailable,
  loadPeople,
  loadRequest,
  loadState,
  reasonOf,
  sendUnlock,
} from "./lib/store";

const card = "flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4";
const POLL_MS = 3000;
const REQUEST_POLL_MS = 500;
const REQUEST_TIMEOUT_MS = 10_000;

// 衣服詳情：大圖（擁有的有 mp4 就播；開發家沒兌換的也看得到真圖，CLO-27）、名稱說明價格、輪播照片縮圖、輪流穿開關、用點數兌換（按一次展開確認，不用 confirm()）。
// 兌換寫一筆 closet.request，由套件的程式驗證處理（只認主人本人）；頁面的按鈕狀態只是先提示，判斷以程式為準（REQ-125）。
export default function Outfit({ search }: { pkg: string; page: string; search: string }) {
  const id = idFromSearch(search);
  const people = useLoad(loadPeople, POLL_MS);
  const state = useLoad(loadState, POLL_MS);
  const raw = people.data ? findOutfit(people.data, id) : undefined;
  // 「到角色頁管理這套」只在能修改衣服的家（edit，開發家）、closet-mine 能用時出現（CLO-27、CLO-37）；讀不到就不出現。
  // 能力跟著輪詢：程式這一輪才寫好（或開發家剛換）時不用重新整理就跟上（CLO-37）。
  const caps = useLoad(loadCapabilities, POLL_MS);
  const mine = useLoad(loadMineAvailable, POLL_MS * 10);
  // 設計中的衣服只有能修改衣服的家看得到（CLO-38）：一般家當找不到；能力還沒讀到先等，不閃一下「找不到」。
  const capsPending = raw?.outfit.state === "draft" && caps.data === undefined && !caps.error;
  const found =
    raw && !capsPending && shownOnDetail(raw.outfit, canEdit(caps.data)) ? raw : undefined;

  return (
    <section data-testid="closet-outfit" className="flex flex-col gap-4">
      {people.error ? (
        <LoadFailed
          what={T.loadWhat}
          auto
          onRetry={people.reload}
          variant={people.data === undefined ? "block" : "inline"}
          className={people.data === undefined ? "py-8" : undefined}
        />
      ) : null}
      {!id ? (
        <EmptyState tone="missing" title={T.notFound} hint={T.notFoundHint} />
      ) : (people.data === undefined && !people.error) || capsPending ? (
        <div className="flex justify-center py-8">
          <Spinner label={T.loading} />
        </div>
      ) : !found ? (
        people.data ? (
          <EmptyState tone="missing" title={T.notFound} hint={T.notFoundHint} />
        ) : null
      ) : (
        <Detail
          key={found.outfit.record}
          person={found.person}
          outfit={found.outfit}
          points={state.data ?? null}
          onChanged={people.reload}
          mineLink={showMineLink(canEdit(caps.data), mine.data, found.outfit)}
          edit={canEdit(caps.data)}
          editTab={canEditTab(caps.data)}
        />
      )}
    </section>
  );
}

function Detail({
  person,
  outfit,
  points,
  onChanged,
  mineLink,
  edit,
  editTab,
}: {
  person: Person;
  outfit: PersonOutfit;
  points: Parameters<typeof unlockButton>[1];
  onChanged: () => void;
  mineLink: boolean;
  /** 能修改衣服的家（開發家）：標籤多「下架」（一般家沒有這個字）。 */
  edit: boolean;
  /** 角色頁有「修改」分頁（edit-tab）：「升成官方」的入口才連過去。 */
  editTab: boolean;
}) {
  const [pick, setPick] = useState(0);
  const [view, setView] = useState<"half" | "full" | "fullHidden">("half");
  const rot = useRotation(person, onChanged);
  const wear = useWear(person, onChanged);
  const photo = outfit.photos[Math.min(pick, Math.max(outfit.photos.length - 1, 0))];
  const owned = outfit.owned;
  // 輪播照與動畫：擁有的才有；開發家沒兌換的也有（程式照樣給真圖，CLO-27）。輪流穿仍然只有擁有的能開。
  const all = seesAll(outfit);
  const lockedHint = lockedHintOf(outfit);
  // 全身照只有擁有的與設計中的衣服有（程式不公開沒擁有的；開發家例外）；沒有就沒有切換。
  // 隱藏版開著時一般版與隱藏版的全身照都可以看（2026-11-13 作者：兩版都顯示，不是二選一）。
  const fullUrl = view === "full" ? outfit.full : view === "fullHidden" ? outfit.fullHidden : "";
  const showFull = fullUrl !== "";
  // 看大圖（FEN-30）：點上面那張大圖在這頁疊一層看，可以左右換；剪影（清單裡沒有）不放大。
  const items = viewerItems(outfit);
  const [zoom, setZoom] = useState<number | null>(null);
  const zoomAt = items.findIndex((it) => it.src === (showFull ? fullUrl : (photo?.url ?? "")));
  const viewOptions: { value: "half" | "full" | "fullHidden"; label: string }[] = [
    { value: "half", label: T.viewHalf },
  ];
  if (outfit.full !== "") viewOptions.push({ value: "full", label: T.viewFull });
  if (outfit.fullHidden !== "")
    viewOptions.push({ value: "fullHidden", label: `${T.viewFull}（${T.levelHidden}）` });

  const hero = showFull ? (
    <img
      data-testid="outfit-full"
      src={fullUrl}
      alt={`${outfit.name} ${T.viewFull}`}
      className="max-h-[70vh] w-full rounded-xl bg-surface-2 object-contain"
    />
  ) : all && photo?.video ? (
    <PersonaMedia
      name={outfit.name}
      src={photo.url}
      video={photo.video}
      scrimHeight={0}
      className="aspect-square w-full rounded-xl"
    />
  ) : (
    <OutfitImage url={photo?.url ?? ""} real={photo?.real ?? false} testId="outfit-big" />
  );

  return (
    <div className="flex flex-col gap-4">
      <div
        data-testid="outfit-hero"
        data-owned={owned ? "true" : "false"}
        className="mx-auto w-full max-w-md"
      >
        {zoomAt >= 0 ? (
          <button
            type="button"
            data-testid="outfit-zoom"
            aria-label={`${T.zoom}：${outfit.name}`}
            onClick={() => setZoom(zoomAt)}
            className="block w-full cursor-zoom-in rounded-xl p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
          >
            {hero}
          </button>
        ) : (
          hero
        )}
        <MediaViewer items={items} index={zoom} onIndex={setZoom} />
      </div>
      {viewOptions.length > 1 ? (
        <Segmented ariaLabel={T.viewGroup} value={view} onChange={setView} options={viewOptions} />
      ) : null}
      <div className={card}>
        <div className="flex flex-col gap-0.5">
          <h2 data-testid="outfit-name" className="text-xl font-semibold">
            {outfit.name}
          </h2>
          <p className="text-sm text-muted">{person.name}</p>
        </div>
        {outfit.desc ? <p className="text-sm">{outfit.desc}</p> : null}
        <p data-testid="outfit-price" className="text-sm">
          {T.price}：{outfit.cost === 0 ? T.free : `${outfit.cost} ${T.pointUnit}`}
        </p>
        <div className="flex flex-wrap gap-1">
          {detailTagsOf(outfit, edit).map((t) => (
            <span key={t} className="rounded-full bg-surface-2 px-2 py-0.5 text-xs text-muted">
              {t}
            </span>
          ))}
        </div>
        {lockedHint ? (
          <p data-testid="outfit-locked-hint" className="text-xs text-muted">
            {lockedHint}
          </p>
        ) : null}
        {all && outfit.photos.length > 1 ? (
          <div className="flex flex-col gap-1.5">
            <h3 className="text-sm font-semibold">{T.photos}</h3>
            <ul data-testid="outfit-thumbs" className="flex flex-wrap gap-2">
              {outfit.photos.map((ph, i) => (
                <li key={ph.url} className="w-16">
                  <button
                    type="button"
                    aria-label={`${T.photos} ${i + 1}`}
                    aria-pressed={i === pick}
                    onClick={() => setPick(i)}
                    className={`block w-full rounded-xl p-0.5 ${i === pick ? "bg-accent" : "bg-transparent"}`}
                  >
                    <OutfitImage url={ph.url} real testId="outfit-thumb" />
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        <UnlockFlow person={person} outfit={outfit} points={points} onChanged={onChanged} />
        {owned ? (
          <>
            {/* 現在穿這套（CLO-30）是主要動作；輪流穿是次要的。只有真的擁有的才有（開發家看得到真圖的不算）。 */}
            <WearControl outfit={outfit} wear={wear} />
            <div className="flex flex-col">
              <RotateSwitch
                on={rot.current.includes(outfit.outfit)}
                disabled={rot.busy}
                label={`${T.rotate} ${outfit.name}`}
                onChange={(on) => rot.toggle(outfit.outfit, on)}
              />
              <p data-testid="rotate-hint" className="text-xs text-muted">
                {T.rotateHint}
              </p>
            </div>
            <a
              href={wardrobeHref(person.persona)}
              data-testid="outfit-to-wardrobe"
              className="inline-flex min-h-11 items-center self-start text-sm font-medium text-accent no-underline"
            >
              {T.toWardrobe}
            </a>
          </>
        ) : null}
        {rot.error ? (
          <p role="alert" className="text-sm text-danger">
            {rot.error}
          </p>
        ) : null}
        {mineLink ? (
          <a
            href={mineOutfitHref(person.persona, outfit.record)}
            data-testid="outfit-to-mine"
            className="inline-flex min-h-11 items-center self-start text-sm font-medium text-accent no-underline"
          >
            {T.toMineOutfit}
          </a>
        ) : null}
        {mineLink && editTab && outfit.promotable ? (
          <a
            href={editTabHref(person.persona)}
            data-testid="outfit-to-edit"
            className="inline-flex min-h-11 items-center self-start text-sm font-medium text-accent no-underline"
          >
            {T.toEditPromote}
          </a>
        ) : null}
      </div>
    </div>
  );
}

function UnlockFlow({
  person,
  outfit,
  points,
  onChanged,
}: {
  person: Person;
  outfit: PersonOutfit;
  points: Parameters<typeof unlockButton>[1];
  onChanged: () => void;
}) {
  const btn = unlockButton(outfit, points);
  const [ask, setAsk] = useState(false);
  const [sending, setSending] = useState(false);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [error, setError] = useState("");
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const send = async () => {
    setSending(true);
    setError("");
    setOutcome({ kind: "waiting" });
    try {
      const reqId = await sendUnlock(person.persona, outfit.outfit);
      const started = Date.now();
      for (;;) {
        await new Promise((r) => setTimeout(r, REQUEST_POLL_MS));
        if (!alive.current) return;
        const timedOut = Date.now() - started >= REQUEST_TIMEOUT_MS;
        const next = requestOutcome(await loadRequest(reqId), timedOut);
        if (next.kind !== "waiting") {
          setOutcome(next);
          if (next.kind === "done") onChanged();
          break;
        }
      }
    } catch (e) {
      setOutcome(null);
      setError(reasonOf(e));
    } finally {
      if (alive.current) {
        setSending(false);
        setAsk(false);
      }
    }
  };

  if (outcome?.kind === "done") {
    // 兌換成功：緊接在下面（擁有的那一段）就是「現在穿這套」與「到衣櫃看」（CLO-30）。
    return (
      <p role="status" data-testid="outfit-result" data-kind="done" className="font-medium text-ok">
        {outcome.text}
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      {btn.show && !ask ? (
        <Button
          data-testid="outfit-unlock"
          variant="primary"
          disabled={!btn.enabled || sending}
          onClick={() => setAsk(true)}
        >
          {T.unlock(outfit.cost)}
        </Button>
      ) : null}
      {btn.hint ? (
        <p data-testid="outfit-unlock-hint" className="text-xs text-muted">
          {btn.hint}
        </p>
      ) : null}
      {ask ? (
        <div
          data-testid="outfit-unlock-confirm"
          className="flex flex-col gap-2 rounded-lg bg-surface-2 p-3"
        >
          <p className="text-sm">{T.unlockAsk(outfit.cost)}</p>
          <div className="flex flex-wrap gap-2">
            <Button
              data-testid="outfit-unlock-yes"
              variant="primary"
              disabled={sending}
              onClick={send}
            >
              {sending ? T.waiting : T.unlockConfirm}
            </Button>
            <Button variant="ghost" disabled={sending} onClick={() => setAsk(false)}>
              {T.cancel}
            </Button>
          </div>
        </div>
      ) : null}
      {outcome && outcome.kind !== "waiting" ? (
        <p
          role="status"
          data-testid="outfit-result"
          data-kind={outcome.kind}
          className={outcome.kind === "rejected" ? "text-sm text-danger" : "text-sm text-muted"}
        >
          {outcome.text}
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}
