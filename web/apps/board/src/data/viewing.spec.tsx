import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { PRIMARY } from "./sites";
import {
  closeNudgePushes,
  ELSEWHERE,
  isNudgeTag,
  nudgeIsHere,
  PRESENCE_EVERY_MS,
  pingPresence,
  presenceTargets,
  resolveNudge,
  useNudges,
  usePresenceReporter,
} from "./viewing";

const api = vi.hoisted(() => ({
  presence: vi.fn(),
  resolveNudge: vi.fn(),
  remote: vi.fn(),
}));
vi.mock("@evergenie/sdk-web", () => ({
  ApiError: class extends Error {},
  api: { notify: { presence: api.presence, resolveNudge: api.resolveNudge } },
  // 別台的 api：記下是送到哪一台（base）。
  makeApi: (base: string) => ({
    notify: {
      presence: (req: unknown) => api.remote(base, "presence", req),
      resolveNudge: (req: unknown) => api.remote(base, "resolveNudge", req),
    },
  }),
}));

const nudge = (over: Record<string, unknown> = {}) => ({
  id: "nudge-1",
  session: "code-sec-12",
  task: "SEC-12",
  title: "SEC-12 還在等你",
  todo: "請回覆",
  surface: "inline",
  resolved: false,
  ...over,
});

let visibility: "visible" | "hidden" = "visible";
function setVisibility(v: "visible" | "hidden") {
  visibility = v;
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  vi.useFakeTimers();
  visibility = "visible";
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });
  api.presence.mockReset().mockResolvedValue({ nudges: [] });
  api.resolveNudge.mockReset().mockResolvedValue({});
  api.remote.mockReset().mockResolvedValue({ nudges: [] });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const calls = () => api.presence.mock.calls.map((c) => c[0] as Record<string, unknown>);

describe("看板回報正在看哪裡（P7-6、D-34）", () => {
  // Guards: REQ-228
  test("前景：馬上報一次、之後每 30 秒報一次；帶路徑與討論串；每個分頁有自己的編號", async () => {
    renderHook(() => usePresenceReporter("/chat/code-sec-12", "code-sec-12"));
    await act(async () => {});
    expect(calls()).toHaveLength(1);
    expect(calls()[0]).toMatchObject({
      path: "/chat/code-sec-12",
      thread: "code-sec-12",
      away: false,
    });
    expect(String(calls()[0]?.client)).toMatch(/^c-/);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(PRESENCE_EVERY_MS);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(PRESENCE_EVERY_MS);
    });
    expect(calls()).toHaveLength(3);
    expect(PRESENCE_EVERY_MS).toBe(30_000);
    expect(new Set(calls().map((c) => c.client)).size).toBe(1);
  });

  // Guards: REQ-228
  test("換頁馬上重報（不等 30 秒）；沒有討論串就帶空字串", async () => {
    const { rerender } = renderHook(
      ({ path, thread }: { path: string; thread?: string }) => usePresenceReporter(path, thread),
      {
        initialProps: { path: "/chat/code-sec-12", thread: "code-sec-12" } as {
          path: string;
          thread?: string;
        },
      },
    );
    await act(async () => {});
    rerender({ path: "/tasks", thread: undefined });
    await act(async () => {});
    expect(calls()).toHaveLength(2);
    expect(calls()[1]).toMatchObject({ path: "/tasks", thread: "", away: false });
  });

  // Guards: REQ-228
  test("切到背景：告訴伺服器不在了、不再每 30 秒報；回到前景馬上報", async () => {
    renderHook(() => usePresenceReporter("/", undefined));
    await act(async () => {});
    act(() => setVisibility("hidden"));
    await act(async () => {});
    expect(calls().at(-1)).toMatchObject({ away: true });
    const n = calls().length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(PRESENCE_EVERY_MS * 3);
    });
    expect(calls()).toHaveLength(n);
    act(() => setVisibility("visible"));
    await act(async () => {});
    expect(calls().at(-1)).toMatchObject({ away: false });
  });

  // Guards: REQ-228
  test("伺服器回的提醒清單就是畫面要顯示的；按「知道了」先從畫面拿掉再通知伺服器", async () => {
    api.presence.mockResolvedValue({ nudges: [nudge()] });
    const { result } = renderHook(() => {
      usePresenceReporter("/reminders", undefined);
      return useNudges();
    });
    await act(async () => {});
    expect(result.current.map((n) => n.id)).toEqual(["nudge-1"]);
    api.presence.mockResolvedValue({ nudges: [] });
    await act(async () => {
      await resolveNudge("nudge-1");
    });
    expect(api.resolveNudge).toHaveBeenCalledWith({ id: "nudge-1" });
    expect(result.current).toEqual([]);
  });

  // Guards: REQ-228
  test("連不上伺服器不丟錯、不打擾（下一次再報）", async () => {
    api.presence.mockRejectedValue(new Error("offline"));
    renderHook(() => usePresenceReporter("/", undefined));
    await act(async () => {});
    await act(async () => {
      await vi.advanceTimersByTimeAsync(PRESENCE_EVERY_MS);
    });
    expect(calls().length).toBeGreaterThanOrEqual(2);
  });
});

describe("手機通知列的提醒推播（DEV-123）", () => {
  const stubNotifications = (tags: string[]) => {
    const closed: string[] = [];
    const getNotifications = vi.fn(async () =>
      tags.map((tag) => ({ tag, close: () => closed.push(tag) })),
    );
    Object.defineProperty(navigator, "serviceWorker", {
      configurable: true,
      value: { getRegistration: async () => ({ getNotifications }) },
    });
    return closed;
  };
  afterEach(() => {
    Reflect.deleteProperty(navigator, "serviceWorker");
  });

  test("只認提醒的 tag：這台的、別台的算，一般訊息不算", () => {
    expect(isNudgeTag("nudge-code-sec-12")).toBe(true);
    expect(isNudgeTag("amy-office|nudge-code-pending:P1")).toBe(true);
    expect(isNudgeTag("M-7")).toBe(false);
    expect(isNudgeTag("amy-office|M-7")).toBe(false);
  });

  test("看板回到前景就收掉提醒推播，一般訊息留著", async () => {
    const closed = stubNotifications(["nudge-code", "amy-mini|nudge-code-pending:P1", "M-7"]);
    renderHook(() => usePresenceReporter("/", undefined));
    await act(async () => {});
    expect(closed).toEqual(["nudge-code", "amy-mini|nudge-code-pending:P1"]);
    closed.length = 0;
    act(() => setVisibility("hidden"));
    await act(async () => {});
    expect(closed).toEqual([]);
    act(() => setVisibility("visible"));
    await act(async () => {});
    expect(closed).toEqual(["nudge-code", "amy-mini|nudge-code-pending:P1"]);
  });

  test("瀏覽器沒有這些 API 不出錯", async () => {
    await expect(closeNudgePushes()).resolves.toBeUndefined();
  });
});

describe("報給多台（S3-8）", () => {
  const OFFICE = { key: "amy-office", base: "https://genie-amy-office.example.ts.net" };
  const remote = (method: string) =>
    api.remote.mock.calls
      .filter((c) => c[0] === OFFICE.base && c[1] === method)
      .map((c) => c[2] as Record<string, unknown>);

  // 看板開著，每台都知道主人在看板前（提醒才出在看板、不推到手機）；正在看的那筆所屬的台拿真的路徑，
  // 其他台拿 ELSEWHERE——兩台都有 code-sec-12，不能讓 A 以為主人在看 A 的同名討論串。
  test("每台能用的台都報；正在看的那台拿真的路徑與討論串，其他台拿 ELSEWHERE", async () => {
    renderHook(() =>
      usePresenceReporter("/chat/code-sec-12", "code-sec-12", OFFICE.key, [PRIMARY, OFFICE]),
    );
    await act(async () => {});
    expect(calls()).toEqual([
      expect.objectContaining({ path: ELSEWHERE, thread: "", away: false }),
    ]);
    expect(remote("presence")).toEqual([
      expect.objectContaining({ path: "/chat/code-sec-12", thread: "code-sec-12", away: false }),
    ]);
  });

  test("沒有 ?home（看的是主伺服器的）：主伺服器拿真的路徑，別台拿 ELSEWHERE", () => {
    expect(presenceTargets("/tasks/SEC-1", "", "", [PRIMARY, OFFICE])).toEqual([
      { site: PRIMARY, path: "/tasks/SEC-1", thread: "" },
      { site: OFFICE, path: ELSEWHERE, thread: "" },
    ]);
    // ?home 指的台不在能用的台裡（連不上）：每台都拿 ELSEWHERE，不會誤報到別台。
    expect(presenceTargets("/tasks/SEC-1", "", "amy-down", [PRIMARY, OFFICE])).toEqual([
      { site: PRIMARY, path: ELSEWHERE, thread: "" },
      { site: OFFICE, path: ELSEWHERE, thread: "" },
    ]);
  });

  test("每台的提醒記著是哪一台；「知道了」只送那則所屬的台；通知串流說有變動只重報那台", async () => {
    api.presence.mockResolvedValue({ nudges: [nudge({ id: "a" })] });
    api.remote.mockResolvedValue({ nudges: [nudge({ id: "b" })] });
    const { result } = renderHook(() => {
      usePresenceReporter("/reminders", undefined, "", [PRIMARY, OFFICE]);
      return useNudges();
    });
    await act(async () => {});
    expect(result.current.map((n) => [n.id, n.site])).toEqual([
      ["a", ""],
      ["b", OFFICE.key],
    ]);

    api.remote.mockResolvedValue({ nudges: [] });
    await act(async () => {
      await resolveNudge("b", OFFICE.key);
    });
    expect(api.resolveNudge).not.toHaveBeenCalled();
    expect(api.remote).toHaveBeenCalledWith(OFFICE.base, "resolveNudge", { id: "b" });
    expect(result.current.map((n) => n.id)).toEqual(["a"]);

    const before = calls().length;
    const officeBefore = remote("presence").length;
    await act(async () => {
      pingPresence(OFFICE.key);
    });
    expect(calls()).toHaveLength(before);
    expect(remote("presence")).toHaveLength(officeBefore + 1);
  });

  test("某台連不上：那台上次的提醒留著，其他台照常更新", async () => {
    api.presence.mockResolvedValue({ nudges: [nudge({ id: "a" })] });
    api.remote.mockResolvedValue({ nudges: [nudge({ id: "b" })] });
    const { result } = renderHook(() => {
      usePresenceReporter("/", undefined, "", [PRIMARY, OFFICE]);
      return useNudges();
    });
    await act(async () => {});
    api.remote.mockRejectedValue(new Error("offline"));
    api.presence.mockResolvedValue({ nudges: [] });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(PRESENCE_EVERY_MS);
    });
    expect(result.current.map((n) => n.id)).toEqual(["b"]);
  });

  test("那台不再是能用的台：它的提醒從畫面拿掉、告訴它一聲不在了，之後不再報給它", async () => {
    api.remote.mockResolvedValue({ nudges: [nudge({ id: "b" })] });
    const { result, rerender } = renderHook(
      ({ sites }: { sites: { key: string; base: string }[] }) => {
        usePresenceReporter("/", undefined, "", sites);
        return useNudges();
      },
      { initialProps: { sites: [PRIMARY, OFFICE] } },
    );
    await act(async () => {});
    expect(result.current.map((n) => n.id)).toEqual(["b"]);
    const officeBefore = remote("presence").length;
    rerender({ sites: [PRIMARY] });
    await act(async () => {});
    expect(result.current).toEqual([]);
    // 不然那台要等 90 秒才知道主人不在看板前，提醒不會改推到手機。
    expect(remote("presence").slice(officeBefore)).toEqual([
      expect.objectContaining({ away: true }),
    ]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(PRESENCE_EVERY_MS);
    });
    expect(remote("presence")).toHaveLength(officeBefore + 1);
  });

  test("切到背景之後才回來的在場回應不會把提醒填回去", async () => {
    let answer: (v: { nudges: unknown[] }) => void = () => {};
    api.remote.mockImplementation((_base: string, method: string) =>
      method === "presence"
        ? new Promise((resolve) => {
            answer = resolve;
          })
        : Promise.resolve({}),
    );
    const { result } = renderHook(() => {
      usePresenceReporter("/", undefined, "", [PRIMARY, OFFICE]);
      return useNudges();
    });
    await act(async () => {});
    const late = answer;
    act(() => setVisibility("hidden"));
    await act(async () => {
      late({ nudges: [nudge({ id: "b" })] });
    });
    expect(result.current).toEqual([]);
  });

  test("同樣內容的台清單（每次新陣列）不會重報", async () => {
    const { rerender } = renderHook(
      ({ sites }: { sites: { key: string; base: string }[] }) =>
        usePresenceReporter("/", undefined, "", sites),
      { initialProps: { sites: [PRIMARY, OFFICE] } },
    );
    await act(async () => {});
    rerender({ sites: [{ ...PRIMARY }, { ...OFFICE }] });
    await act(async () => {});
    expect(calls()).toHaveLength(1);
  });

  test("分頁關掉：每台各送一次 keepalive 的「不在了」", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response(null));
    vi.stubGlobal("fetch", fetchMock);
    try {
      renderHook(() => usePresenceReporter("/", undefined, "", [PRIMARY, OFFICE]));
      await act(async () => {});
      window.dispatchEvent(new Event("pagehide"));
      expect(fetchMock.mock.calls.map((c) => c[0])).toEqual([
        "/api/notify/presence",
        `${OFFICE.base}/api/notify/presence`,
      ]);
      expect(fetchMock.mock.calls.every((c) => c[1]?.keepalive === true)).toBe(true);
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

describe("提醒顯示在哪", () => {
  // Guards: REQ-228
  test("在那個討論串或那張單的頁面是頁內卡片，其他頁是站內提示", () => {
    const n = { session: "code-sec-12", task: "SEC-12" };
    expect(nudgeIsHere(n, "/chat/code-sec-12")).toBe(true);
    expect(nudgeIsHere(n, "/tasks/SEC-12")).toBe(true);
    expect(nudgeIsHere(n, "/tasks/SEC-13")).toBe(false);
    expect(nudgeIsHere(n, "/chat/code")).toBe(false);
    expect(nudgeIsHere(n, "/")).toBe(false);
    expect(nudgeIsHere({ session: "", task: "" }, "/chat/")).toBe(false);
  });

  // 多台（S3-8）：別台同名的討論串不算「在那一頁」。
  test("在那一頁要同一台：別台的 code-sec-12 不算主伺服器的 /chat/code-sec-12", () => {
    const office = { session: "code-sec-12", task: "SEC-12", site: "amy-office" };
    expect(nudgeIsHere(office, "/chat/code-sec-12")).toBe(false);
    expect(nudgeIsHere(office, "/chat/code-sec-12", "amy-office")).toBe(true);
    expect(
      nudgeIsHere({ session: "code-sec-12", site: "" }, "/chat/code-sec-12", "amy-office"),
    ).toBe(false);
  });
});
