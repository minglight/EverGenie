import type { RosterAgent, ThreadSummary } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { PageAgentBanner } from "../../components/PageAgentBanner";
import { rosterQuery } from "../../data/queries";
import type { Site, SiteState } from "../../data/sites";
import { PageChat } from "./PageChat";

// 套件頁的聊天與頂端橫幅連多台（S3 收尾）：選別台的秘書就讀寫那台，那台不能用只寫 SiteNotice、不改打主伺服器。
// 只有台清單（useSites）、每台的 API（apiFor）、記住的選擇（usePageChat）與討論串本體（ThreadView）是假的。

const fake = (agents: RosterAgent[], threads: ThreadSummary[]) => ({
  web: { getRoster: vi.fn(async () => ({ agents })) },
  session: {
    listThreads: vi.fn(async () => ({ threads })),
    open: vi.fn(async ({ agent }: { agent: string }) => ({ session: { name: agent } })),
  },
});

const state = vi.hoisted(() => ({
  sites: [] as unknown[],
  apis: {} as Record<string, unknown>,
  picks: {} as Record<string, string>,
}));

vi.mock("../../data/sites", async () => {
  const actual = await vi.importActual<typeof import("../../data/sites")>("../../data/sites");
  return {
    ...actual,
    useSites: () => state.sites,
    apiFor: (site: { base: string }) => state.apis[site.base],
  };
});
vi.mock("../../data/pageChat", async () => {
  const actual = await vi.importActual<typeof import("../../data/pageChat")>("../../data/pageChat");
  return {
    ...actual,
    usePageChat: () => ({
      picks: state.picks,
      pick: (id: string) => {
        state.picks = { ...state.picks, fitness: id };
      },
    }),
  };
});
vi.mock("./ThreadView", () => ({
  ThreadView: ({ name, home }: { name: string; home?: { key: string } }) => (
    <div data-testid="thread-view" data-name={name} data-home={home?.key ?? "?"} />
  ),
}));
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="/chat">{children}</a>,
}));

const site = (over: Partial<Site>): Site => ({
  key: "",
  base: "",
  primary: true,
  label: "amy-mini",
  machine: "mini",
  env: "prod",
  version: "1.0.0",
  boardApi: 1,
  publicUrl: "",
  color: "",
  state: "ok",
  reason: "",
  ...over,
});
const mini = site({});
const office = (s: SiteState) =>
  site({
    key: "amy-office",
    base: "https://office.example",
    primary: false,
    label: "amy-office",
    machine: "office",
    state: s,
  });

const agent = (id: string, name: string) => ({ id, name, enabled: true }) as RosterAgent;
const thread = (name: string, agentId: string) =>
  ({ name, agent: agentId, status: "idle", unread: 0, title: name }) as ThreadSummary;

let miniApi: ReturnType<typeof fake>;
let officeApi: ReturnType<typeof fake>;
let client: QueryClient;

function setup(officeState: SiteState, picks: Record<string, string>, officeThreads = true) {
  miniApi = fake([agent("life", "維納斯")], [thread("life", "life")]);
  officeApi = fake([agent("code", "雅典娜")], officeThreads ? [thread("code", "code")] : []);
  state.apis = { "": miniApi, "https://office.example": officeApi };
  state.sites = [mini, office(officeState)];
  state.picks = picks;
}

const show = (ui: ReactNode) =>
  render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});
afterEach(() => cleanup());

describe("套件頁聊天：別台的秘書", () => {
  test("選了別台的秘書：討論串讀那台、選單照電腦分組，主伺服器的討論串不讀", async () => {
    setup("ok", { fitness: "amy-office/code" });
    show(<PageChat chatKey="fitness" fallback="life" />);
    const view = await screen.findByTestId("thread-view");
    expect(view.dataset.home).toBe("amy-office");
    expect(view.dataset.name).toBe("code");
    const select = screen.getByTestId("page-chat-agent") as HTMLSelectElement;
    expect(select.value).toBe("amy-office/code");
    const groups = [...select.querySelectorAll("optgroup")].map((g) => g.label);
    expect(groups).toEqual(["amy-mini", "amy-office"]);
    expect(officeApi.session.listThreads).toHaveBeenCalled();
    expect(miniApi.session.listThreads).not.toHaveBeenCalled();
  });

  test("固定跟別台的秘書、還沒有對話：開對話開在那台，不開在主伺服器", async () => {
    setup("ok", {}, false);
    show(<PageChat chatKey="fitness" fallback="" fixed="amy-office/code" />);
    fireEvent.click(await screen.findByRole("button", { name: "開始跟雅典娜聊" }));
    await waitFor(() => expect(officeApi.session.open).toHaveBeenCalledWith({ agent: "code" }));
    expect(miniApi.session.open).not.toHaveBeenCalled();
    expect((await screen.findByTestId("thread-view")).dataset.home).toBe("amy-office");
  });

  test("那台連不到：照樣選著她、寫 SiteNotice、名字用讀過的名冊，主伺服器一樣不打", async () => {
    setup("down", { fitness: "amy-office/code" });
    client.setQueryData(rosterQuery(office("down")).queryKey, [agent("code", "雅典娜")]);
    show(<PageChat chatKey="fitness" fallback="life" />);
    const notice = await screen.findByTestId("site-notice");
    expect(notice.dataset.site).toBe("amy-office");
    const select = screen.getByTestId("page-chat-agent") as HTMLSelectElement;
    expect(select.value).toBe("amy-office/code");
    expect(within(select).getByRole("option", { name: "雅典娜（amy-office）" })).toBeTruthy();
    expect(screen.queryByTestId("thread-view")).toBeNull();
    expect(miniApi.session.listThreads).not.toHaveBeenCalled();
    expect(miniApi.session.open).not.toHaveBeenCalled();
  });

  test("那台連不到、這個分頁沒讀過她的名冊：選單寫「秘書」，不露職位 id", async () => {
    setup("down", { fitness: "amy-office/code" });
    show(<PageChat chatKey="fitness" fallback="life" />);
    await screen.findByTestId("site-notice");
    const select = screen.getByTestId("page-chat-agent");
    expect(within(select).getByRole("option", { name: "秘書（amy-office）" })).toBeTruthy();
    expect(select.textContent).not.toContain("code");
  });

  test("舊的選擇（主伺服器的職位 id）照用：讀主伺服器的討論串", async () => {
    setup("ok", { fitness: "life" });
    show(<PageChat chatKey="fitness" fallback="" />);
    const view = await screen.findByTestId("thread-view");
    expect(view.dataset.home).toBe("");
    expect(view.dataset.name).toBe("life");
    expect((screen.getByTestId("page-chat-agent") as HTMLSelectElement).value).toBe("life");
  });
});

describe("套件頁橫幅：別台的秘書", () => {
  test("那台能用：名字讀那台的名冊", async () => {
    setup("ok", {});
    show(<PageAgentBanner id="amy-office/code" />);
    expect(await screen.findByRole("region", { name: "負責這頁的秘書：雅典娜" })).toBeTruthy();
    expect(officeApi.web.getRoster).toHaveBeenCalled();
  });

  test("那台連不到：只寫一行 SiteNotice，不改顯示主伺服器的人", async () => {
    setup("down", {});
    show(<PageAgentBanner id="amy-office/code" />);
    expect((await screen.findByTestId("site-notice")).dataset.site).toBe("amy-office");
    expect(screen.queryByTestId("page-agent-banner")).toBeNull();
    expect(miniApi.session.listThreads).not.toHaveBeenCalled();
  });
});
