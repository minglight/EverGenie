import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import type { Site } from "../../data/sites";
import { DevicesSettings } from "./DevicesSettings";

const state = vi.hoisted(() => ({
  sites: [] as Site[],
  subscribeOthers: vi.fn(),
}));

vi.mock("@evergenie/sdk-web", () => ({
  ApiError: Error,
  api: { notify: { pushKey: async () => ({ public_key: "KEY" }) } },
}));
vi.mock("../../data/sites", () => ({ useSites: () => state.sites }));
vi.mock("./push", () => ({
  pushSupported: () => true,
  localSubscription: async () => ({ endpoint: "https://push.example/1" }),
  keysOf: () => ({ endpoint: "https://push.example/1", p256dh: "p", auth: "a" }),
  keyToBytes: () => new Uint8Array(),
  readyRegistration: async () => ({}),
}));
vi.mock("./pushSites", () => ({
  subscribeOtherSites: state.subscribeOthers,
  unsubscribeOtherSites: vi.fn(),
}));

const site = (over: Partial<Site>): Site =>
  ({
    key: "",
    base: "",
    primary: false,
    label: "",
    state: "ok",
    ...over,
  }) as Site;
const PRIMARY = site({ primary: true, label: "amy-mini" });
const OFFICE = site({
  key: "amy-office",
  base: "https://genie-amy-office.example.ts.net",
  label: "amy-office",
});

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const tree = () => (
    <QueryClientProvider client={client}>
      <DevicesSettings />
    </QueryClientProvider>
  );
  const view = render(tree());
  return { rerender: () => view.rerender(tree()) };
}

beforeEach(() => {
  vi.stubGlobal("Notification", { permission: "granted" });
  state.subscribeOthers
    .mockReset()
    .mockImplementation(async (sites: Site[]) =>
      Object.fromEntries(sites.map((s) => [s.key, { state: "on" }])),
    );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("設定 → 裝置：其他電腦的推播（S3-8）", () => {
  test("只有一台：不出現「其他電腦的推播」、不寫別台", async () => {
    state.sites = [PRIMARY];
    mount();
    await waitFor(() => expect(screen.getByText("這台已開啟")).toBeTruthy());
    expect(screen.queryByText("其他電腦的推播")).toBeNull();
    expect(state.subscribeOthers).not.toHaveBeenCalled();
  });

  // 開著推播時進這頁就把同一個訂閱補寫進別台（後來才加的台也收得到）；同一台同一個狀態只寫一次。
  test("開著推播：每台別台寫一次，畫面標會收到；重畫不重寫", async () => {
    state.sites = [PRIMARY, OFFICE];
    const view = mount();
    await waitFor(() =>
      expect(screen.getByTestId("push-site").getAttribute("data-state")).toBe("on"),
    );
    expect(state.subscribeOthers).toHaveBeenCalledTimes(1);
    expect(state.subscribeOthers.mock.calls[0]?.[0]).toEqual([OFFICE]);
    expect(state.subscribeOthers.mock.calls[0]?.[2]).toBe("KEY");
    state.sites = [PRIMARY, { ...OFFICE }];
    view.rerender();
    await act(async () => {});
    expect(state.subscribeOthers).toHaveBeenCalledTimes(1);
  });

  // 還在試連的台先不寫，那列保持「正在確認…」；試出 ok 才寫。
  test("還在試連的台先不寫，變成能用了才寫", async () => {
    state.sites = [PRIMARY, { ...OFFICE, state: "checking" }];
    const view = mount();
    await waitFor(() => expect(screen.getByText("其他電腦的推播")).toBeTruthy());
    await act(async () => {});
    expect(state.subscribeOthers).not.toHaveBeenCalled();
    expect(screen.getByTestId("push-site").getAttribute("data-state")).toBe("checking");
    state.sites = [PRIMARY, OFFICE];
    view.rerender();
    await waitFor(() => expect(state.subscribeOthers).toHaveBeenCalledTimes(1));
  });
});
