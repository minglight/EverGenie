// @vitest-environment node
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import {
  ApiError,
  call,
  onStreamsChange,
  READ_TIMEOUT_MS,
  readTimeoutFor,
  STREAM_IDLE_MS,
  stream,
  streamsDown,
} from "./transport";

function sse(...chunks: string[]): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

// Guards: REQ-116
test("call 用 POST 送 JSON 並回傳解析後的內容", async () => {
  fetchMock.mockResolvedValueOnce(json(200, { ok: 1 }));
  const resp = await call<{ a: string }, { ok: number }>("/api/x/y", { a: "b" });
  expect(resp).toEqual({ ok: 1 });
  const [path, init] = fetchMock.mock.calls[0] ?? [];
  expect(path).toBe("/api/x/y");
  expect(init?.method).toBe("POST");
  expect(init?.headers).toEqual({ "Content-Type": "application/json" });
  expect(init?.body).toBe('{"a":"b"}');
});

// Guards: REQ-116
test("非 2xx 變成帶 code、message、field 的 ApiError", async () => {
  fetchMock.mockResolvedValueOnce(
    json(400, { code: "invalid", message: "請填內容", field: "title" }),
  );
  const err = await call("/api/x/y", {}).catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect(err).toMatchObject({ code: "invalid", message: "請填內容", field: "title" });
});

// Guards: REQ-116
test("網路失敗變成 unavailable 的 ApiError", async () => {
  fetchMock.mockRejectedValueOnce(new TypeError("Failed to fetch"));
  const err = await call("/api/x/y", {}).catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect(err).toMatchObject({ code: "unavailable", message: "連不到看板服務，稍後會自動重試" });
});

// 一直不回應的 fetch：只有 signal 被 abort 才結束（跟瀏覽器一樣丟 AbortError）。
function hangingFetch(_path: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return new Promise((_resolve, reject) => {
    init?.signal?.addEventListener("abort", () =>
      reject(new DOMException("aborted", "AbortError")),
    );
  });
}

test("讀取（get／list 開頭的方法）一直不回應：等 READ_TIMEOUT_MS 就放棄，變成白話的 unavailable", async () => {
  vi.useFakeTimers();
  fetchMock.mockImplementation(hangingFetch);
  const done = call("/api/web/get_menu", {}).catch((e: unknown) => e);
  await vi.advanceTimersByTimeAsync(READ_TIMEOUT_MS - 1);
  let settled = false;
  void done.then(() => {
    settled = true;
  });
  await vi.advanceTimersByTimeAsync(0);
  expect(settled).toBe(false);
  await vi.advanceTimersByTimeAsync(1);
  const err = await done;
  expect(err).toBeInstanceOf(ApiError);
  expect(err).toMatchObject({ code: "unavailable", message: "等太久沒有回應，可以再試一次" });
});

test("會改東西的請求不設時限：伺服器慢也等到它回來，不會中途放棄", async () => {
  vi.useFakeTimers();
  let signal: AbortSignal | null | undefined;
  fetchMock.mockImplementation((_path, init) => {
    signal = init?.signal;
    return new Promise((resolve) => setTimeout(() => resolve(json(200, { ok: 1 })), 60_000));
  });
  const done = call<object, { ok: number }>("/api/tasks/create", {});
  await vi.advanceTimersByTimeAsync(60_000);
  expect(await done).toEqual({ ok: 1 });
  expect(signal).toBeUndefined();
});

test("readTimeoutFor：讀取才有時限，別台的網址前綴照樣認得；可能跑很久的讀取與寫入沒有", () => {
  expect(readTimeoutFor("/api/web/get_menu")).toBe(READ_TIMEOUT_MS);
  expect(readTimeoutFor("/api/tasks/list")).toBe(READ_TIMEOUT_MS);
  expect(readTimeoutFor("/api/tasks/get")).toBe(READ_TIMEOUT_MS);
  expect(readTimeoutFor("/api/session/list_threads")).toBe(READ_TIMEOUT_MS);
  expect(readTimeoutFor("https://other.example/api/web/get_overview")).toBe(READ_TIMEOUT_MS);
  expect(readTimeoutFor("/api/web/get_health")).toBeUndefined();
  expect(readTimeoutFor("/api/package/list_updates")).toBeUndefined();
  expect(readTimeoutFor("/api/tasks/create")).toBeUndefined();
  expect(readTimeoutFor("/api/notify/answer")).toBeUndefined();
  expect(readTimeoutFor("/api/system/set_mode")).toBeUndefined();
});

// Guards: REQ-116
test("stream 一行 data 一筆，跨區塊切開的也會接起來", async () => {
  fetchMock.mockResolvedValueOnce(sse('data: {"n":1}\n\ndata: {"n', '":2}\n\n'));
  const ctrl = new AbortController();
  const items: unknown[] = [];
  const done = stream(
    "/api/x/watch",
    {},
    (item) => {
      items.push(item);
      if (items.length === 2) ctrl.abort();
    },
    ctrl.signal,
  );
  await done;
  expect(items).toEqual([{ n: 1 }, { n: 2 }]);
});

// Guards: REQ-116
test("event: error 的 data 變成 ApiError 丟出，不重連", async () => {
  fetchMock.mockResolvedValueOnce(
    sse('event: error\ndata: {"code":"not_found","message":"沒有這個討論串"}\n\n'),
  );
  const err = await stream("/api/x/watch", {}, () => {}).catch((e: unknown) => e);
  expect(err).toMatchObject({ code: "not_found", message: "沒有這個討論串" });
  expect(fetchMock).toHaveBeenCalledTimes(1);
});

// Guards: REQ-116
test("斷線自動重連（1s、2s），重連成功後呼叫 onReconnect", async () => {
  vi.useFakeTimers();
  fetchMock
    .mockResolvedValueOnce(sse('data: {"n":1}\n\n'))
    .mockRejectedValueOnce(new TypeError("offline"))
    .mockResolvedValueOnce(sse('data: {"n":2}\n\n'))
    .mockImplementation(() => new Promise(() => {}));
  const ctrl = new AbortController();
  const items: unknown[] = [];
  const onReconnect = vi.fn();
  const done = stream("/api/x/watch", {}, (item) => items.push(item), ctrl.signal, onReconnect);

  await vi.advanceTimersByTimeAsync(0);
  expect(items).toEqual([{ n: 1 }]);
  expect(onReconnect).not.toHaveBeenCalled();

  await vi.advanceTimersByTimeAsync(999);
  expect(fetchMock).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(1);
  expect(fetchMock).toHaveBeenCalledTimes(2);
  expect(onReconnect).not.toHaveBeenCalled();

  await vi.advanceTimersByTimeAsync(2000);
  expect(fetchMock).toHaveBeenCalledTimes(3);
  expect(items).toEqual([{ n: 1 }, { n: 2 }]);
  expect(onReconnect).toHaveBeenCalledTimes(1);

  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
});

// 服務重開時 Tailscale 會回不是 JSON 的 502：開著的頁面要自己接回來，不然再也收不到新訊息。
test.each([
  502, 503, 504,
])("串流遇到代理回的 %i 會重連，接回來後呼叫 onReconnect", async (status) => {
  vi.useFakeTimers();
  fetchMock
    .mockResolvedValueOnce(sse('data: {"n":1}\n\n'))
    .mockResolvedValueOnce(new Response("", { status }))
    .mockResolvedValueOnce(sse('data: {"n":2}\n\n'))
    .mockImplementation(() => new Promise(() => {}));
  const ctrl = new AbortController();
  const items: unknown[] = [];
  const onReconnect = vi.fn();
  const done = stream("/api/x/watch", {}, (item) => items.push(item), ctrl.signal, onReconnect);

  await vi.advanceTimersByTimeAsync(1000 + 2000);
  expect(items).toEqual([{ n: 1 }, { n: 2 }]);
  expect(onReconnect).toHaveBeenCalledTimes(1);

  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
});

test("代理回的 502 對一般請求也是 unavailable，訊息說稍後會重試", async () => {
  fetchMock.mockResolvedValueOnce(new Response("Bad Gateway", { status: 502 }));
  const err = await call("/api/x/y", {}).catch((e: unknown) => e);
  expect(err).toMatchObject({ code: "unavailable", message: "連不到看板服務，稍後會自動重試" });
});

test("看板服務自己回的 JSON 502 照它說的代碼，不改成 unavailable", async () => {
  fetchMock.mockResolvedValueOnce(json(502, { code: "internal", message: "壞了" }));
  const err = await call("/api/x/y", {}).catch((e: unknown) => e);
  expect(err).toMatchObject({ code: "internal", message: "壞了" });
});

test("不是 JSON 的 500 還是 internal：只有代理的 502、503、504 算暫時不在", async () => {
  fetchMock.mockResolvedValueOnce(new Response("oops", { status: 500 }));
  const err = await call("/api/x/y", {}).catch((e: unknown) => e);
  expect(err).toMatchObject({ code: "internal", message: "看板服務回應異常（500）" });
});

// Guards: REQ-116
test("signal abort 就停，不再重連", async () => {
  vi.useFakeTimers();
  fetchMock.mockResolvedValue(sse('data: {"n":1}\n\n'));
  const ctrl = new AbortController();
  const done = stream("/api/x/watch", {}, () => {}, ctrl.signal);
  await vi.advanceTimersByTimeAsync(0);
  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
  expect(fetchMock).toHaveBeenCalledTimes(1);
});

// 看板頁首的「現在連不上」（DEV-156 U-21）讀這個狀態：連線失敗才算、連回來就撤，不另外發請求；別台的串流不算。
test("同源串流連不上時 streamsDown 是 true，連回來、停掉都撤掉；別台的串流不算", async () => {
  vi.useFakeTimers();
  const seen: boolean[] = [];
  const off = onStreamsChange(() => seen.push(streamsDown()));
  fetchMock
    .mockResolvedValueOnce(sse('data: {"n":1}\n\n'))
    .mockRejectedValueOnce(new TypeError("offline"))
    .mockImplementation(() => new Promise(() => {}));
  const ctrl = new AbortController();
  const done = stream("/api/x/watch", {}, () => {}, ctrl.signal);
  await vi.advanceTimersByTimeAsync(0);
  expect(streamsDown()).toBe(false);
  // 第一條正常結束（伺服器關掉串流）不算連不上；1 秒後重連失敗才算。
  await vi.advanceTimersByTimeAsync(1000);
  expect(streamsDown()).toBe(true);
  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
  expect(streamsDown()).toBe(false);
  expect(seen).toEqual([true, false]);

  // 別台（完整網址）連不上：不算。
  fetchMock.mockReset();
  fetchMock.mockRejectedValue(new TypeError("offline"));
  const other = new AbortController();
  const otherDone = stream("https://b.example/api/x/watch", {}, () => {}, other.signal);
  await vi.advanceTimersByTimeAsync(5000);
  expect(streamsDown()).toBe(false);
  other.abort();
  await vi.runAllTimersAsync();
  await otherDone;

  // 連回來就撤：失敗一次之後下一次連上，streamsDown 回到 false。
  fetchMock.mockReset();
  fetchMock
    .mockRejectedValueOnce(new TypeError("offline"))
    .mockResolvedValueOnce(openSse('data: {"n":2}\n\n'))
    .mockImplementation(() => new Promise(() => {}));
  const again = new AbortController();
  const againDone = stream("/api/x/watch", {}, () => {}, again.signal);
  await vi.advanceTimersByTimeAsync(0);
  expect(streamsDown()).toBe(true);
  await vi.advanceTimersByTimeAsync(1000);
  expect(streamsDown()).toBe(false);
  again.abort();
  await vi.runAllTimersAsync();
  await againDone;
  off();
});

// 「連不上」只算請求沒回來或代理說後面不在（502）；看板服務自己回的「暫時無法服務」（JSON 503，例：引擎還沒好）表示連得到，不算（DEV-156 review）。
test("看板服務自己回的 unavailable 不算連不上，代理的 502 才算", async () => {
  vi.useFakeTimers();
  fetchMock
    .mockResolvedValueOnce(json(503, { code: "unavailable", message: "引擎還沒好" }))
    .mockResolvedValueOnce(new Response("Bad Gateway", { status: 502 }))
    .mockImplementation(() => new Promise(() => {}));
  const ctrl = new AbortController();
  const done = stream("/api/x/watch", {}, () => {}, ctrl.signal);
  await vi.advanceTimersByTimeAsync(0);
  expect(streamsDown()).toBe(false);
  await vi.advanceTimersByTimeAsync(1000);
  expect(streamsDown()).toBe(true);
  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
  expect(streamsDown()).toBe(false);
});

// 等著重連時網路回來（online）就不等完、馬上再連：到了 10 秒一次的階段，連回來後橫條不會多掛好幾秒。
test("串流等重連時收到 online 就馬上再連", async () => {
  vi.useFakeTimers();
  // 這個檔跑在 node：拿一個 EventTarget 當 window，只用來收發 online。
  const win = new EventTarget();
  vi.stubGlobal("window", win);
  fetchMock.mockRejectedValue(new TypeError("offline"));
  const ctrl = new AbortController();
  const done = stream("/api/x/watch", {}, () => {}, ctrl.signal);
  await vi.advanceTimersByTimeAsync(1000 + 2000 + 5000);
  const before = fetchMock.mock.calls.length;
  // 現在在等 10 秒；才過 1 秒就收到 online。
  await vi.advanceTimersByTimeAsync(1000);
  expect(fetchMock.mock.calls.length).toBe(before);
  win.dispatchEvent(new Event("online"));
  await vi.advanceTimersByTimeAsync(0);
  expect(fetchMock.mock.calls.length).toBe(before + 1);
  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
});

// 開著不關的串流：先送 chunks，之後一直不送東西（模擬手機背景後悄悄斷掉的連線）。
function openSse(...chunks: string[]): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
    },
  });
  return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

// 還在連線中（請求沒回來）就回到前景：丟掉那次時 fetch 會丟錯，那不是連不上——馬上重連、「連不上」不閃（DEV-156 U-21）。
test("連線中回到前景：丟掉的那次不算連不上，馬上重連", async () => {
  vi.useFakeTimers();
  const doc = Object.assign(new EventTarget(), { visibilityState: "visible" });
  vi.stubGlobal("document", doc);
  fetchMock
    .mockImplementationOnce(
      (_url, init) =>
        new Promise((_, reject) => {
          init?.signal?.addEventListener("abort", () =>
            reject(new DOMException("aborted", "AbortError")),
          );
        }),
    )
    .mockImplementation(async () => openSse('data: {"n":1}\n\n'));
  const seen: boolean[] = [];
  const off = onStreamsChange(() => seen.push(streamsDown()));
  const ctrl = new AbortController();
  const done = stream("/api/x/watch", {}, () => {}, ctrl.signal);
  await vi.advanceTimersByTimeAsync(0);
  expect(fetchMock).toHaveBeenCalledTimes(1);
  doc.dispatchEvent(new Event("visibilitychange"));
  await vi.advanceTimersByTimeAsync(0);
  expect(fetchMock).toHaveBeenCalledTimes(2);
  expect(seen).not.toContain(true);
  off();
  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
});

// 畫面回到前景時自己丟掉正在讀的連線重連（REQ-305）：不是連不上——馬上重連、不照失敗的間隔等，「連不上」也不閃（DEV-156 U-21）。
test("回到前景丟掉正在讀的串流：馬上重連，不算連不上", async () => {
  vi.useFakeTimers();
  // 這個檔跑在 node：拿一個 EventTarget 當 document，只用來發 visibilitychange。
  const doc = Object.assign(new EventTarget(), { visibilityState: "visible" });
  vi.stubGlobal("document", doc);
  fetchMock.mockImplementation(async () => openSse('data: {"n":1}\n\n'));
  const seen: boolean[] = [];
  const off = onStreamsChange(() => seen.push(streamsDown()));
  const ctrl = new AbortController();
  const done = stream("/api/x/watch", {}, () => {}, ctrl.signal);
  await vi.advanceTimersByTimeAsync(0);
  expect(fetchMock).toHaveBeenCalledTimes(1);
  doc.dispatchEvent(new Event("visibilitychange"));
  await vi.advanceTimersByTimeAsync(0);
  expect(fetchMock).toHaveBeenCalledTimes(2);
  expect(seen).not.toContain(true);
  off();
  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
});

// Guards: REQ-305
test("太久沒收到任何東西就重連；心跳（: ping）算有收到、但不是資料", async () => {
  vi.useFakeTimers();
  fetchMock
    .mockResolvedValueOnce(openSse('data: {"n":1}\n\n'))
    .mockResolvedValueOnce(openSse(": ping\n\n", 'data: {"n":2}\n\n'))
    .mockImplementation(() => new Promise(() => {}));
  const ctrl = new AbortController();
  const items: unknown[] = [];
  const onReconnect = vi.fn();
  const done = stream("/api/x/watch", {}, (item) => items.push(item), ctrl.signal, onReconnect);
  await vi.advanceTimersByTimeAsync(0);
  expect(items).toEqual([{ n: 1 }]);
  await vi.advanceTimersByTimeAsync(STREAM_IDLE_MS - 1);
  expect(fetchMock).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(1 + 1000);
  expect(fetchMock).toHaveBeenCalledTimes(2);
  expect(items).toEqual([{ n: 1 }, { n: 2 }]);
  expect(onReconnect).toHaveBeenCalledTimes(1);
  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
});

// 斷線 3 秒內知道（S8）：伺服器每 1 秒送一次心跳，心跳不停就不重連；3 秒什麼都沒收到就斷掉重連（重試第一次等 1 秒）。
// Guards: REQ-388
test("3 秒沒收到東西就重連；每秒的心跳讓串流一直活著", async () => {
  vi.useFakeTimers();
  expect(STREAM_IDLE_MS).toBe(3_000);
  const encoder = new TextEncoder();
  let push: ((s: string) => void) | undefined;
  const live = new ReadableStream<Uint8Array>({
    start(controller) {
      push = (s) => controller.enqueue(encoder.encode(s));
    },
  });
  fetchMock
    .mockResolvedValueOnce(
      new Response(live, { status: 200, headers: { "Content-Type": "text/event-stream" } }),
    )
    .mockResolvedValueOnce(openSse())
    .mockImplementation(() => new Promise(() => {}));
  const ctrl = new AbortController();
  const onReconnect = vi.fn();
  const done = stream("/api/x/watch", {}, () => {}, ctrl.signal, onReconnect);
  await vi.advanceTimersByTimeAsync(0);
  for (let i = 0; i < 10; i++) {
    push?.(": ping\n\n");
    await vi.advanceTimersByTimeAsync(1_000);
  }
  expect(fetchMock).toHaveBeenCalledTimes(1);
  // 心跳停了（對方斷線、網路卡住）：3 秒到就放棄這條，1 秒後重連。
  await vi.advanceTimersByTimeAsync(STREAM_IDLE_MS);
  await vi.advanceTimersByTimeAsync(1_000);
  expect(fetchMock).toHaveBeenCalledTimes(2);
  expect(onReconnect).toHaveBeenCalledTimes(1);
  ctrl.abort();
  await vi.runAllTimersAsync();
  await done;
});
