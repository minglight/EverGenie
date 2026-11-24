import { beforeEach, describe, expect, test, vi } from "vitest";
// 手寫的 service worker（public/sw.js）原文：在假的 self 上執行，驗證 push 與點擊的行為。
import swSource from "../public/sw.js?raw";

type Handler = (event: unknown) => void;

interface Shown {
  title: string;
  options: {
    body?: string;
    renotify?: boolean;
    icon?: string;
    tag?: string;
    data?: { url: string };
  };
}

function boot(own: string | null = "amy-mini") {
  const handlers = new Map<string, Handler>();
  const shown: Shown[] = [];
  const opened: string[] = [];
  const windows: {
    url: string;
    focus: () => Promise<void>;
    navigate: (u: string) => Promise<void>;
  }[] = [];
  // 這個看板自己是哪一台：sw 用同源 get_mode 問（own＝null 表示問不到）。
  const fetch = vi.fn(async (url: string, init?: { method?: string }) => {
    if (url !== "/api/system/get_mode" || init?.method !== "POST" || own === null)
      throw new Error("offline");
    return { ok: true, json: async () => ({ label: own }) };
  });
  const self = {
    location: { origin: "http://board.test" },
    fetch,
    addEventListener: (type: string, fn: Handler) => handlers.set(type, fn),
    skipWaiting: vi.fn(),
    clients: {
      claim: vi.fn(async () => {}),
      matchAll: vi.fn(async () => windows),
      openWindow: vi.fn(async (u: string) => {
        opened.push(u);
      }),
    },
    registration: {
      showNotification: vi.fn(async (title: string, options: Shown["options"]) => {
        shown.push({ title, options });
      }),
    },
  };
  new Function("self", swSource)(self);

  const waits: Promise<unknown>[] = [];
  const waitUntil = (p: Promise<unknown>) => waits.push(p);
  return {
    self,
    shown,
    opened,
    windows,
    install: () => handlers.get("install")?.({}),
    activate: async () => {
      handlers.get("activate")?.({ waitUntil });
      await Promise.all(waits);
    },
    push: async (data: unknown) => {
      handlers.get("push")?.({
        waitUntil,
        data:
          data === undefined ? null : { json: () => (typeof data === "function" ? data() : data) },
      });
      await Promise.all(waits);
    },
    click: async (url?: string) => {
      const closed = vi.fn();
      handlers.get("notificationclick")?.({
        waitUntil,
        notification: { close: closed, data: url === undefined ? undefined : { url } },
      });
      await Promise.all(waits);
      return closed;
    },
    // 打開一頁（導覽）：回傳 worker 給的回應；沒接手（交給瀏覽器自己拿）就是 undefined。
    navigate: async (
      url: string,
      opts: { mode?: string; method?: string; preload?: Promise<unknown> } = {},
    ) => {
      let responded: Promise<Response> | undefined;
      handlers.get("fetch")?.({
        request: { url, mode: opts.mode ?? "navigate", method: opts.method ?? "GET" },
        preloadResponse: opts.preload,
        respondWith: (p: Promise<Response>) => {
          responded = Promise.resolve(p);
        },
      });
      return responded ? await responded : undefined;
    },
  };
}

describe("service worker", () => {
  let sw: ReturnType<typeof boot>;
  beforeEach(() => {
    sw = boot();
  });

  // Guards: REQ-104
  test("安裝就接手：skipWaiting 與 clients.claim", async () => {
    sw.install();
    await sw.activate();
    expect(sw.self.skipWaiting).toHaveBeenCalled();
    expect(sw.self.clients.claim).toHaveBeenCalled();
  });

  // Guards: REQ-28
  test("推播：標題、內文取待辦（沒有就取背景）的前 120 字、tag 是編號", async () => {
    await sw.push({ id: "M-7", title: "要不要訂課", todo: "好".repeat(200), context: "背景" });
    await sw.push({ id: "M-8", title: "做完了", context: "背景說明" });
    expect(sw.shown[0]?.title).toBe("要不要訂課");
    expect(Array.from(sw.shown[0]?.options.body ?? "")).toHaveLength(120);
    expect(sw.shown[0]?.options.tag).toBe("M-7");
    expect(sw.shown[0]?.options.data?.url).toBe("/messages/M-7");
    expect(sw.shown[1]?.options.body).toBe("背景說明");
  });

  // 待辦是「無」（不用做事，notify 預設會補）當作沒有、改用背景：新角色來打招呼只有自我介紹在背景，
  // 手機上不能只看到「無」（NTF-66）。
  test("推播內文：待辦是「無」時用背景", async () => {
    await sw.push({ id: "M-9", title: "露娜 來打招呼", todo: "無", context: "你好，我是露娜。" });
    await sw.push({ id: "M-10", title: "沒有背景", todo: "無" });
    expect(sw.shown[0]?.options.body).toBe("你好，我是露娜。");
    expect(sw.shown[1]?.options.body).toBe("有一則新通知");
  });

  // Guards: REQ-28
  test("圖示只收同源路徑，其他一律用預設圖示", async () => {
    for (const image of [
      "/avatars/code.png",
      "https://evil.example/a.png",
      "//evil.example/a.png",
      "",
      undefined,
    ]) {
      await sw.push({ id: "M-1", title: "x", todo: "y", image });
    }
    expect(sw.shown.map((s) => s.options.icon)).toEqual([
      "/avatars/code.png",
      "/icons/icon-192.png",
      "/icons/icon-192.png",
      "/icons/icon-192.png",
      "/icons/icon-192.png",
    ]);
  });

  // Guards: REQ-28
  test("內容讀不懂就顯示固定的標題與內文", async () => {
    await sw.push(() => {
      throw new Error("壞掉的 JSON");
    });
    await sw.push(undefined);
    for (const s of sw.shown) {
      expect(s.title).toBe("EverGenie 看板");
      expect(s.options.body).toBe("有一則新通知");
    }
  });

  // Guards: REQ-28
  test("測試通知（編號 test）點了開裝置頁", async () => {
    await sw.push({ id: "test", title: "測試通知", context: "這台裝置收得到推播" });
    expect(sw.shown[0]?.options.data?.url).toBe("/settings/devices");
  });

  // 不吵的通知（P7-6、D-34）：提醒的 payload 帶 nudge { tag }，點下去開那個 session 的聊天頁。
  test("提醒：用 nudge 的 tag、舊的還在也會響、點下去開 session 的聊天頁", async () => {
    const nudge = { tag: "nudge-mini-code-sec-12" };
    await sw.push({
      title: "SEC-12 還在等你",
      todo: "把通知做安靜\n請你：請確認",
      url: "/chat/code-sec-12",
      nudge,
    });
    expect(sw.shown[0]?.options.tag).toBe(nudge.tag);
    expect(sw.shown[0]?.options.renotify).toBe(true);
    expect(sw.shown[0]?.options.data?.url).toBe("/chat/code-sec-12");
    expect(sw.shown[0]?.options.body).toContain("請你：請確認");
  });

  // Guards: REQ-227
  test("提醒的網址只收站內路徑，其他退回首頁", async () => {
    // `/\t/evil.com`、`/\n/evil.com` 照字面是站內路徑，但瀏覽器解析會去掉 tab／換行變成 `//evil.com`。
    for (const url of [
      "https://evil.example/x",
      "//evil.example/x",
      undefined,
      "/\t/evil.example/x",
      "/\n/evil.example/x",
    ]) {
      await sw.push({ title: "x", todo: "y", url, nudge: { tag: "t" } });
    }
    expect(sw.shown.map((s) => s.options.data?.url)).toEqual(["/", "/", "/", "/", "/"]);
  });

  test("提醒的站內網址保留路徑、查詢與 #", async () => {
    await sw.push({ title: "x", url: "/chat/s-1?x=1#end", nudge: { tag: "t" } });
    expect(sw.shown[0]?.options.data?.url).toBe("/chat/s-1?x=1#end");
  });

  // Guards: REQ-108
  test("點通知：有開著的看板就聚焦並導向，沒有就開新視窗", async () => {
    const focus = vi.fn(async () => {});
    const navigate = vi.fn(async () => {});
    sw.windows.push({ url: "http://board.test/chat", focus, navigate });
    const closed = await sw.click("/messages/M-7");
    expect(closed).toHaveBeenCalled();
    expect(focus).toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith("/messages/M-7");
    expect(sw.opened).toEqual([]);

    sw.windows.length = 0;
    sw.windows.push({ url: "http://other.test/", focus, navigate });
    await sw.click("/messages/M-8");
    expect(sw.opened).toEqual(["/messages/M-8"]);
  });

  // Guards: REQ-108
  test("導向失敗（navigate 不能用）退回開新視窗", async () => {
    sw.windows.push({
      url: "http://board.test/",
      focus: async () => {},
      navigate: async () => {
        throw new Error("不支援");
      },
    });
    await sw.click("/messages/M-9");
    expect(sw.opened).toEqual(["/messages/M-9"]);
  });

  // 多台（S3-8）：推播帶 home（送出那台的 label）。自己這台的跟 S3 之前完全一樣；別台的網址帶 ?home、tag 分開、不用那台的圖示。
  test("自己這台的推播（home＝這個看板）：網址、tag、圖示都跟以前一樣", async () => {
    await sw.push({
      id: "M-7",
      title: "x",
      todo: "y",
      image: "/avatars/code.png",
      home: "amy-mini",
    });
    expect(sw.shown[0]?.options).toMatchObject({
      tag: "M-7",
      icon: "/avatars/code.png",
      data: { url: "/messages/M-7" },
    });
  });

  test("別台的訊息推播：點了開那台的訊息（?home=），tag 加台名，圖示用預設", async () => {
    await sw.push({
      id: "M-7",
      title: "x",
      todo: "y",
      image: "/avatars/code.png",
      home: "amy-office",
    });
    expect(sw.shown[0]?.options).toMatchObject({
      tag: "amy-office|M-7",
      icon: "/icons/icon-192.png",
      data: { url: "/messages/M-7?home=amy-office" },
    });
    // home 照原字串用（開發家帶 @dev），網址照規矩編碼。
    await sw.push({ id: "M-1", title: "x", home: "amy-office@dev" });
    expect(sw.shown[1]?.options.data?.url).toBe("/messages/M-1?home=amy-office%40dev");
  });

  test("別台的提醒：點了開那台的聊天（原本的查詢字串保留）", async () => {
    const nudge = { tag: "nudge-code-sec-12" };
    await sw.push({ title: "t", url: "/chat/code-sec-12?x=1", nudge, home: "amy-office" });
    expect(sw.shown[0]?.options.tag).toBe("amy-office|nudge-code-sec-12");
    expect(sw.shown[0]?.options.data?.url).toBe("/chat/code-sec-12?x=1&home=amy-office");
    // 別台的提醒網址也只收站內路徑。
    await sw.push({ title: "t", url: "https://evil.example/x", nudge, home: "amy-office" });
    expect(sw.shown.at(-1)?.options.data?.url).toBe("/?home=amy-office");
  });

  test("問不到自己是哪一台：有 home 就當別台（帶 ?home 一樣開得對），之後再問", async () => {
    sw = boot(null);
    await sw.push({ id: "M-7", title: "x", home: "amy-mini" });
    expect(sw.shown[0]?.options.data?.url).toBe("/messages/M-7?home=amy-mini");
    await sw.push({ id: "M-8", title: "x", home: "amy-mini" });
    expect(sw.self.fetch).toHaveBeenCalledTimes(2);
  });

  test("沒帶 home（S3 之前的版本）：不問自己是誰，照以前", async () => {
    await sw.push({ id: "M-7", title: "x" });
    expect(sw.shown[0]?.options.data?.url).toBe("/messages/M-7");
    expect(sw.self.fetch).not.toHaveBeenCalled();
  });

  test("自己是誰只問一次", async () => {
    await sw.push({ id: "M-1", title: "x", home: "amy-office" });
    await sw.push({ id: "M-2", title: "x", home: "amy-office" });
    expect(sw.self.fetch).toHaveBeenCalledTimes(1);
  });

  // DEV-156 U-21、WEB-185：連不上時回內建的離線頁；正常時照常走網路，不快取任何東西。
  describe("離線頁", () => {
    const page = "http://board.test/tasks";
    const ok = { status: 200, marker: "network" };

    test("正常時照常走網路，回的就是網路那份", async () => {
      sw.self.fetch.mockImplementationOnce(async () => ok as never);
      expect(await sw.navigate(page)).toBe(ok);
    });

    test("有預先載入就用它，不再多打一次", async () => {
      const res = await sw.navigate(page, { preload: Promise.resolve(ok) });
      expect(res).toBe(ok);
      expect(sw.self.fetch).not.toHaveBeenCalled();
    });

    test("網路失敗或轉發回 502／503／504：回離線頁，連上會自動回來", async () => {
      sw.self.fetch.mockImplementationOnce(async () => {
        throw new Error("offline");
      });
      const down = await sw.navigate(page);
      expect(down?.status).toBe(503);
      expect(down?.headers.get("cache-control")).toBe("no-store");
      const html = await down?.text();
      expect(html).toContain("連不上看板");
      expect(html).toContain("會自己重開，通常一分鐘內就好");
      expect(html).toContain("等了兩三分鐘還是這樣");
      expect(html).toContain("搜尋「evergenie 修復」打開它");
      expect(html).toContain("location.reload()");
      for (const status of [502, 503, 504]) {
        sw.self.fetch.mockImplementationOnce(async () => ({ status }) as never);
        expect((await sw.navigate(page))?.status).toBe(503);
      }
      // 預先載入失敗也一樣
      expect((await sw.navigate(page, { preload: Promise.reject(new Error("x")) }))?.status).toBe(
        503,
      );
    });

    test("看板自己回的 404、500 照原樣（那是連得上）", async () => {
      for (const status of [404, 500]) {
        const res = { status };
        sw.self.fetch.mockImplementationOnce(async () => res as never);
        expect(await sw.navigate(page)).toBe(res);
      }
    });

    test("API、圖與程式檔、別的網站、不是 GET 的都不接手", async () => {
      expect(await sw.navigate("http://board.test/api/files/x")).toBeUndefined();
      expect(
        await sw.navigate("http://board.test/assets/a.js", { mode: "no-cors" }),
      ).toBeUndefined();
      expect(await sw.navigate("http://board.test/api/x", { mode: "cors" })).toBeUndefined();
      expect(await sw.navigate("https://evil.test/")).toBeUndefined();
      expect(await sw.navigate(page, { method: "POST" })).toBeUndefined();
      expect(sw.self.fetch).not.toHaveBeenCalled();
    });

    test("原文沒有用到快取（Cache Storage）", () => {
      expect(swSource).not.toMatch(/caches\.|CacheStorage|cache\.put/);
    });
  });
});
