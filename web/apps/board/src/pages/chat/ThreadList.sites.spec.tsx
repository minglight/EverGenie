import type { ThreadSummary } from "@evergenie/sdk-web";
import { cleanup, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import type { Site, SiteState } from "../../data/sites";
import type { SiteThreads } from "./siteThreads";
import { ThreadList } from "./ThreadList";

// 聊天清單照電腦分組（S3-6）：只有 useSiteThreads 換成假的兩台，其他（排序、分組、篩選、狀態字）都是真的。

const state = vi.hoisted(() => ({
  groups: [] as unknown[],
  picked: [] as string[],
}));

vi.mock("../../lib/chatDisplay", () => ({ useToolDetails: () => false }));
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    ...props
  }: {
    children: ReactNode;
    params?: { session?: string };
    search?: { home?: string };
    [k: string]: unknown;
  }) => (
    <a
      href={`/chat/${props.params?.session ?? ""}${props.search?.home ? `?home=${props.search.home}` : ""}`}
      aria-current={props["aria-current"] as "true" | undefined}
      data-thread={props["data-thread"] as string}
      data-site={props["data-site"] as string}
    >
      {children}
    </a>
  ),
}));
vi.mock("./siteThreads", async () => {
  const actual = await vi.importActual<typeof import("./siteThreads")>("./siteThreads");
  return { ...actual, useSiteThreads: () => state.groups };
});
vi.mock("../../data/agentFilter", async () => {
  const { agentKey } =
    await vi.importActual<typeof import("../../data/agents")>("../../data/agents");
  return {
    useAgentFilter: () => ({ picked: state.picked, clear: () => {} }),
    usePickedNames: () => "",
    matchAgent: (picked: string[], id?: string, site = "") =>
      picked.length === 0 || picked.includes(agentKey(site, id ?? "")),
  };
});
vi.mock("../../data/agents", async () => {
  const actual = await vi.importActual<typeof import("../../data/agents")>("../../data/agents");
  return {
    ...actual,
    useAgents: (site?: { key: string }) => ({
      list: [],
      view: (id: string) => ({
        id,
        key: actual.agentKey(site?.key ?? "", id),
        name: id === "code" ? "雅典娜" : "維納斯",
        duty: "",
      }),
    }),
  };
});

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
const office = (s: SiteState | string, reason = ""): Site =>
  site({
    key: "amy-office",
    base: "https://office.example",
    primary: false,
    label: "amy-office",
    machine: "office",
    state: s as SiteState,
    reason,
  });

const thread = (name: string, agent: string): ThreadSummary =>
  ({ name, agent, status: "idle", unread: 0, title: name, last_preview: "" }) as ThreadSummary;

function group(s: Site, data: ThreadSummary[] | undefined): SiteThreads {
  const usable = s.primary || s.state === "ok";
  return {
    site: s,
    usable,
    data: usable ? data : undefined,
    isPending: usable && !data,
    isError: false,
    refetch: () => {},
  };
}

const section = (name: string) => screen.getByRole("region", { name: `${name} 的討論串` });

beforeEach(() => {
  state.picked = [];
});
afterEach(() => cleanup());

describe("聊天清單：連多台時照電腦分組", () => {
  test("TestThreadListGroupsBySite：每台一段、同一位秘書在兩台是兩列，別台的連結帶 ?home", () => {
    state.groups = [
      group(mini, [thread("code", "code"), thread("life", "life")]),
      group(office("ok"), [thread("code", "code")]),
    ];
    render(<ThreadList />);
    const a = section("amy-mini");
    const b = section("amy-office");
    expect(within(a).getByText("amy-mini")).toBeTruthy();
    expect(
      within(a)
        .getAllByRole("link")
        .map((l) => l.getAttribute("href")),
    ).toEqual(["/chat/code", "/chat/life"]);
    expect(
      within(b)
        .getAllByRole("link")
        .map((l) => l.getAttribute("href")),
    ).toEqual(["/chat/code?home=amy-office"]);
    // 頭像外框用那台的顏色：每台的列都包一層帶台名的外框。
    expect(b.querySelector('[data-site-ring="amy-office"]')).not.toBeNull();
    expect(a.querySelector('[data-site-ring=""]')).not.toBeNull();
  });

  test("TestThreadListOnlyHighlightsSelectedSite：選中的是哪台的哪串，另一台同名的串不亮", () => {
    state.groups = [
      group(mini, [thread("code", "code")]),
      group(office("ok"), [thread("code", "code")]),
    ];
    render(<ThreadList selected={{ site: "amy-office", name: "code" }} />);
    expect(within(section("amy-office")).getByRole("link").getAttribute("aria-current")).toBe(
      "true",
    );
    expect(within(section("amy-mini")).getByRole("link").getAttribute("aria-current")).toBeNull();
  });

  test("TestThreadListSiteDownOnlyThatSection：別台連不到時只有那一段寫原因，主伺服器照常", () => {
    state.groups = [group(mini, [thread("code", "code")]), group(office("down"), undefined)];
    render(<ThreadList />);
    expect(within(section("amy-mini")).getAllByRole("link")).toHaveLength(1);
    const b = section("amy-office");
    expect(within(b).queryAllByRole("link")).toHaveLength(0);
    expect(within(b).getByRole("status").textContent).toBe(
      "amy-office 現在連不到，等它開機或網路恢復會自己再試。",
    );
    expect(within(section("amy-mini")).queryByRole("status")).toBeNull();
  });

  test("TestThreadListSiteDeniedShowsReason：被拒時寫伺服器給的原因；沒原因時寫可能的兩種", () => {
    state.groups = [
      group(mini, [thread("code", "code")]),
      group(office("denied", "這台只接受主人的裝置"), undefined),
    ];
    render(<ThreadList />);
    expect(within(section("amy-office")).getByRole("status").textContent).toBe(
      "amy-office 不讓這個看板連：這台只接受主人的裝置",
    );
    cleanup();
    state.groups = [group(mini, []), group(office("denied"), undefined)];
    render(<ThreadList />);
    expect(within(section("amy-office")).getByRole("status").textContent).toBe(
      "amy-office 沒有允許這個看板，或版本太舊。",
    );
  });

  // 不認得的狀態一律畫灰、寫「現在不能用」（S5 約定：不替新狀態另做畫面）。
  test("TestThreadListUnknownSiteStateIsGray：不認得的台狀態寫不能用、不畫清單", () => {
    state.groups = [group(mini, []), group(office("whatever"), undefined)];
    render(<ThreadList />);
    const note = within(section("amy-office")).getByRole("status");
    expect(note.textContent).toBe("amy-office 現在不能用。");
    expect(note.className).toContain("text-muted");
  });

  test("TestThreadListFilterAcrossSites：篩選只選別台的秘書時，主伺服器那段整段不畫", () => {
    state.picked = ["amy-office/code"];
    state.groups = [
      group(mini, [thread("code", "code")]),
      group(office("ok"), [thread("code", "code")]),
    ];
    render(<ThreadList />);
    expect(screen.queryByRole("region", { name: "amy-mini 的討論串" })).toBeNull();
    expect(within(section("amy-office")).getAllByRole("link")).toHaveLength(1);
  });

  test("TestThreadListSingleSiteUnchanged：只有一台時沒有分段、沒有台名、沒有外框", () => {
    state.groups = [group(mini, [thread("code", "code")])];
    render(<ThreadList />);
    expect(screen.queryByRole("region")).toBeNull();
    expect(screen.queryByTestId("site-tag")).toBeNull();
    expect(document.querySelector("[data-site-ring]")).toBeNull();
    expect(screen.getByRole("link").getAttribute("href")).toBe("/chat/code");
  });
});
