import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import {
  keysForBusEvent,
  LIVE_TOPICS,
  REOPEN_MAX_MS,
  REOPEN_MS,
  useLiveSites,
  useLiveUpdates,
} from "./live";
import { taskQuery, tasksQuery, taskViews } from "./queries";
import { PRIMARY, type SiteRef } from "./sites";
import { pingPresence } from "./viewing";

type OnItem = (ev: Record<string, unknown>) => void;
const api = vi.hoisted(() => {
  const remote = vi.fn();
  // 別台（makeApi(base)）：同一組 mock，第一個參數多帶 base、第二個是哪一條。
  const remoteApi = (base: string) => ({
    session: { watchThreads: (...a: unknown[]) => remote(base, "threads", ...a) },
    notify: { watch: (...a: unknown[]) => remote(base, "notify", ...a) },
    bus: { subscribe: (...a: unknown[]) => remote(base, "bus", ...a) },
  });
  return {
    watchThreads: vi.fn(),
    notifyWatch: vi.fn(),
    busSubscribe: vi.fn(),
    emitVaultChanged: vi.fn(),
    remote,
    remoteApi,
  };
});
vi.mock("@evergenie/sdk-web", () => ({
  ApiError: Error,
  api: {
    session: { watchThreads: api.watchThreads },
    notify: { watch: api.notifyWatch },
    bus: { subscribe: api.busSubscribe },
    system: { getMode: () => new Promise(() => {}) },
    web: { getSites: () => new Promise(() => {}) },
  },
  makeApi: api.remoteApi,
  emitVaultChanged: api.emitVaultChanged,
  ALL_KINDS: "*",
}));
vi.mock("./viewing", () => ({ pingPresence: vi.fn() }));

const pending = () => new Promise<void>(() => {});
let emitBus: OnItem | undefined;
let busReconnect: (() => void) | undefined;

beforeEach(() => {
  vi.useFakeTimers();
  api.watchThreads.mockReset().mockImplementation(pending);
  api.notifyWatch.mockReset().mockImplementation(pending);
  api.remote.mockReset().mockImplementation(pending);
  vi.mocked(pingPresence).mockClear();
  api.busSubscribe
    .mockReset()
    .mockImplementation((_req: unknown, onItem: OnItem, _s: unknown, onReconnect: () => void) => {
      emitBus = onItem;
      busReconnect = onReconnect;
      return pending();
    });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function mount() {
  const client = new QueryClient();
  const spy = vi.spyOn(client, "invalidateQueries").mockResolvedValue();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  renderHook(() => useLiveUpdates(), { wrapper });
  const keys = () => spy.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
  return { spy, keys };
}

describe("顯示狀態的資料不輪詢、改推送（S8）", () => {
  // 看板列表、任務列：沒有超過 3 秒的輪詢（改由推送重抓）。待辦、權限申請的查詢在各自頁面資料夾的
  // queries.spec.ts 檢查，工作對話的快取倒數在 pages/chat/threads.spec.ts（FEN-27：共用層不 import 頁面）。
  // Guards: REQ-389
  test("列出的 query 沒有超過 3 秒的 refetchInterval", () => {
    const opts = [...taskViews.map((v) => tasksQuery(v)), taskQuery("SEC-1")];
    for (const o of opts) {
      const every = (o as { refetchInterval?: unknown }).refetchInterval;
      expect(
        every === undefined || every === false || (typeof every === "number" && every <= 3_000),
        `${JSON.stringify(o.queryKey)} refetchInterval=${String(every)}`,
      ).toBe(true);
    }
  });

  // 看板只訂記錄變化（vault.changed）一個主題；記錄看種類重抓對應的清單與單頁；去抖 500ms 合成一次。
  // Guards: REQ-389
  test("vault.changed 依種類重抓；其他的不理", async () => {
    const { spy, keys } = mount();
    expect(api.busSubscribe).toHaveBeenCalledTimes(1);
    expect(api.busSubscribe.mock.calls[0]?.[0]).toEqual({ topics: LIVE_TOPICS });
    expect(LIVE_TOPICS).toEqual(["vault.changed"]);

    act(() => {
      emitBus?.({
        topic: "vault.changed",
        payload: { kind: "task", id: "SEC-1", type: "updated" },
      });
      emitBus?.({
        topic: "vault.changed",
        payload: { kind: "task", id: "SEC-2", type: "updated" },
      });
    });
    expect(spy).not.toHaveBeenCalled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    expect(keys().sort()).toEqual(['["menu"]', '["overview"]', '["task"]', '["tasks"]']);

    spy.mockClear();
    act(() => {
      emitBus?.({ topic: "vault.changed", payload: { kind: "message", id: "m1" } });
      emitBus?.({ topic: "something.else", payload: {} });
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    expect(spy).not.toHaveBeenCalled();

    // 斷線重連回來：bus 管的資料全部重抓一次（斷掉時漏掉的變化）。
    act(() => busReconnect?.());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    for (const k of ['["tasks"]', '["reminders"]', '["policy"]']) expect(keys()).toContain(k);
  });

  // Guards: REQ-389
  test("keysForBusEvent：待辦、權限申請、Goal 各自對到看板的 key", () => {
    const kinds = (kind: string) =>
      keysForBusEvent({ topic: "vault.changed", payload: { kind } }).map((k) => JSON.stringify(k));
    expect(kinds("reminder")).toEqual(expect.arrayContaining(['["reminders"]', '["reminder"]']));
    expect(kinds("policy_request")).toEqual(expect.arrayContaining(['["policy"]']));
    expect(kinds("goal")).toEqual(expect.arrayContaining(['["goals"]', '["goal"]']));
    expect(keysForBusEvent({ topic: "vault.changed", payload: {} })).toEqual([]);
  });

  // 套件的記錄（飲食、運動、身體…）變了：轉給套件頁面自己重讀，秘書在聊天裡記的一餐不用重新整理就看得到（PKG-49）。
  test("TestPackageRecordChangeIsForwardedToPackagePages", () => {
    api.emitVaultChanged.mockClear();
    mount();
    act(() => {
      emitBus?.({ topic: "vault.changed", payload: { kind: "nutrition.meal", id: "M-7" } });
      emitBus?.({ topic: "something.else", payload: { kind: "nutrition.meal" } });
      emitBus?.({ topic: "vault.changed", payload: {} });
    });
    expect(api.emitVaultChanged.mock.calls).toEqual([[{ kind: "nutrition.meal", id: "M-7" }]]);
    // 串流斷過又接上：套件頁面全部重讀一次（斷掉時秘書記的東西）。
    api.emitVaultChanged.mockClear();
    act(() => busReconnect?.());
    expect(api.emitVaultChanged.mock.calls).toEqual([[{ kind: "*" }]]);
  });
});

describe("每台各自一組串流（S3-8）", () => {
  const OFFICE: SiteRef = { key: "amy-office", base: "https://genie-amy-office.example.ts.net" };

  // 別台的串流：記下每條的 onItem／onReconnect／signal，測試可以假裝那台送事件、重連、拒絕。
  interface Remote {
    onItem: OnItem;
    onReconnect: () => void;
    signal: AbortSignal;
    fail: (err: unknown) => void;
  }
  let remotes: Map<string, Remote[]>;
  beforeEach(() => {
    remotes = new Map();
    api.remote.mockImplementation(
      (
        _base: string,
        kind: string,
        _req: unknown,
        onItem: OnItem,
        signal: AbortSignal,
        onReconnect: () => void,
      ) =>
        new Promise<void>((_resolve, reject) => {
          const list = remotes.get(kind) ?? [];
          list.push({ onItem, onReconnect, signal, fail: reject });
          remotes.set(kind, list);
        }),
    );
  });
  const last = (kind: string) => remotes.get(kind)?.at(-1);

  function mountSites(initial: SiteRef[]) {
    const client = new QueryClient();
    const spy = vi.spyOn(client, "invalidateQueries").mockResolvedValue();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const view = renderHook(({ sites }: { sites: SiteRef[] }) => useLiveSites(sites), {
      wrapper,
      initialProps: { sites: initial },
    });
    const keys = () => spy.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
    return { spy, keys, rerender: (sites: SiteRef[]) => view.rerender({ sites }) };
  }
  const settle = () =>
    act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });

  test('每台開一組；別台的事件只重抓那台的 key（["site", 台名, …]）', async () => {
    const { spy, keys } = mountSites([PRIMARY, OFFICE]);
    await act(async () => {});
    expect(api.busSubscribe).toHaveBeenCalledTimes(1);
    expect(api.remote.mock.calls.map((c) => [c[0], c[1]]).sort()).toEqual(
      [
        [OFFICE.base, "bus"],
        [OFFICE.base, "notify"],
        [OFFICE.base, "threads"],
      ].sort(),
    );
    act(() => last("bus")?.onItem({ topic: "vault.changed", payload: { kind: "task" } }));
    await settle();
    expect(keys().sort()).toEqual(
      [
        '["site","amy-office","menu"]',
        '["site","amy-office","overview"]',
        '["site","amy-office","task"]',
        '["site","amy-office","tasks"]',
      ].sort(),
    );
    spy.mockClear();
    // 主伺服器的事件只動主伺服器的 key（跟 S3 之前一樣，沒有前綴）。
    act(() => emitBus?.({ topic: "vault.changed", payload: { kind: "goal" } }));
    await settle();
    expect(keys().every((k) => !k.includes("site"))).toBe(true);
    expect(keys()).toContain('["goals"]');
  });

  test("別台通知串流說提醒有變動：只重報那台的在場", async () => {
    mountSites([PRIMARY, OFFICE]);
    await act(async () => {});
    act(() => last("notify")?.onItem({ nudge: { id: "n" } }));
    expect(pingPresence).toHaveBeenCalledWith(OFFICE.key);
  });

  test("別台重連成功：只重抓那台的資料，主伺服器的串流不動", async () => {
    const { keys } = mountSites([PRIMARY, OFFICE]);
    await act(async () => {});
    act(() => last("threads")?.onReconnect());
    await settle();
    expect(keys().length).toBeGreaterThan(0);
    expect(keys().every((k) => k.startsWith('["site","amy-office"'))).toBe(true);
    expect(api.watchThreads).toHaveBeenCalledTimes(1);
    // 通知串流斷過：可能漏了提醒的變動，也重報一次那台的在場。
    expect(pingPresence).not.toHaveBeenCalled();
    act(() => last("notify")?.onReconnect());
    expect(pingPresence).toHaveBeenCalledWith(OFFICE.key);
  });

  test("某台不能用了：只關那台的串流；回來時重開並把那台的資料重抓一次", async () => {
    const { keys, spy, rerender } = mountSites([PRIMARY, OFFICE]);
    await act(async () => {});
    const first = last("bus");
    rerender([PRIMARY]);
    await act(async () => {});
    expect(first?.signal.aborted).toBe(true);
    expect(api.watchThreads).toHaveBeenCalledTimes(1);
    expect(api.watchThreads.mock.calls[0]?.[2]?.aborted).toBe(false);

    spy.mockClear();
    rerender([PRIMARY, OFFICE]);
    await act(async () => {});
    expect(remotes.get("bus")).toHaveLength(2);
    expect(pingPresence).toHaveBeenCalledWith(OFFICE.key);
    await settle();
    expect(keys()).toContain('["site","amy-office","tasks"]');
    expect(keys().every((k) => k.startsWith('["site","amy-office"'))).toBe(true);
  });

  test("同樣的台清單（每次新陣列）不會重開串流", async () => {
    const { rerender } = mountSites([PRIMARY, OFFICE]);
    await act(async () => {});
    rerender([{ ...PRIMARY }, { ...OFFICE }]);
    await act(async () => {});
    expect(api.busSubscribe).toHaveBeenCalledTimes(1);
    expect(remotes.get("bus")).toHaveLength(1);
  });

  // 別台的串流被拒絕（讀得到的錯誤）是真的請求失敗：重新試連那台，還能用就隔 REOPEN_MS 再開、開之前先重抓。
  test("別台的串流被拒絕：重新試連那台，隔一段時間再開；主伺服器被拒照舊不重開", async () => {
    const { keys } = mountSites([PRIMARY, OFFICE]);
    await act(async () => {});
    await act(async () => {
      last("bus")?.fail(new Error("forbidden"));
    });
    expect(keys()).toContain(JSON.stringify(["site-probe", OFFICE.base]));
    expect(keys()).not.toContain(JSON.stringify(["site", OFFICE.key, "tasks"]));
    expect(remotes.get("bus")).toHaveLength(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REOPEN_MS);
    });
    expect(remotes.get("bus")).toHaveLength(2);
    // 重開之前先把那台 bus 管的資料重抓一次（被拒的這段時間可能漏了事件；照樣經過 500ms 去抖）。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    expect(keys()).toContain(JSON.stringify(["site", OFFICE.key, "tasks"]));
  });

  // 一開就被拒（例如那台不提供這條）不要每 10 秒重抓整台：10、30、60 秒，之後每 60 秒。
  test("別台的串流一直被拒：越等越久再開", async () => {
    mountSites([PRIMARY, OFFICE]);
    await act(async () => {});
    const failAndWait = async (ms: number) => {
      await act(async () => {
        last("bus")?.fail(new Error("not_found"));
      });
      const before = remotes.get("bus")?.length ?? 0;
      await act(async () => {
        await vi.advanceTimersByTimeAsync(ms - 1);
      });
      expect(remotes.get("bus")).toHaveLength(before);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
      });
      expect(remotes.get("bus")).toHaveLength(before + 1);
    };
    await failAndWait(REOPEN_MS);
    await failAndWait(30_000);
    await failAndWait(REOPEN_MAX_MS);
    await failAndWait(REOPEN_MAX_MS);
  });

  test("主伺服器的串流被拒絕：安靜，不重開（跟 S3 之前一樣）", async () => {
    api.busSubscribe.mockReset().mockRejectedValue(new Error("forbidden"));
    mountSites([PRIMARY]);
    await act(async () => {});
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REOPEN_MS * 3);
    });
    expect(api.busSubscribe).toHaveBeenCalledTimes(1);
  });

  test("useLiveUpdates：只有一台時跟 S3 之前一樣只開主伺服器那組", async () => {
    mount();
    await act(async () => {});
    expect(api.watchThreads).toHaveBeenCalledTimes(1);
    expect(api.notifyWatch).toHaveBeenCalledTimes(1);
    expect(api.busSubscribe).toHaveBeenCalledTimes(1);
    expect(api.remote).not.toHaveBeenCalled();
  });
});
