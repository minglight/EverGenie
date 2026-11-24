// 衣櫃與展示間共用的小零件：載入用的 hook（可以定時刷新）、衣服的圖（沒有圖時畫灰色人形）、輪流穿開關、
// 衣服格子的一格、角色頭像列（一次看一位，DEV-143）、隱藏版開關。
import { Button, Spinner } from "@evergenie/ui";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import {
  chosenPersona,
  emptyNotice,
  mineHref,
  nextRotation,
  outfitHref,
  PERSONA_KEY,
  type Person,
  type PersonOutfit,
  personaFromSearch,
  T,
  tagsOf,
  undoOf,
  type VariantSwitch,
  type WearAction,
  wearOutcome,
  withPersona,
  wornImage,
} from "./closet";
import {
  loadMineAvailable,
  loadPackStatus,
  loadRequest,
  loadVariantSwitches,
  reasonOf,
  retryPack,
  saveRotation,
  sendWear,
  setVariantOn,
} from "./store";

export interface Loaded<D> {
  data: D | undefined;
  error: string;
  reload: () => void;
}

/** 載一次、之後可以 reload；pollMs > 0 時定時再載一次（程式處理完才會更新 person／state，所以頁面每幾秒看一次）。 */
export function useLoad<D>(fn: () => Promise<D>, pollMs = 0): Loaded<D> {
  const [data, setData] = useState<D>();
  const [error, setError] = useState("");
  const [tick, setTick] = useState(0);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  useEffect(() => {
    let live = true;
    const run = () =>
      fnRef
        .current()
        .then((d) => {
          if (!live) return;
          setError("");
          setData(d);
        })
        .catch((e: unknown) => {
          if (!live) return;
          console.warn("讀取失敗", e);
          setError(reasonOf(e));
        });
    void run();
    const timer = pollMs > 0 ? setInterval(run, pollMs) : undefined;
    return () => {
      live = false;
      if (timer) clearInterval(timer);
    };
  }, [tick, pollMs]);
  // 重讀時先清掉錯誤：畫面回到「載入中」，再失敗才又顯示，不會按了「再試一次」像沒反應。
  const reload = useCallback(() => {
    setError("");
    setTick((n) => n + 1);
  }, []);
  return { data, error, reload };
}

/** 灰色人形（沒有剪影檔、圖載入失敗時用；舊系統同一個退路）。 */
function Figure() {
  return (
    <svg
      viewBox="0 0 100 100"
      aria-hidden="true"
      className="absolute inset-0 size-full text-border-strong"
      fill="currentColor"
    >
      <circle cx="50" cy="34" r="14" />
      <path d="M22 92c0-18 12-30 28-30s28 12 28 30z" />
    </svg>
  );
}

function Lock() {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      className="size-4"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="5" y="11" width="14" height="9" rx="2" />
      <path d="M8 11V8a4 4 0 0 1 8 0v3" />
    </svg>
  );
}

/** 衣服的圖：real = false 是剪影，加鎖頭標記；沒有網址或載入失敗畫灰色人形。 */
export function OutfitImage({
  url,
  real,
  testId = "outfit-img",
  round = false,
}: {
  url: string;
  real: boolean;
  testId?: string;
  /** true：圓形（小頭像）；預設圓角方塊。 */
  round?: boolean;
}) {
  const [failed, setFailed] = useState("");
  const show = url !== "" && failed !== url;
  return (
    <div
      className={`relative aspect-square w-full overflow-hidden bg-surface-2 ${round ? "rounded-full" : "rounded-xl"}`}
      data-real={real ? "true" : "false"}
    >
      {show ? (
        <img
          data-testid={testId}
          src={url}
          alt=""
          loading="lazy"
          onError={() => setFailed(url)}
          className="absolute inset-0 size-full object-cover object-top"
        />
      ) : (
        <>
          <Figure />
          <span data-testid={testId} data-placeholder="true" className="sr-only" />
        </>
      )}
      {real ? null : (
        <span
          data-testid="lock-badge"
          className="absolute right-2 top-2 inline-flex size-7 items-center justify-center rounded-full bg-surface text-fg shadow"
        >
          <Lock />
          <span className="sr-only">{T.tagLocked}</span>
        </span>
      )}
    </div>
  );
}

/** 輪流穿開關（至少 44px 高）。 */
export function RotateSwitch(props: {
  on: boolean;
  disabled?: boolean;
  onChange: (next: boolean) => void;
  label: string;
}) {
  return <Switch {...props} text={T.rotate} testId="rotate-toggle" />;
}

/** 開關（至少 44px 高）：輪流穿、隱藏版共用。 */
export function Switch({
  on,
  disabled,
  onChange,
  label,
  text,
  testId,
}: {
  on: boolean;
  disabled?: boolean;
  onChange: (next: boolean) => void;
  label: string;
  text: string;
  testId: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      data-testid={testId}
      disabled={disabled}
      onClick={() => onChange(!on)}
      className="inline-flex min-h-11 items-center gap-2 rounded-lg px-1 text-sm text-fg disabled:opacity-50"
    >
      <span
        aria-hidden="true"
        className={`relative inline-block h-6 w-10 rounded-full transition-colors ${on ? "bg-accent" : "bg-border-strong"}`}
      >
        <span
          className={`absolute top-0.5 size-5 rounded-full bg-surface transition-all ${on ? "left-[18px]" : "left-0.5"}`}
        />
      </span>
      <span>{text}</span>
    </button>
  );
}

const sameSet = (a: string[], b: string[]) =>
  a.length === b.length && a.every((x) => b.includes(x));

/**
 * 輪流穿的切換：寫完 closet.rotation 之後，套件的程式要過一兩秒才會重算 person，
 * 所以先用「剛存的清單」讓開關立刻有反應（UXC-9），person 追上來（或 10 秒後）就改回讀 person。
 */
export function useRotation(person: Person, onChanged: () => void) {
  const [pending, setPending] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (pending && sameSet(pending, person.rotation)) setPending(null);
  }, [pending, person.rotation]);
  useEffect(() => {
    if (!pending) return;
    const t = setTimeout(() => setPending(null), 10_000);
    return () => clearTimeout(t);
  }, [pending]);
  const current = pending ?? person.rotation;
  const toggle = async (outfit: string, on: boolean) => {
    const next = nextRotation({ ...person, rotation: current }, outfit, on);
    if (next === null) {
      setError(T.lastOne);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await saveRotation(person.persona, next);
      setPending(next);
      onChanged();
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };
  return { current, toggle, busy, error };
}

const WEAR_POLL_MS = 400;
const WEAR_TIMEOUT_MS = 10_000;

/** 「現在穿這套」這一次的結果：哪一套、成功（可復原）／已復原／已送出還沒處理完／沒成功。 */
export interface WearNotice {
  outfit: string;
  kind: "done" | "undone" | "pending" | "failed";
  text: string;
  /** 成功時「復原」要送的請求。 */
  undo?: WearAction;
}

/**
 * 現在穿這套（CLO-30）：送一筆請求、等程式處理完（看到 done 時 person 已經是換好的），再重讀 person。
 * worn 是畫面該當成「現在穿著」的那套：程式剛處理完、頁面還沒重讀到之前先用剛換的那套，免得按鈕閃回來。
 */
export function useWear(person: Person, onChanged: () => void) {
  const [busy, setBusy] = useState("");
  const [expect, setExpect] = useState<string | null>(null);
  const [notice, setNotice] = useState<WearNotice | null>(null);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (expect === null) return;
    if (expect === person.worn) {
      setExpect(null);
      return;
    }
    const t = setTimeout(() => setExpect(null), 10_000);
    return () => clearTimeout(t);
  }, [expect, person.worn]);

  const run = async (
    outfit: string,
    action: WearAction,
    next: (ok: boolean, text: string) => WearNotice,
  ) => {
    if (busy) return;
    setBusy(outfit);
    setNotice(null);
    try {
      const id = await sendWear(person.persona, action);
      const started = Date.now();
      for (;;) {
        await new Promise((r) => setTimeout(r, WEAR_POLL_MS));
        if (!alive.current) return;
        const out = wearOutcome(await loadRequest(id), Date.now() - started >= WEAR_TIMEOUT_MS);
        if (out.kind === "waiting") continue;
        if (!alive.current) return;
        if (out.kind === "done") {
          // 復原（unwear）之後穿哪套由輪流穿決定，等重讀；指定穿的先當成已經穿上。
          setExpect(action.type === "wear" ? action.outfit : null);
          onChanged();
        }
        // 等太久還沒處理完不是失敗：請求還在，之後照樣會換（中性的字，不用紅字）。
        if (out.kind === "timeout") setNotice({ outfit, kind: "pending", text: out.text });
        else setNotice(next(out.kind === "done", out.text));
        break;
      }
    } catch (e) {
      if (alive.current) setNotice({ outfit, kind: "failed", text: reasonOf(e) });
    } finally {
      if (alive.current) setBusy("");
    }
  };

  const wear = (outfit: string) => {
    // 「剛才那套」用送出當下記住的：剛指定穿、頁面還沒重讀到的那套（expect）優先，免得連按兩套時復原回錯。
    const undo = undoOf(expect !== null ? { worn: expect, picked: true } : person, outfit);
    return run(outfit, { type: "wear", outfit }, (ok, text) =>
      ok ? { outfit, kind: "done", text, undo } : { outfit, kind: "failed", text },
    );
  };
  const undo = () => {
    const n = notice;
    if (!n?.undo) return;
    return run(n.outfit, n.undo, (ok, text) =>
      ok
        ? { outfit: n.outfit, kind: "undone", text: T.wearUndone }
        : { outfit: n.outfit, kind: "failed", text },
    );
  };
  const worn = expect ?? person.worn;
  // 現在這套是不是「現在穿這套」指定的（今天都穿這套）：剛換好還沒重讀到的也算。
  const picked = expect !== null || (person.picked && person.worn === worn);
  return { worn, picked, busy, notice, wear, undo };
}

/**
 * 一套擁有的衣服的「現在穿這套」：正在穿的顯示「現在穿著」、不給按鈕；其他是主要按鈕。
 * 這套剛換好時在下面說「換好了　今天都穿這套，明天起照輪流穿的換」，並給「復原」；指定穿的那套寫「現在穿著（今天都穿這套）」。沒擁有的不要放這個元件（程式也會擋）。
 */
export function WearControl({
  outfit: o,
  wear,
}: {
  outfit: PersonOutfit;
  wear: ReturnType<typeof useWear>;
}) {
  const wearing = wear.worn === o.outfit;
  const n = wear.notice?.outfit === o.outfit ? wear.notice : null;
  return (
    <div data-testid="wear-control" className="flex flex-col gap-1.5">
      {wearing ? (
        <p
          data-testid="wearing-now"
          className="inline-flex min-h-11 items-center gap-1 text-sm font-medium text-ok"
        >
          <span aria-hidden="true">✓</span>
          {wear.picked ? T.wearingToday : T.wearingNow}
        </p>
      ) : (
        <Button
          data-testid="wear-now"
          variant="primary"
          className="w-full px-2"
          aria-label={`${T.wearNow}：${o.name}`}
          disabled={wear.busy !== ""}
          onClick={() => void wear.wear(o.outfit)}
        >
          {wear.busy === o.outfit ? T.wearBusy : T.wearNow}
        </Button>
      )}
      {n && n.kind !== "failed" ? (
        <div className="flex flex-col gap-1">
          <p role="status" data-testid="wear-result" data-kind={n.kind} className="text-sm">
            <span className={n.kind === "pending" ? "text-muted" : "font-medium text-ok"}>
              {n.text}
            </span>
            {n.kind === "done" && wearing ? (
              <span className="text-muted">　{T.wearToday}</span>
            ) : null}
          </p>
          {n.kind === "done" && n.undo ? (
            <Button
              data-testid="wear-undo"
              variant="secondary"
              className="self-start px-3"
              disabled={wear.busy !== ""}
              onClick={() => void wear.undo()}
            >
              {wear.busy === o.outfit ? T.wearBusy : T.wearUndo}
            </Button>
          ) : null}
        </div>
      ) : null}
      {n?.kind === "failed" ? (
        <p
          role="alert"
          data-testid="wear-result"
          data-kind="failed"
          className="text-sm text-danger"
        >
          {n.text}
        </p>
      ) : null}
    </div>
  );
}

export const card = "flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4";

/** 衣服格子的一格：圖（沒擁有的是剪影＋鎖頭）、名字、標籤；children 放在最下面（衣櫃放輪流穿開關）。 */
export function OutfitTile({
  outfit: o,
  href,
  owner,
  edit = false,
  children,
}: {
  outfit: PersonOutfit;
  /** 能修改衣服的家（開發家）：下架的多標「下架」（一般家沒有這個字，tagsOf）。 */
  edit?: boolean;
  /** 點了去哪；不給是衣服詳情。 */
  href?: string;
  /** 角色名：跨角色列出時（展示間的篩選）標在名字下面。 */
  owner?: string;
  children?: ReactNode;
}) {
  const photo = o.photos[0];
  return (
    <li
      data-testid="outfit-tile"
      data-outfit={o.record}
      data-owned={o.owned ? "true" : "false"}
      className="flex flex-col gap-1.5"
    >
      <a
        href={href ?? outfitHref(o.record)}
        className="flex min-w-0 flex-col gap-1.5 text-fg no-underline"
      >
        <OutfitImage url={photo?.url ?? ""} real={photo?.real ?? false} />
        <span className="break-words font-medium">{o.name}</span>
        {owner ? (
          <span data-testid="outfit-owner" className="break-words text-xs text-muted">
            {owner}
          </span>
        ) : null}
      </a>
      <div data-testid="outfit-tags" className="flex flex-wrap gap-1">
        {tagsOf(o, edit).map((t) => (
          <span key={t} className="rounded-full bg-surface-2 px-2 py-0.5 text-xs text-muted">
            {t}
          </span>
        ))}
      </div>
      {children}
    </li>
  );
}

/** 每位角色一區：有人坐的在前，沒人坐的收在摺疊區（一套衣服都沒有的角色不顯示）。 */
function readRemembered(): string {
  try {
    return window.localStorage.getItem(PERSONA_KEY) ?? "";
  } catch {
    return "";
  }
}

function remember(code: string) {
  try {
    window.localStorage.setItem(PERSONA_KEY, code);
  } catch {
    // 私密視窗、封鎖網站資料：記不住就算了，下次從第一位開始。
  }
}

/**
 * 現在看哪位角色（DEV-143）：網址的 ?persona= 優先、再來是這台瀏覽器上次看的、都不在清單上就第一位。
 * 選了就記下來，網址也換成 ?persona=<代號>（replaceState 帶著原本的 history.state，看板的上一頁才不會亂，FEN-29）。
 */
export function usePickedPersona(list: Person[], search: string): [string, (code: string) => void] {
  // localStorage 只在第一次畫的時候讀一次（重畫不再碰它）。
  const [remembered] = useState(readRemembered);
  const [picked, setPicked] = useState(() => personaFromSearch(search) || remembered);
  const current = chosenPersona(list, picked, remembered);
  const pick = useCallback((code: string) => {
    setPicked(code);
    remember(code);
    try {
      const next = withPersona(window.location.search, code);
      window.history.replaceState(
        window.history.state,
        "",
        `${window.location.pathname}${next}${window.location.hash}`,
      );
    } catch {
      // 換不了網址也不影響看哪位。
    }
  }, []);
  return [current, pick];
}

/**
 * 一排角色頭像（DEV-143）：一次只看一位，點了換人；手機上橫向捲動、每格至少 44px。
 * 頭像用她現在穿的那張圖（沒有就畫人形）；順序照 pickerPeople（有人擔任職位的在前）。
 */
export function PersonPicker({
  people,
  current,
  onPick,
}: {
  people: Person[];
  current: string;
  onPick: (code: string) => void;
}) {
  if (people.length <= 1) return null;
  return (
    <fieldset
      data-testid="person-picker"
      className="m-0 -mx-1 flex min-w-0 gap-1 overflow-x-auto border-0 px-1 pb-1"
    >
      <legend className="sr-only">{T.pickPerson}</legend>
      {people.map((p) => {
        const on = p.persona === current;
        return (
          <button
            key={p.persona}
            type="button"
            aria-pressed={on}
            data-testid="person-pick"
            data-persona={p.persona}
            onClick={() => onPick(p.persona)}
            className={`flex w-[4.5rem] shrink-0 flex-col items-center gap-1 rounded-xl p-1.5 focus-visible:outline-2 focus-visible:outline-focus ${on ? "bg-accent-soft" : "hover:bg-surface-2"}`}
          >
            <span
              className={`block size-12 rounded-full ${on ? "ring-2 ring-accent ring-offset-2 ring-offset-bg" : ""}`}
            >
              <OutfitImage url={wornImage(p)} real round testId="person-pick-img" />
            </span>
            <span
              className={`w-full truncate text-center text-xs ${on ? "font-semibold text-accent" : "text-muted"}`}
            >
              {p.name}
            </span>
          </button>
        );
      })}
    </fieldset>
  );
}

/**
 * 隱藏版開關（CLO-17）：衣櫃與展示間頂端，隱藏版只有一組（DEV-160 S2），任一筆設定 button = true 才畫；沒有就什麼都不畫。
 * 讀程式寫在 closet.state 的 variants（不自己算時段）；按了寫這一組每筆 closet.variant 的 on，onChanged 讓頁面重讀 person。
 */
export function VariantSwitches({ onChanged }: { onChanged: () => void }) {
  const variants = useLoad(loadVariantSwitches, 3000);
  if (!variants.data || variants.data.length === 0) return null;
  return (
    <VariantBar
      switches={variants.data}
      onChanged={() => {
        variants.reload();
        onChanged();
      }}
    />
  );
}

/** 隱藏版開關列：按了先照剛按的顯示（UXC-9），程式算好的 state 追上來就改回讀 state。 */
function VariantBar({ switches, onChanged }: { switches: VariantSwitch[]; onChanged: () => void }) {
  const [pending, setPending] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // state 追上剛按的值就丟掉暫存，之後別人（秘書、時段）改的照 state 顯示；
  // 10 秒還沒追上（程式沒在跑、或別人又改回去）也丟掉，改回照 state，不讓開關一直顯示錯的。
  useEffect(() => {
    if (Object.keys(pending).length === 0) return;
    const t = setTimeout(() => setPending({}), 10_000);
    return () => clearTimeout(t);
  }, [pending]);
  useEffect(() => {
    setPending((p) => {
      const left = Object.fromEntries(
        Object.entries(p).filter(([id, on]) => switches.find((v) => v.id === id)?.on !== on),
      );
      return Object.keys(left).length === Object.keys(p).length ? p : left;
    });
  }, [switches]);
  const toggle = async (v: VariantSwitch, on: boolean) => {
    setBusy(true);
    setError("");
    try {
      await setVariantOn(v, on);
      setPending((p) => ({ ...p, [v.id]: on }));
      onChanged();
    } catch (e) {
      setError(reasonOf(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div data-testid="variant-bar" className="flex flex-col gap-1">
      <div className="flex flex-wrap gap-x-4">
        {switches.map((v) => {
          const on = pending[v.id] ?? v.on;
          return (
            <div key={v.id} data-variant={v.id} className="flex flex-col">
              <Switch
                on={on || v.inWindow}
                disabled={busy || v.inWindow}
                label={v.name}
                text={v.name}
                testId="variant-toggle"
                onChange={(next) => void toggle(v, next)}
              />
              {v.inWindow ? <p className="text-xs text-muted">{T.variantInWindow}</p> : null}
            </div>
          );
        })}
      </div>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

/** 聊天清單：「到聊天請秘書幫忙」開到這裡，主人自己挑一位、自己說（不替主人送話）。 */
const CHAT_HREF = "/chat";

/**
 * 衣櫃、展示間一套衣服都沒有時的說明（CLO-25，DEV-157）：照角色包的狀態分三種白話——衣服包還沒裝（給「到聊天請秘書幫忙」，
 * 開聊天清單，DEV-156 U-09）／正在下載或上次沒成功（會自動再試，另給「現在再試」）／已經同步但沒有衣服；讀不到角色包狀態就用原本的說明（fallback）。
 * 「現在再試」請服務再同步一次（不帶指標、不強制），結果那一行照實顯示；好了 onSynced 讓頁面重讀。
 * 自己上傳的入口照樣留著（closet-mine 能用時）。
 */
export function EmptyCloset({
  wanted,
  fallback,
  onSynced,
}: {
  /** 網址指定要看的那位（?persona=）；沒有是空字串。 */
  wanted: string;
  fallback: { title: string; hint: string };
  onSynced: () => void;
}) {
  const pack = useLoad(loadPackStatus, 5000);
  const mine = useLoad(loadMineAvailable, 30000);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  if (pack.data === undefined) {
    return (
      <div className="flex justify-center py-8">
        <Spinner label={T.loading} />
      </div>
    );
  }
  const notice = emptyNotice(pack.data, wanted, fallback);
  const retry = async () => {
    setBusy(true);
    setResult(null);
    try {
      const text = await retryPack();
      if (alive.current) setResult({ ok: true, text });
    } catch (e) {
      if (alive.current) setResult({ ok: false, text: `${T.packRetryFailed}${reasonOf(e)}` });
    }
    if (!alive.current) return;
    setBusy(false);
    pack.reload();
    onSynced();
  };
  return (
    <div
      data-testid="closet-empty"
      data-kind={notice.kind}
      className="flex flex-col items-center gap-2 rounded-[18px] border border-dashed border-border-strong px-4 py-[22px] text-center"
    >
      <div className="text-[15px] font-semibold">{notice.title}</div>
      <div data-testid="closet-empty-hint" className="text-[13px] text-muted">
        {notice.hint}
      </div>
      {notice.retry ? (
        <Button
          variant="secondary"
          data-testid="closet-pack-retry"
          disabled={busy}
          onClick={() => void retry()}
        >
          {busy ? T.packRetrying : T.packRetry}
        </Button>
      ) : null}
      {result ? (
        <p
          role={result.ok ? "status" : "alert"}
          data-testid="closet-pack-result"
          className={result.ok ? "text-[13px] text-muted" : "text-[13px] text-danger"}
        >
          {result.text}
        </p>
      ) : null}
      {notice.kind === "none" ? (
        <a
          href={CHAT_HREF}
          data-testid="closet-empty-ask"
          className="inline-flex min-h-11 items-center text-[13px] font-medium text-accent no-underline"
        >
          {T.packAsk}
        </a>
      ) : null}
      {mine.data === true && notice.kind !== "unknown" ? (
        <p className="text-[13px] text-muted">
          {T.ownUploadHint}
          <a
            href={mineHref(wanted ? "person" : "new", wanted)}
            data-testid="closet-empty-upload"
            className="ml-1 inline-flex min-h-11 items-center font-medium text-accent no-underline"
          >
            {T.ownUpload}
          </a>
        </p>
      ) : null}
    </div>
  );
}
