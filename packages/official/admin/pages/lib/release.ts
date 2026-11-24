// 開發工坊頁面的純函式與字：把 admin 程式寫的 admin.release／admin.devhome 轉成畫面用的形狀，
// 以及按鈕開的任務單內文（部署、發版測試、發正式版）。從舊 /admin/ 的 ReleasePage 搬來（計畫 2026-10-31-dev-workshop-split S2）。

/** 接單的秘書：擔任 admin 職位的那位（誰坐、用哪個帳號在「設定 → 秘書」改，跟其他秘書一樣）。 */
export const ADMIN_AGENT = "admin";

export const T = {
  loading: "載入中",
  loadError: "讀不到：",
  refresh: "重新檢查",
  refreshing: "檢查中",
  refreshRejected: "沒有重新檢查：",
  refreshTimeout:
    "admin 的程式 30 秒內沒有回應（可能沒有在跑，或還在算），先顯示上次算好的；等一下再按一次。",
  computedAt: "算好的時間：{at}",
  notYet: "admin 的程式還沒算出來（剛啟用時要等一下），或程式沒有在跑。",

  releaseTitle: "發版",
  releaseSub: "看這台跑的版本和還沒部署的改動；按鈕會開一張任務單給 admin 秘書，照發版流程去做。",
  versions: "版本",
  hereVersion: "這台跑的版本",
  versionUnknown: "不知道版本",
  main: "main 最新",
  mainNone: "讀不到 main 的最新版本",
  pending: "還沒部署的改動",
  pendingNone: "沒有還沒部署的改動：這台跑的就是 main 最新的版本。",
  pendingUnknown: "算不出還沒部署的改動，原因在上面的說明。",
  pendingMore: "還有更多",

  noAgent:
    "沒有 admin 秘書可以接單：請在「設定 → 秘書」開 admin 這個職位並派一位角色（開發用帳號也在那裡選）。",
  confirmNo: "先不要",
  failed: "沒有開成任務單：{reason}",
  notStarted: "單開好了，但還沒開工：{reason}。到那張單上按「開始做」。",
  running: "已經有一張在跑了：{id} {title}",
  see: "看那張任務單",

  deploy: "部署到 mini 和 office",
  deployConfirm:
    "會開一張部署單給{name}，照發版流程先部署 mini、再部署 office；失敗會自動換回舊版。",
  deployYes: "開部署單",
  deployTitle: "部署 {short} 到 mini、office",
  deployBodyHead: "主人從開發工坊的「發版」按了部署。",
  deployBodyVersion: "- 要部署的版本：{commit}（{subject}）",
  deployBodyHow:
    "- 照 `ai_docs/runbooks/release.md`：發版前檢查、在 office 跑 `scripts/deploy.sh all`（先 mini 再 office，失敗會自動換回舊版）。",
  deployBodyChanges: "- 這次帶上的改動：",
  deployBodyTail:
    "開工先補驗收條件（`evergenie tasks set <編號> --criteria …`，例：兩台的版本都是這個 commit、健檢沒有紅字）；做完用 `evergenie tasks submit <編號> --proposed done --result … --verify verified --verify_how …` 回報兩台的版本與檢查結果。",

  stable: "正式版",
  stableSub:
    "正式版是給別的使用者升級用的：跑發版測試全部通過的版本會標成「測試過」，你確定了才把最新的一個升成正式版。",
  latest: "上一個正式版",
  latestNone: "還沒有發過正式版",
  safe: "最新測試過的版本",
  safeNone: "還沒有測過的版本",
  safeSame: "和上一個正式版是同一版",
  safeNewer: "比上一個正式版新",
  nightly: "上次每晚測試",
  nightlyNone: "還沒有跑過每晚測試",
  nightlyOk: "✔ 全部通過",
  nightlyBad: "✘ 有一步沒過",
  nightlyStep: "{name} · {seconds} 秒",
  nightlyStepBad: "{name} · {seconds} 秒 · 沒過",
  noSafe: "還沒有測過的版本：先按上面的「跑發版測試」，過了才能發正式版。",
  noNext:
    "還沒有下一版的草稿：發版測試通過後會自動寫好建議的版號和發版說明（`scripts/release.sh prepare`）。",

  nightlyRun: "跑發版測試",
  nightlyHint: "完整測試要跑幾分鐘到十幾分鐘；全過就會標成「測試過」，才能發正式版。",
  nightlyConfirm:
    "會開一張發版測試的單給{name}，在 office 跑完整測試，全過才打「測試過」標記，不會推到外面。確定嗎？",
  nightlyYes: "開測試單",
  nightlyTitle: "發版測試（每晚測試）",
  nightlyBodyHead: "主人從開發工坊的「發版」按了跑發版測試，想接著發正式版。",
  nightlyBodyHow:
    "- 做法：照 `ai_docs/runbooks/release.md`「正式發版」，在 office 的 repo 跑 `scripts/release.sh nightly`（不加 `--push`，safe 標記只打在本機）；同一個 commit 測過就加 `--force` 重跑。\n- 過了之後：`scripts/release.sh prepare` 寫的發版說明草稿（`~/.evergenie-release/next.json`）是原樣的 commit 標題。請改寫成使用者看得懂的白話：分「新增」「修正」兩段、合併同一件事、拿掉純內部的（測試、文件、重構），存成檔後跑 `scripts/release.sh notes --notes <檔>` 寫回去，主人在「發版」頁看到的就是這份；版號不對就在單上說明建議改成多少。",
  nightlyBodyTail:
    "- 開工先補驗收條件（`evergenie tasks set <編號> --criteria …`）；做完用 `evergenie tasks submit <編號> --proposed done --result … --verify verified --verify_how …` 回報：每一步有沒有過、有沒有打 safe 標記、建議的下一版版號。沒過就寫是哪一步、為什麼。",

  version: "版號",
  versionHint: "寫成 x.y.z，例如 0.2.0。要比上一個正式版大。",
  versionBad: "版號要寫成 x.y.z（三個數字用點隔開），例如 0.2.0。",
  versionSmall: "版號要比上一個正式版 {latest} 大。",
  notes: "發版說明",
  notesHint: "使用者在「版本與更新」會看到這段，可以改。",
  notesDraft:
    "這還是原樣的 commit 清單：跑發版測試的 admin 秘書會改寫成白話換掉（這裡會自己更新），也可以自己改。",
  notesEmpty: "發版說明不能是空的。",
  stableGo: "發正式版 {version}",
  stableConfirm:
    "會開一張發正式版的單給{name}：把測試過的 {safe} 升成正式版 {version}。使用者之後就能升級到它。確定嗎？",
  stableYes: "開單",
  stableTitle: "發正式版 {version}",
  stableBodyHead: "主人從開發工坊按了發正式版。",
  stableBodyVersion: "- 版號：{version}",
  stableBodySafe: "- 要升成正式版的測試過版本：{safe}（{commit}）",
  stableBodyNotes: "- 發版說明：",
  stableBodyHow:
    "- 做法：照 `ai_docs/runbooks/release.md`「正式發版」：把說明存成檔、跑 `scripts/release.sh stable --version {version} --notes <檔> --push`，做完把結果寫回這張單。",

  homeTitle: "開發家",
  homeSub: "開發家是只在開發時開的另一個家：有自己的資料、port 和終端機 socket，不推播。",
  homeState: "狀態",
  // 卡片標題不寫「狀態」：第一列就是「狀態」，重複（ADM-33）。
  homeCard: "這台的開發家",
  homeDetail: "說明",
  stateRunning: "開著",
  stateStopped: "沒開",
  stateUnset: "還沒設定",
  homePath: "資料夾",
  homeVersion: "版本",
  homeUrl: "網址",
  homePort: "看板 port",
  homeVersionUnknown: "不知道（對方的核心比較舊，不回報版本）",
  homePortUnknown: "讀不到（開發家的 home.toml 沒寫看板 port）",
  homeUnsetHint: "在 home.toml 的 [admin] 寫開發家的完整路徑，admin 的程式就會開始檢查它：",
  homeUnsetCode: '[admin]\ndev_home = "/完整/路徑/到/開發家"',
  homeStoppedHint: "開發家沒有開時是正常的：它只在開發時才啟動。要開它，請跟 admin 秘書說。",
  homeAccount:
    "開發用帳號就是 admin 秘書用的帳號：在「設定 → 秘書」的 admin 那一列選，跟其他秘書一樣。",
} as const;

/**
 * 確認句裡「給{name}」的名字：英文開頭的名字前面補一格空白（「給 Helper」），中文名字照舊（「給阿媞米斯」）。
 * 後面接的是全形標點，不用再補。
 */
export function nameAfterCJK(name: string): string {
  return /^[A-Za-z0-9]/.test(name) ? ` ${name}` : name;
}

/** 把 {name} 換成值。 */
export function fill(s: string, vars: Record<string, string | number> = {}): string {
  return s.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m));
}

export interface RecordLike {
  id?: string;
  fields?: Record<string, unknown>;
}

export interface Commit {
  commit: string;
  short: string;
  subject: string;
  at: string;
}

export interface Tag {
  name: string;
  version: string;
  commit: string;
  short: string;
  at: string;
  notes: string;
}

export interface NightlyStep {
  name: string;
  ok: boolean;
  seconds: number;
}

export interface Nightly {
  commit: string;
  short: string;
  at: string;
  ok: boolean;
  steps: NightlyStep[];
}

export interface Next {
  version: string;
  notes: string;
  /** 草稿對應的測試過版本（safe tag）；換了草稿就換表單。 */
  safeTag: string;
  /** 說明還是原樣的 commit 清單（還沒改寫成白話，DEV-113）。 */
  draft: boolean;
}

export interface Release {
  /** 這台核心跑的版本；讀不到是空字串。 */
  version: string;
  main: Commit | null;
  pending: Commit[];
  pendingMore: boolean;
  detail: string;
  latest: Tag | null;
  safe: Tag | null;
  nightly: Nightly | null;
  next: Next | null;
  computedAt: string;
}

export type HomeState = "unset" | "stopped" | "running";

export interface DevHome {
  state: HomeState;
  path: string;
  version: string;
  boardPort: number;
  detail: string;
  url: string;
  computedAt: string;
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);
const obj = (v: unknown): Record<string, unknown> | null =>
  v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
const list = (v: unknown): Record<string, unknown>[] =>
  Array.isArray(v) ? v.map(obj).filter((x): x is Record<string, unknown> => x !== null) : [];

function toCommit(v: unknown): Commit | null {
  const o = obj(v);
  if (!o) return null;
  const commit = str(o.commit);
  return {
    commit,
    short: str(o.short) || commit.slice(0, 7),
    subject: str(o.subject),
    at: str(o.at),
  };
}

function toTag(v: unknown): Tag | null {
  const o = obj(v);
  if (!o || !str(o.name)) return null;
  const commit = str(o.commit);
  return {
    name: str(o.name),
    version: str(o.version),
    commit,
    short: str(o.short) || commit.slice(0, 7),
    at: str(o.at),
    notes: str(o.notes),
  };
}

// 跨台時代的舊資料沒有 version、只有 sites（每台一列：site、role、online、version）：取第一個有寫版本的那台。
// admin 的程式下一輪就會整筆換掉，這裡只是不讓舊資料顯示成「不知道版本」。
function legacyVersion(v: unknown): string {
  for (const s of list(v)) {
    const ver = str(s.version);
    if (ver) return ver;
  }
  return "";
}

export function toRelease(r: RecordLike | null | undefined): Release | null {
  if (!r) return null;
  const f = r.fields ?? {};
  const n = obj(f.nightly);
  const next = obj(f.next);
  return {
    version: str(f.version) || legacyVersion(f.sites),
    main: toCommit(f.main),
    pending: list(f.pending)
      .map(toCommit)
      .filter((c): c is Commit => c !== null),
    pendingMore: f.pending_more === true,
    detail: str(f.detail),
    latest: toTag(f.latest),
    safe: toTag(f.safe),
    nightly: n
      ? {
          commit: str(n.commit),
          short: str(n.short) || str(n.commit).slice(0, 7),
          at: str(n.at),
          ok: n.ok === true,
          steps: list(n.steps).map((s) => ({
            name: str(s.name),
            ok: s.ok === true,
            seconds: num(s.seconds),
          })),
        }
      : null,
    next:
      next && str(next.version)
        ? {
            version: str(next.version),
            notes: str(next.notes),
            safeTag: str(next.safe_tag),
            draft: next.draft === true,
          }
        : null,
    computedAt: str(f.computed_at),
  };
}

const STATES: readonly HomeState[] = ["unset", "stopped", "running"];

export function toDevHome(r: RecordLike | null | undefined): DevHome | null {
  if (!r) return null;
  const f = r.fields ?? {};
  const state = STATES.find((s) => s === f.state) ?? "unset";
  return {
    state,
    path: str(f.path),
    version: str(f.version),
    boardPort: num(f.board_port),
    detail: str(f.detail),
    url: /^https?:\/\//.test(str(f.url)) ? str(f.url) : "",
    computedAt: str(f.computed_at),
  };
}

/**
 * 開發家卡片的「說明」：開著時程式寫的是「開發家開著（…不會回報版本）」，跟狀態、版本兩列重複，不顯示；
 * 沒開時才有用（為什麼算沒開、路徑要寫完整）。
 */
export function homeDetail(h: Pick<DevHome, "state" | "detail">): string {
  return h.state === "running" ? "" : h.detail;
}

/** 部署單的內文：要部署的版本、做法、這次帶上的改動，做完怎麼回報。 */
export function deployBody(main: Commit, pending: readonly Commit[]): string {
  return [
    T.deployBodyHead,
    fill(T.deployBodyVersion, { commit: main.commit, subject: main.subject }),
    T.deployBodyHow,
    T.deployBodyChanges,
    ...pending.map((c) => `  - ${c.short} ${c.subject}`),
    T.deployBodyTail,
  ].join("\n");
}

/**
 * 「還沒部署的改動」那一段要寫的一句話；回空字串＝列出改動。讀不到 main 時上一列已經寫了
 * 「讀不到 main 的最新版本」，這裡改說原因在上面，不重複同一句。
 */
export function pendingNote(r: Pick<Release, "main" | "pending" | "detail">): string {
  if (!r.main) return T.pendingUnknown;
  if (r.pending.length > 0) return "";
  return r.detail ? T.pendingUnknown : T.pendingNone;
}

/** 發版測試單的內文：主人為什麼按、怎麼跑、做完怎麼回報。 */
/**
 * 「還沒有測過的版本」那句提示：只在上面真的有「跑發版測試」鈕時才說「按上面的」。
 * 秘書名冊還沒讀到、或這個家沒有 admin 秘書（上面已經寫了 noAgent）時不說（DEV-113）。
 */
export function noSafeHint(desk: { name: string } | undefined): string {
  return desk?.name ? T.noSafe : "";
}

export function nightlyBody(): string {
  return [T.nightlyBodyHead, T.nightlyBodyHow, T.nightlyBodyTail].join("\n");
}

/** 發正式版單的內文：版號、要升格的測試過版本、發版說明全文、做法與回報。 */
export function stableBody(
  version: string,
  safeTag: string,
  safeCommit: string,
  notes: string,
): string {
  return [
    T.stableBodyHead,
    fill(T.stableBodyVersion, { version }),
    fill(T.stableBodySafe, { safe: safeTag, commit: safeCommit }),
    T.stableBodyNotes,
    notes.trim(),
    fill(T.stableBodyHow, { version }),
  ].join("\n");
}

const VERSION_RE = /^\d+\.\d+\.\d+$/;

function newerThan(a: string, b: string): boolean {
  const x = a.split(".").map(Number);
  const y = b.split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    if ((x[i] ?? 0) !== (y[i] ?? 0)) return (x[i] ?? 0) > (y[i] ?? 0);
  }
  return false;
}

/** 版號檢查：空字串（還沒填）不報錯；格式不對或不比上一版大回白話原因，沒問題回空字串。 */
export function versionProblem(version: string, latest: string): string {
  if (version === "") return "";
  if (!VERSION_RE.test(version)) return T.versionBad;
  if (VERSION_RE.test(latest) && !newerThan(version, latest)) {
    return fill(T.versionSmall, { latest });
  }
  return "";
}

const pad2 = (n: number) => String(n).padStart(2, "0");

/**
 * 程式寫的時間（RFC3339，例 2026-11-03T02:10:00+08:00）換成白話的「11 月 3 日 02:10」（照看的人瀏覽器的時區）；
 * 不是今年的多寫年；讀不懂的照原樣、空的回空字串（ADM-27）。
 */
export function whenText(iso: string, now: Date = new Date()): string {
  if (!iso) return "";
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  const year = at.getFullYear() === now.getFullYear() ? "" : `${at.getFullYear()} 年 `;
  return `${year}${at.getMonth() + 1} 月 ${at.getDate()} 日 ${pad2(at.getHours())}:${pad2(at.getMinutes())}`;
}

/** 測試過的版本 tag 是 safe/2026-10-29(.2)：畫面上只寫日期那段。 */
export const safeLabel = (name: string): string => name.replace(/^safe\//, "");

/** 三種單的標題開頭：同一種還沒結束的單已經有了就不再開（找標題開頭）。 */
export const KIND_PREFIX = {
  deploy: "部署 ",
  nightly: "發版測試",
  stable: "發正式版 ",
} as const;

export interface TaskLike {
  id?: string;
  title?: string;
  status?: string;
}

const OPEN = new Set(["idea", "doing", "waiting_user", "waiting_external"]);

/** 還沒結束、標題以 prefix 開頭的第一張單。 */
export interface StartedTask {
  id: string;
  /** 單開好了但沒改成進行中：沒有人開工，原因寫在這（單還在，不能當成沒開成）。 */
  notStarted?: string;
}

/**
 * 開單並開工：先開成待做再改進行中。新開就是進行中的單不會開工作 session，
 * 要「改成進行中」才會開對話、把單送給秘書（SES-27、DEV-124）。
 */
export async function createAndStart(
  tasks: {
    create(req: {
      title: string;
      body: string;
      agent: string;
      status: "idea";
    }): Promise<{ task?: { id?: string } }>;
    update(req: { id: string; status: "doing" }): Promise<unknown>;
  },
  req: { title: string; body: string; agent: string },
): Promise<StartedTask> {
  const r = await tasks.create({ ...req, status: "idea" });
  const id = r.task?.id ?? "";
  if (!id) return { id };
  try {
    await tasks.update({ id, status: "doing" });
    return { id };
  } catch (e) {
    return { id, notStarted: e instanceof Error && e.message ? e.message : String(e) };
  }
}

export function runningTask<X extends TaskLike>(
  tasks: readonly X[],
  prefix: string,
): X | undefined {
  return tasks.find((t) => OPEN.has(t.status ?? "") && (t.title ?? "").startsWith(prefix));
}

/** 這個畫面剛開的單；seen＝看板的「沒結束的單」清單已經列過它。 */
export interface Opened {
  id: string;
  title: string;
  seen: boolean;
  /** 還沒列過時，重讀了幾次清單都沒有它（單在列到前就結束、或沒拿到單號時不會一直卡在「在跑」）。 */
  misses?: number;
}

/** 沒列過的剛開的單，最多等幾次清單重讀就放掉。 */
export const OPENED_MAX_MISSES = 3;

/**
 * 剛開的單在清單列到之前照樣當成在跑（不讓按鈕閃回來、被按第二次）；清單列過一次之後就交給清單，
 * 單結束（清單不再有它）就丟掉，按鈕回來（ADM-30）。
 */
export function trackOpened(opened: Opened | null, open: readonly TaskLike[]): Opened | null {
  if (!opened) return null;
  const listed = open.some((t) => t.id === opened.id && OPEN.has(t.status ?? ""));
  if (listed) return opened.seen ? opened : { ...opened, seen: true };
  if (opened.seen || !opened.id) return null;
  const misses = (opened.misses ?? 0) + 1;
  return misses >= OPENED_MAX_MISSES ? null : { ...opened, misses };
}

export interface RequestState {
  state: string;
  reason: string;
}

/**
 * 等重算請求處理完：每 wait 一次就讀一次，done／rejected 就停；tries 次都還沒好回 timeout
 * （程式沒在跑時不能默默當成好了，畫面要說，ADM-29）。
 */
export async function waitRequest(
  load: () => Promise<RequestState | undefined>,
  wait: () => Promise<void>,
  tries = 30,
): Promise<{ end: "done" | "rejected" | "timeout"; reason: string }> {
  for (let i = 0; i < tries; i++) {
    await wait();
    const req = await load();
    if (req?.state === "done") return { end: "done", reason: "" };
    if (req?.state === "rejected") return { end: "rejected", reason: req.reason };
  }
  return { end: "timeout", reason: "" };
}

/** 任務單的看板網址。 */
export const taskHref = (id: string): string => `/tasks/${encodeURIComponent(id)}`;
