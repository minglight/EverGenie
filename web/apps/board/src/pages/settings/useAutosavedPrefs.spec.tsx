import { api, type NotifyPrefs } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { notifyPrefsQuery } from "./queries";
import { useAutosavedPrefs } from "./useAutosavedPrefs";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const start: NotifyPrefs = { quiet_hours: "" } as NotifyPrefs;
  client.setQueryData(notifyPrefsQuery.queryKey, start);
  vi.spyOn(api.notify, "getPrefs").mockResolvedValue({ prefs: start } as never);
  const set = vi
    .spyOn(api.notify, "setPrefs")
    .mockImplementation(async (req) => ({ prefs: req.prefs }) as never);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { set, ...renderHook(() => useAutosavedPrefs(), { wrapper }) };
}

describe("設定自動存", () => {
  // NTF-64：改了安靜時段、600ms 內就換分頁離開，那一格照樣存（以前計時器被清掉、改的值默默不見）。
  test("離開頁面時還沒存的那一格立刻存", async () => {
    const { result, unmount, set } = setup();
    act(() => result.current.edit("quiet", (p) => ({ ...p, quiet_hours: "23:00-07:00" })));
    expect(set).not.toHaveBeenCalled();
    unmount();
    await act(async () => {});
    expect(set).toHaveBeenCalledTimes(1);
    expect(set.mock.calls[0]?.[0].prefs?.quiet_hours).toBe("23:00-07:00");
  });

  // NTF-65：離開時補存的那次還沒回來，人就回到頁面改別格：畫面接著剛改的值、等補存回來才送，送出的那份不會把那格蓋回舊值。
  test("補存還沒回來就回頁面改別格，不會把剛補存的那格蓋回去", async () => {
    vi.useFakeTimers();
    try {
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      const start = { quiet_hours: "", preset: "quiet" } as NotifyPrefs;
      client.setQueryData(notifyPrefsQuery.queryKey, start);
      vi.spyOn(api.notify, "getPrefs").mockResolvedValue({ prefs: start } as never);
      let release: () => void = () => {};
      const sent: NotifyPrefs[] = [];
      const set = vi.spyOn(api.notify, "setPrefs").mockImplementation(async (req) => {
        sent.push(req.prefs as NotifyPrefs);
        // 第一次（離開時補存）卡住，等測試放行。
        if (sent.length === 1) await new Promise<void>((r) => (release = r));
        return { prefs: req.prefs } as never;
      });
      const wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      );
      const first = renderHook(() => useAutosavedPrefs(), { wrapper });
      act(() => first.result.current.edit("quiet", (p) => ({ ...p, quiet_hours: "23:00-07:00" })));
      first.unmount();
      await act(async () => {});
      expect(set).toHaveBeenCalledTimes(1);

      // 回到頁面：補存還沒回來，畫面上是剛改的值。
      const again = renderHook(() => useAutosavedPrefs(), { wrapper });
      expect(again.result.current.prefs?.quiet_hours).toBe("23:00-07:00");
      act(() => again.result.current.edit("preset", (p) => ({ ...p, preset: "busy" })));
      await act(async () => {
        vi.advanceTimersByTime(1000);
      });
      // 補存還沒回來：第二份排隊，不同時送。
      expect(set).toHaveBeenCalledTimes(1);
      await act(async () => {
        release();
      });
      expect(set).toHaveBeenCalledTimes(2);
      expect(sent[1]?.quiet_hours).toBe("23:00-07:00");
      expect(sent[1]?.preset).toBe("busy");
      again.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  test("沒改東西就離開不送", async () => {
    const { unmount, set } = setup();
    unmount();
    await act(async () => {});
    expect(set).not.toHaveBeenCalled();
  });
});
