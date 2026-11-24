import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, test, vi } from "vitest";

const state = vi.hoisted(() => ({
  list: null as null | (() => Promise<{ notifications: unknown[] }>),
  show: undefined as string | undefined,
  lane: undefined as string | undefined,
  archive: vi.fn(async (_req: { ids?: string[] }) => ({ archived: [] as string[] })),
  wide: false,
  get: vi.fn(() => new Promise(() => {})),
  markRead: vi.fn(async () => ({})),
  navigate: vi.fn((_to: unknown) => {}),
}));

vi.mock("@evergenie/sdk-web", async () => {
  const actual = await vi.importActual<typeof import("@evergenie/sdk-web")>("@evergenie/sdk-web");
  return {
    ...actual,
    api: {
      ...actual.api,
      notify: {
        ...actual.api.notify,
        list: vi.fn(() => (state.list ? state.list() : new Promise(() => {}))),
        get: state.get,
        markRead: state.markRead,
        archive: state.archive,
      },
    },
  };
});
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, ...rest }: { children: ReactNode }) => {
    const r = rest as Record<string, string>;
    return (
      <a
        href="/"
        data-unread={r["data-unread"]}
        data-lane={r["data-lane"]}
        data-testid={r["data-testid"]}
        aria-current={r["aria-current"] as "page" | undefined}
      >
        {children}
      </a>
    );
  },
  useNavigate: () => state.navigate,
  useSearch: () => ({ show: state.show, lane: state.lane }),
}));
vi.mock("../../data/agents", async () => {
  const actual = await vi.importActual<typeof import("../../data/agents")>("../../data/agents");
  return {
    ...actual,
    useAgents: () => ({ view: (id?: string) => ({ id, name: id ?? "" }), list: [] }),
  };
});
vi.mock("../../data/agentFilter", async () => {
  const actual =
    await vi.importActual<typeof import("../../data/agentFilter")>("../../data/agentFilter");
  return { ...actual, useAgentFilter: () => ({ picked: [] }) };
});
vi.mock("../../lib/media", () => ({
  useIsDesktop: () => state.wide,
  useIsWide: () => state.wide,
  useIsXWide: () => false,
}));

import type { Notification } from "@evergenie/sdk-web";
import type { Site } from "../../data/sites";
import { MessagesPage, mergeNotices, segmentLabel, sendBySite } from "./MessagesPage";

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MessagesPage />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  state.list = null;
  state.show = undefined;
  state.lane = undefined;
  state.wide = false;
  state.archive.mockClear();
  state.get.mockClear();
  state.markRead.mockClear();
  state.navigate.mockClear();
});

const notice = (over: Record<string, unknown>) => ({
  agent: "work",
  title: "標題",
  created_at: "2026-10-30T08:00:00Z",
  urgent: false,
  ...over,
});

describe("訊息頁（WEB-70）", () => {
  // Guards: REQ-383
  test("載入中分段只寫名稱，不先顯示 0", async () => {
    renderPage();
    const buttons = await screen.findAllByRole("button");
    const labels = buttons.map((b) => b.textContent ?? "");
    expect(labels).toEqual(expect.arrayContaining(["未讀", "要你同意", "全部"]));
    expect(labels.filter((l) => l.search(/\d/) >= 0)).toEqual([]);
    expect(segmentLabel("未讀", false, 0)).toBe("未讀");
    expect(segmentLabel("未讀", true, 0)).toBe("未讀 0");
  });

  // Guards: REQ-383
  test("載入完才有數字；要你回答的那則是橘點，回答過、自動收掉的沒有點", async () => {
    state.list = async () => ({
      notifications: [
        notice({ id: "M-1", title: "第一則", category: "approve", state: "read" }),
        notice({
          id: "M-2",
          title: "第二則",
          category: "approve",
          state: "answered",
          answer: { text: "已經不用回了", via: "auto" },
        }),
        notice({ id: "M-3", title: "第三則", category: "done", state: "unread" }),
      ],
    });
    renderPage();
    expect(await screen.findByRole("button", { name: "未讀 1" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "要你同意 1" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "全部 3" })).toBeTruthy();
    cleanup();
    // 「全部」看每一列的點：M-1 橘（要你回答）、M-2 沒有（自動收掉）、M-3 綠（不用回的未讀）。
    state.show = "all";
    renderPage();
    await screen.findByRole("button", { name: "全部 3" });
    // 簡單模式的清單列不顯示編號（DEV-156 U-22）：用標題認。
    const dotOf = (title: string) =>
      [...document.querySelectorAll("li")]
        .find((li) => li.textContent?.includes(title))
        ?.querySelector("[data-dot]")
        ?.getAttribute("data-dot");
    expect(dotOf("第一則")).toBe("reply");
    expect(dotOf("第二則")).toBe("none");
    expect(dotOf("第三則")).toBe("fresh");
  });
});

describe("訊息頁合併兩台（S3-7a）", () => {
  const site = (key: string): Site =>
    ({ key, base: key ? `https://${key}.example.ts.net` : "", primary: key === "" }) as Site;

  test("TestMergeNoticesNewestFirstAcrossSitesWithSiteKeys：兩台照時間排在一起、同編號不撞、討論串內提問不進來", () => {
    const merged = mergeNotices([
      {
        site: site(""),
        failed: false,
        data: [
          notice({ id: "M-1", created_at: "2026-10-30T08:00:00Z" }),
          notice({ id: "M-2", created_at: "2026-10-30T10:00:00Z", inline: true }),
        ] as Notification[],
      },
      {
        site: site("amy-office"),
        failed: false,
        data: [notice({ id: "M-1", created_at: "2026-10-30T09:00:00Z" })] as Notification[],
      },
    ]);
    expect(merged.map((e) => e.key)).toEqual(["amy-office/M-1", "M-1"]);
    expect(merged.map((e) => e.n.id)).toEqual(["M-1", "M-1"]);
    expect(merged[0]?.site.key).toBe("amy-office");
  });

  test("TestWidePaneWaitsForHomeSiteInsteadOfReadingPrimary：?home= 那台還找不到時，右欄不讀也不標主伺服器同編號那則", async () => {
    state.wide = true;
    state.list = async () => ({ notifications: [notice({ id: "M-1", state: "unread" })] });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <MessagesPage selected="M-1" selectedSite={null} selectedPending />
      </QueryClientProvider>,
    );
    await screen.findByRole("button", { name: "未讀 1" });
    expect(state.get).not.toHaveBeenCalled();
    expect(state.markRead).not.toHaveBeenCalled();
  });

  test("TestSendBySiteSkipsSitesNoLongerOkAndKeepsOthers：清單畫出來後才斷的台不送、不退回主伺服器，別台照做", async () => {
    const A = { ...site(""), state: "ok" } as Site;
    const B = { ...site("amy-office"), label: "amy-office", state: "down" } as Site;
    const C = { ...site("amy-home"), label: "amy-home", state: "ok" } as Site;
    const entry = (s: Site, id: string) => ({
      site: s,
      key: id,
      n: notice({ id }) as Notification,
    });
    const sent: string[] = [];
    const { done, failed } = await sendBySite(
      [entry(A, "M-1"), entry(B, "M-2"), entry(C, "M-3"), entry(C, "M-4")],
      [A, B, C],
      async (s, ids) => {
        sent.push(`${s.key || "primary"}:${ids.join(",")}`);
        if (s.key === "amy-home") throw new Error("boom");
        return ids.length;
      },
    );
    expect(sent).toEqual(["primary:M-1", "amy-home:M-3,M-4"]);
    expect(done).toEqual([1]);
    expect(failed).toEqual(["amy-office", "amy-home"]);
  });
});

describe("訊息分「秘書｜系統」（D-47、WEB-86）", () => {
  const nightly = (id: string, hash: string, at: string) =>
    notice({
      id,
      agent: "admin",
      category: "done",
      state: "unread",
      lane: "system",
      badge: false,
      title: `每晚測試沒過：${hash}（false）`,
      created_at: at,
    });
  const list = () => ({
    notifications: [
      notice({
        id: "M-1",
        category: "done",
        state: "unread",
        lane: "agent",
        badge: true,
        title: "幫你訂好位了",
      }),
      nightly("M-2", "8c4e971f", "2026-10-30T07:00:00Z"),
      nightly("M-3", "c703aef8", "2026-10-30T06:00:00Z"),
      nightly("M-4", "6c27669e", "2026-10-30T05:00:00Z"),
      notice({
        id: "M-5",
        agent: "system",
        category: "approve",
        state: "unread",
        lane: "system",
        badge: true,
        source: "system.alert",
        title: "office 的 acme 帳號要重新登入",
        created_at: "2026-10-30T04:00:00Z",
      }),
    ],
  });
  const tab = (lane: string) => document.querySelector(`a[data-lane="${lane}"]`);

  test("秘書分頁只列秘書的；系統有要你回答的，上面一行帶過去；兩個分頁各寫自己的未讀數", async () => {
    state.list = async () => list();
    renderPage();
    expect(await screen.findByRole("button", { name: "未讀 1" })).toBeTruthy();
    expect(tab("agent")?.getAttribute("aria-current")).toBe("page");
    expect(tab("system")?.getAttribute("aria-current")).toBeNull();
    // 數字給眼睛看，報讀器聽的是整句。
    expect(tab("agent")?.textContent).toBe("秘書11 則未讀");
    // 系統那道有等你處理的：寫那幾則（不是 4 則未讀）。
    expect(tab("system")?.textContent).toBe("系統11 則等你處理");
    expect(screen.getByText(/幫你訂好位了/)).toBeTruthy();
    expect(screen.queryByText(/每晚測試沒過/)).toBeNull();
    expect(screen.getByTestId("system-waiting").textContent).toContain("系統有 1 則等你處理");
  });

  test("推播或別處連進來、網址沒寫 ?lane=：清單跟著選的那一則到系統那道，所在的組先展開", async () => {
    state.wide = true;
    state.list = async () => list();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <MessagesPage selected="M-3" />
      </QueryClientProvider>,
    );
    expect(await screen.findByTestId("notice-group")).toBeTruthy();
    expect(tab("system")?.getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("list", { name: "這 3 則" })).toBeTruthy();
  });

  test("網址沒寫 ?lane=、照選的那則開在系統那道時，換分段還留在系統那道（不跳回秘書）", async () => {
    state.wide = true;
    state.list = async () => list();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <MessagesPage selected="M-3" />
      </QueryClientProvider>,
    );
    fireEvent.click(await screen.findByRole("button", { name: "全部 4" }));
    expect(state.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ search: { show: "all", lane: "system" } }),
    );
  });

  test("系統分頁：連在一起的同一種收成一列 ×N，展開看每一則，刪除整組送全部編號；要你回答的不收", async () => {
    state.lane = "system";
    state.list = async () => list();
    renderPage();
    expect(await screen.findByRole("button", { name: "未讀 4" })).toBeTruthy();
    expect(screen.queryByTestId("system-waiting")).toBeNull();
    const groups = screen.getAllByTestId("notice-group");
    expect(groups).toHaveLength(1);
    expect(groups[0]?.getAttribute("data-count")).toBe("3");
    expect(groups[0]?.textContent).toContain("每晚測試沒過：8c4e971f（false）");
    expect(groups[0]?.textContent).toContain("×3");
    // 要你回答的系統提醒自己一列，不收進組裡。
    expect(screen.getByText(/要重新登入/)).toBeTruthy();
    expect(screen.queryByText(/c703aef8/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /收起來的 3 則/ }));
    expect(screen.getByText(/c703aef8/)).toBeTruthy();
    expect(screen.getByRole("list", { name: "這 3 則" }).querySelectorAll("li")).toHaveLength(3);
    const del = screen.getByRole("button", { name: "刪除這 3 則" });
    fireEvent.click(del);
    fireEvent.click(screen.getByRole("button", { name: "確定刪除？" }));
    await vi.waitFor(() => expect(state.archive).toHaveBeenCalled());
    expect(state.archive.mock.calls[0]?.[0]).toEqual({ ids: ["M-2", "M-3", "M-4"] });
  });
});
