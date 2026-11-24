import type { Error as SdkError } from "./gen/types";

const UNAVAILABLE = "連不到看板服務，稍後會自動重試";
const TIMED_OUT = "等太久沒有回應，可以再試一次";
const GATEWAY_DOWN = new Set([502, 503, 504]);

// 伺服器回的 sdk.Error 或網路失敗，統一成這個例外。
export class ApiError extends Error {
  readonly code: string;
  readonly field?: string;

  constructor(code: string, message: string, field?: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.field = field;
  }
}

function unavailable(): ApiError {
  return new ApiError("unavailable", UNAVAILABLE);
}

function timedOut(): ApiError {
  return new ApiError("unavailable", TIMED_OUT);
}

/**
 * 不是看板服務自己回的錯（中間的代理說後面的服務不在）：串流的「連不上」只算這種與請求根本沒回來的（DEV-156 U-21）；
 * 看板服務自己回的「暫時無法服務」（例：引擎還沒好）表示連得到，不算。
 */
const unreachable = new WeakSet<ApiError>();

async function toApiError(res: Response): Promise<ApiError> {
  try {
    const body = (await res.json()) as Partial<SdkError>;
    if (body && typeof body.code === "string") {
      return new ApiError(body.code, body.message ?? "", body.field);
    }
  } catch {
    // 不是 JSON：下面用狀態碼補一個。
  }
  // 不是看板服務自己回的：中間的代理（Tailscale）說後面的服務不在，多半是服務正在重開，跟連不上一樣。
  // 當成永久錯誤的話，開著的頁面串流就此停掉、再也收不到新訊息（DEV-132）。
  if (GATEWAY_DOWN.has(res.status)) {
    const err = unavailable();
    unreachable.add(err);
    return err;
  }
  return new ApiError(
    res.status >= 500 ? "internal" : "invalid",
    `看板服務回應異常（${res.status}）`,
  );
}

// 用字元數估：JSON 多半是 ASCII；中文一字 3 bytes，所以留足餘裕。
const KEEPALIVE_MAX = 16_000;

function post(
  path: string,
  req: unknown,
  signal?: AbortSignal,
  keepalive?: boolean,
): Promise<Response> {
  const body = JSON.stringify(req);
  return fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
    signal,
    // 瀏覽器限制 keepalive 請求的內容合計 64 KiB，超過會直接失敗；長訊息照一般請求送。
    ...(keepalive && body.length < KEEPALIVE_MAX ? { keepalive: true } : {}),
  });
}

/** 讀取等多久沒回應就放棄（看板後端自己的時限比這個短，正常情況先收到它的白話錯誤）。 */
export const READ_TIMEOUT_MS = 10_000;

// 本來就可能跑很久的讀取（健康檢查要逐項跑、查更新要連外網）：不設時限。
const SLOW_READS = new Set(["/api/web/get_health", "/api/package/list_updates"]);
const READ_PATH = /(\/api\/[a-z_]+\/(?:get|list)(?:_[a-z_]+)?)$/;

/**
 * 這個請求最多等多久（WEB-175）：讀取（方法名是 get、get_*、list、list_*）等 READ_TIMEOUT_MS；
 * 會改東西的、可能跑很久的不設時限（回 undefined）——中途放棄不知道伺服器做了沒，主人再按一次會做兩次。
 * path 可以帶別台的網址前綴（https://別台/api/…）。
 */
export function readTimeoutFor(path: string): number | undefined {
  const m = READ_PATH.exec(path.split("?")[0] ?? "");
  if (!m?.[1] || SLOW_READS.has(m[1])) return undefined;
  return READ_TIMEOUT_MS;
}

export async function call<Req, Resp>(path: string, req: Req): Promise<Resp> {
  // 讀取有時限：伺服器卡住、別台不通時，畫面拿到「等太久」的錯誤、可以重試，不會一直轉圈（WEB-175）。
  const limit = readTimeoutFor(path);
  const timer = limit === undefined ? undefined : new AbortController();
  const timeout = timer ? setTimeout(() => timer.abort(), limit) : undefined;
  try {
    let res: Response;
    try {
      // 一般請求（不是串流）都用 keepalive：離開頁面（pagehide）時送出的動作不會被瀏覽器取消。
      res = await post(path, req, timer?.signal, true);
    } catch {
      throw timer?.signal.aborted ? timedOut() : unavailable();
    }
    if (!res.ok) {
      throw await toApiError(res);
    }
    try {
      return (await res.json()) as Resp;
    } catch (err) {
      // 標頭回來了、內容卡在半路也算等太久。
      if (timer?.signal.aborted) throw timedOut();
      throw err;
    }
  } finally {
    clearTimeout(timeout);
  }
}

const RETRY_DELAYS_MS = [1000, 2000, 5000];
const RETRY_STEADY_MS = 10_000;

function sleep(ms: number, signal?: AbortSignal, wake = false): Promise<void> {
  return new Promise((resolve) => {
    if (signal?.aborted) {
      resolve();
      return;
    }
    // wake：串流等著重連時，網路回來（online）或畫面回到前景就不等完、馬上再連（DEV-156 U-21：
    // 等到 10 秒一次的階段時，連回來後「連不上」的橫條不會多掛好幾秒）。
    const onVisible = () => {
      if (document.visibilityState === "visible") done();
    };
    const hasWindow = wake && typeof window !== "undefined";
    const hasDocument = wake && typeof document !== "undefined";
    const done = () => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", done);
      if (hasWindow) window.removeEventListener("online", done);
      if (hasDocument) document.removeEventListener("visibilitychange", onVisible);
      resolve();
    };
    const timer = setTimeout(done, ms);
    signal?.addEventListener("abort", done);
    if (hasWindow) window.addEventListener("online", done);
    if (hasDocument) document.addEventListener("visibilitychange", onVisible);
  });
}

// 解析一個 SSE 區塊（以空行分隔）：event 名稱＋data 各行。
function parseBlock(block: string): { event: string; data: string } | null {
  let event = "message";
  const data: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith(":")) continue;
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) data.push(line.slice(5).replace(/^ /, ""));
  }
  return data.length ? { event, data: data.join("\n") } : null;
}

/** 串流多久沒收到任何東西（伺服器每 1 秒送一次心跳）就當成斷線重連：斷了 3 秒內知道（REQ-388）。 */
export const STREAM_IDLE_MS = 3_000;

// 讀一次連線直到結束；連線成功（2xx）時呼叫 onOpen。畫面回到前景、自己丟掉這次連線時回 true（呼叫的人馬上重連）。
async function readOnce<Resp>(
  path: string,
  req: unknown,
  onItem: (item: Resp) => void,
  signal: AbortSignal | undefined,
  onOpen: () => void,
): Promise<boolean> {
  // 這一次連線自己的 abort：太久沒收到東西（連心跳都沒有）、或畫面從背景回來，就丟掉重連（REQ-305）。
  // 手機切到背景、換網路時連線會悄悄斷掉，不會有錯誤，不這樣做畫面會一直停在舊的內容。
  const local = new AbortController();
  const stop = () => local.abort();
  // 畫面回到前景時是自己丟掉重連，不是連不上：這次連線（連線中或已經在讀）丟掉時發生的錯都不算失敗，
  // 不讓「連不上」的計數閃一下，也不照失敗的間隔等（DEV-156 U-21）。
  let refreshed = false;
  signal?.addEventListener("abort", stop);
  let idle = setTimeout(stop, STREAM_IDLE_MS);
  const onVisible = () => {
    if (typeof document !== "undefined" && document.visibilityState === "visible") {
      refreshed = true;
      stop();
    }
  };
  if (typeof document !== "undefined") document.addEventListener("visibilitychange", onVisible);
  try {
    const res = await post(path, req, local.signal);
    if (!res.ok) {
      throw await toApiError(res);
    }
    onOpen();
    if (!res.body) {
      return false;
    }
    await readBody(res.body, onItem, local.signal, () => {
      clearTimeout(idle);
      idle = setTimeout(stop, STREAM_IDLE_MS);
    });
    return refreshed && !signal?.aborted;
  } catch (err) {
    if (refreshed && !signal?.aborted) return true;
    throw err;
  } finally {
    clearTimeout(idle);
    signal?.removeEventListener("abort", stop);
    if (typeof document !== "undefined")
      document.removeEventListener("visibilitychange", onVisible);
  }
}

async function readBody<Resp>(
  body: ReadableStream<Uint8Array>,
  onItem: (item: Resp) => void,
  abort: AbortSignal,
  onData: () => void,
): Promise<void> {
  const reader = body.getReader();
  // 丟掉這次連線時也停止讀（不只靠 fetch 的 signal）：read() 會回 done，外層照常重連。
  const cancel = () => void reader.cancel().catch(() => {});
  if (abort.aborted) cancel();
  abort.addEventListener("abort", cancel);
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    onData();
    buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, "\n");
    for (;;) {
      const cut = buffer.indexOf("\n\n");
      if (cut < 0) break;
      const parsed = parseBlock(buffer.slice(0, cut));
      buffer = buffer.slice(cut + 2);
      if (!parsed) continue;
      if (parsed.event === "error") {
        const e = JSON.parse(parsed.data) as SdkError;
        throw new ApiError(e.code, e.message, e.field);
      }
      onItem(JSON.parse(parsed.data) as Resp);
    }
  }
  abort.removeEventListener("abort", cancel);
}

// 跟頁面同一台（同源：路徑以 "/" 開頭）的串流裡，現在有幾條連不上（DEV-156 U-21）。
// 串流本來就會自己重連；這裡只記「這次沒連上」與「又連上了」，不另外發請求。畫面用它說「現在連不上」。
// 別台的串流（網址是完整的 https://…）不算：別台連不上另有那台自己的說明（FEN-28）。
let failingStreams = 0;
const streamListeners = new Set<() => void>();

function markStream(delta: 1 | -1) {
  failingStreams += delta;
  for (const fn of streamListeners) fn();
}

/** 同源的串流現在有沒有連不上的（看板的即時更新斷了）：連上就回 false。 */
export function streamsDown(): boolean {
  return failingStreams > 0;
}

/** 同源串流的連線狀態變了就叫 fn（給 useSyncExternalStore）。回傳取消訂閱。 */
export function onStreamsChange(fn: () => void): () => void {
  streamListeners.add(fn);
  return () => {
    streamListeners.delete(fn);
  };
}

// 串流：斷線自動重連（1s、2s、5s，之後每 10s），每次重連成功後呼叫 onReconnect（讓畫面重新抓清單）；
// signal abort 就停。網路失敗與 unavailable（含代理回的 502、503、504）才重連，其他伺服器錯誤（event: error、4xx）直接丟出。
export async function stream<Req, Resp>(
  path: string,
  req: Req,
  onItem: (item: Resp) => void,
  signal?: AbortSignal,
  onReconnect?: () => void,
): Promise<void> {
  let attempt = 0;
  let opened = false;
  // 這一條現在算不算「連不上」（只記同源的）：連線失敗記一次，連上了或不再連就撤掉。
  const local = path.startsWith("/");
  let down = false;
  const setDown = (next: boolean) => {
    if (!local || next === down) return;
    down = next;
    markStream(next ? 1 : -1);
  };
  try {
    while (!signal?.aborted) {
      const isReconnect = opened;
      let refreshed = false;
      try {
        refreshed = await readOnce<Resp>(path, req, onItem, signal, () => {
          attempt = 0;
          opened = true;
          setDown(false);
          if (isReconnect) onReconnect?.();
        });
      } catch (err) {
        if (signal?.aborted) return;
        if (err instanceof ApiError && err.code !== "unavailable") throw err;
        // 連不上＝請求根本沒回來（網路斷、連線被拒、等太久被丟掉）或代理說後面的服務不在；服務自己回的「暫時無法服務」不算。
        setDown(!(err instanceof ApiError) || unreachable.has(err));
      }
      if (signal?.aborted) return;
      if (refreshed) continue; // 回到前景自己丟掉的：馬上重連，不等
      await sleep(RETRY_DELAYS_MS[attempt] ?? RETRY_STEADY_MS, signal, true);
      attempt++;
    }
  } finally {
    setDown(false);
  }
}
