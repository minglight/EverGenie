// @vitest-environment node
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { ApiError, api, makeApi, upload, uploadPersona } from "./index";

const fetchMock = vi.fn<typeof fetch>();

function ok(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("api（同源）打的路徑跟以前一樣是相對的 /api/<服務>/<方法>", async () => {
  fetchMock.mockResolvedValueOnce(ok({ mode: "simple" }));
  await api.system.getMode({});
  expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/system/get_mode");
  expect(makeApi("").system.getMode).toBeTypeOf("function");
});

test("makeApi(base) 在每個方法的路徑前面接上那台的網址（一般請求與串流都是）", async () => {
  const remote = makeApi("https://office.example.ts.net");
  fetchMock.mockResolvedValueOnce(ok({ mode: "simple" }));
  await remote.system.getMode({});
  expect(fetchMock.mock.calls[0]?.[0]).toBe("https://office.example.ts.net/api/system/get_mode");

  fetchMock.mockResolvedValueOnce(new Response(null, { status: 200 }));
  const stop = new AbortController();
  const done = remote.notify.watch({}, () => {}, stop.signal);
  await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  stop.abort();
  await done;
  expect(fetchMock.mock.calls[1]?.[0]).toBe("https://office.example.ts.net/api/notify/watch");
});

// S3-0b 量過：跨站時 pagehide 送出的動作只有帶 keepalive 的才到得了別台，所以傳完整網址也照樣帶；串流不帶（會一直開著）。
test("傳別台的完整網址時，一般請求照樣帶 keepalive、串流不帶", async () => {
  const remote = makeApi("https://office.example.ts.net");
  fetchMock.mockResolvedValueOnce(ok({}));
  await remote.notify.presence({ client: "c-1", path: "/", thread: "", away: true });
  expect(fetchMock.mock.calls[0]?.[1]?.keepalive).toBe(true);

  fetchMock.mockResolvedValueOnce(new Response(null, { status: 200 }));
  const stop = new AbortController();
  const done = remote.notify.watch({}, () => {}, stop.signal);
  await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  stop.abort();
  await done;
  expect(fetchMock.mock.calls[1]?.[1]?.keepalive).toBeUndefined();
});

test("upload 可以指定傳到哪一台：給 base 時打那台的 /api/web/upload，不給是同源", async () => {
  const file = new File(["hi"], "照片.png", { type: "image/png" });
  fetchMock.mockResolvedValueOnce(ok({ name: "a.png", url: "/share/a.png", size: 2 }));
  await upload(file);
  expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/web/upload");

  fetchMock.mockResolvedValueOnce(ok({ name: "a.png", url: "/share/a.png", size: 2 }));
  const res = await upload(file, "https://office.example.ts.net");
  expect(fetchMock.mock.calls[1]?.[0]).toBe("https://office.example.ts.net/api/web/upload");
  // 回傳的網址照那台給的相對網址；要顯示在別台的看板時由呼叫端補（siteHref）。
  expect(res.url).toBe("/share/a.png");
});

test("uploadPersona：原始位元組 POST 到 /api/web/persona-upload（帶 X-Requested-With），伺服器的白話錯誤照傳", async () => {
  const file = new File(["zip"], "米娜.hgpersona");
  fetchMock.mockResolvedValueOnce(ok({ name: "20261104-120000-0a1b2c3d.hgpersona", size: 3 }));
  const res = await uploadPersona(file);
  expect(res.name).toBe("20261104-120000-0a1b2c3d.hgpersona");
  const [url, init] = fetchMock.mock.calls[0] ?? [];
  expect(url).toBe("/api/web/persona-upload");
  expect((init?.headers as Record<string, string>)["X-Requested-With"]).toBe("evergenie");
  expect(init?.body).toBe(file);

  fetchMock.mockResolvedValueOnce(
    new Response(JSON.stringify({ code: "invalid", message: "檔案太大（上限 2 GB）" }), {
      status: 413,
    }),
  );
  const err = await uploadPersona(file).catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).message).toBe("檔案太大（上限 2 GB）");
});
