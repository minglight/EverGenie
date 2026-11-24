import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { Welcome } from "./Welcome";
import { WELCOME_PATH, welcomeGate } from "./welcomeFlow";

// 假的路由：只記現在的網址（pathname＋search），Link 與 navigate 都改它；歡迎頁只用到這三樣。
const nav = vi.hoisted(() => {
  const state = { pathname: "", search: {} as Record<string, unknown> };
  const listeners = new Set<() => void>();
  let snapshot = { ...state };
  return {
    get: () => snapshot,
    go(to: string, search: Record<string, unknown> = {}) {
      state.pathname = to;
      state.search = Object.fromEntries(Object.entries(search).filter(([, v]) => v !== undefined));
      snapshot = { ...state };
      for (const l of listeners) l();
    },
    subscribe(l: () => void) {
      listeners.add(l);
      return () => listeners.delete(l);
    },
  };
});

vi.mock("@tanstack/react-router", async () => {
  const React = await import("react");
  type To = { to: string; search?: Record<string, unknown> };
  return {
    useLocation: ({ select }: { select: (l: ReturnType<typeof nav.get>) => unknown }) =>
      select(React.useSyncExternalStore(nav.subscribe, nav.get)),
    useNavigate:
      () =>
      async ({ to, search }: To) =>
        nav.go(to, search),
    Link: ({ to, search, children, ...rest }: To & { children: React.ReactNode }) => (
      <a
        href={to}
        {...rest}
        onClick={(e) => {
          e.preventDefault();
          nav.go(to, search);
        }}
      >
        {children}
      </a>
    ),
  };
});

const calls = vi.hoisted(() => ({
  knowledge: vi.fn(),
  setKnowledge: vi.fn(),
  sync: vi.fn(),
  setField: vi.fn(),
  getAgents: vi.fn(),
  getRoster: vi.fn(),
  packages: [] as unknown[],
  personas: [] as unknown[],
}));

vi.mock("@evergenie/sdk-web", () => {
  class ApiError extends Error {}
  return {
    ApiError,
    api: {
      web: { getAgents: calls.getAgents, getRoster: calls.getRoster },
      system: { getMode: async () => ({ env: "", label: "", user_name: "小明" }) },
      personas: { list: async () => ({ personas: calls.personas }) },
      package: { list: async () => ({ packages: calls.packages }), sync: calls.sync },
      agents: {
        setField: calls.setField,
        knowledge: calls.knowledge,
        setKnowledge: calls.setKnowledge,
      },
    },
  };
});

const officialPackages = [
  {
    id: "health",
    enabled: true,
    state: "running",
    agents: [{ id: "health", duty: "健康：飲食、運動、身體一起照顧" }],
  },
  {
    id: "secretaries",
    enabled: true,
    state: "running",
    agents: [
      { id: "code", duty: "寫程式：讀 code" },
      { id: "life", duty: "生活：家庭、提醒" },
      { id: "system", duty: "電腦：電腦環境" },
      { id: "work", duty: "工作：行程、會議" },
    ],
  },
  {
    // 開發家才開著的開發工坊帶選用的生圖
    id: "admin",
    enabled: true,
    state: "running",
    agents: [{ id: "art", duty: "生圖", optional: true }],
  },
];

const officialPersonas = [
  {
    slug: "athena",
    name: "雅典娜",
    source: "official",
    status: "live",
    seats: [],
    shadowed: false,
    avatar_url: "/avatars/persona/athena.png",
    tagline: "知性冷靜、愛拆問題的夥伴。",
  },
  { slug: "venus", name: "維納斯", source: "official", status: "live", seats: [], shadowed: false },
  { slug: "wip", name: "設計中", source: "mine", status: "design", seats: [], shadowed: false },
];

let client: QueryClient;

function renderAt(url: string) {
  const [path, query = ""] = url.split("?");
  nav.go(path ?? "", Object.fromEntries(new URLSearchParams(query)));
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <Shown />
    </QueryClientProvider>,
  );
  return { state: { location: nav.get() } } as never as {
    state: { location: { pathname: string; search: Record<string, unknown> } };
  };
}

/** 歡迎頁只在自己的網址畫；離開了（到今天、到設定）就不畫，跟外殼的做法一樣。 */
function Shown() {
  return nav.get().pathname === WELCOME_PATH ? <Welcome /> : null;
}

beforeEach(() => {
  calls.sync.mockReset().mockResolvedValue({});
  calls.setField.mockReset().mockResolvedValue({});
  // 預設這個職位沒有可選知識（介面 1：blocks 是空的）。
  calls.knowledge.mockReset().mockResolvedValue({ agent: "", blocks: [] });
  calls.setKnowledge.mockReset().mockResolvedValue({ agent: "", blocks: [] });
  calls.getAgents.mockReset().mockResolvedValue({ agents: [] });
  calls.getRoster.mockReset().mockResolvedValue({ agents: [] });
  calls.packages = officialPackages;
  calls.personas = officialPersonas;
  sessionStorage.clear();
});
afterEach(cleanup);

const step = () => screen.getByTestId("welcome").getAttribute("data-step");

// Guards: WEB-176
describe("新家的歡迎頁", () => {
  test("一頁一件事：說明 → 選職位 → 選角色 → 請〈角色名字〉開始上班 → 到今天頁；用跟設定頁同一個請秘書的動作", async () => {
    renderAt(WELCOME_PATH);
    await waitFor(() => expect(step()).toBe("intro"));
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("歡迎");
    expect(screen.getByText("這是你的秘書們。先請一位開始，其他的之後再加。")).toBeTruthy();
    expect(screen.queryByTestId("welcome-back")).toBeNull();
    fireEvent.click(screen.getByTestId("welcome-begin"));

    // 第二步：三張大卡（生活／健康／工作），沒有寫程式、電腦、選用的生圖；畫面上沒有職位代號。
    await waitFor(() => expect(step()).toBe("job"));
    const jobs = await screen.findAllByTestId("welcome-job");
    expect(jobs.map((j) => j.getAttribute("data-job"))).toEqual(["life", "health", "work"]);
    const jobText = screen.getByTestId("welcome").textContent ?? "";
    for (const code of ["health", "life", "work", "code", "system", "art", "職位"]) {
      expect(jobText).not.toContain(code);
    }
    expect(nav.get().search).toEqual({ step: "job" });
    fireEvent.click(jobs[1] as HTMLElement);

    // 第三步：角色是卡片（頭像＋名字＋一句介紹），不是下拉；設計中的不列。
    await waitFor(() => expect(step()).toBe("persona"));
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("請誰來當你的健康秘書？");
    const personas = screen.getAllByTestId("welcome-persona");
    expect(personas.map((p) => p.getAttribute("data-slug"))).toEqual(["athena", "venus"]);
    expect(personas[0]?.textContent).toContain("雅典娜");
    expect(personas[0]?.textContent).toContain("知性冷靜、愛拆問題的夥伴。");
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(nav.get().search).toEqual({ step: "persona", job: "health" });
    fireEvent.click(personas[0] as HTMLElement);

    // 第四步：請她開始上班。
    await waitFor(() => expect(step()).toBe("start"));
    expect(nav.get().search).toEqual({
      step: "start",
      job: "health",
      persona: "athena",
    });
    const hire = screen.getByTestId("welcome-hire");
    expect(hire.textContent).toBe("請雅典娜開始上班");
    fireEvent.click(hire);
    await waitFor(() => expect(nav.get().pathname).toBe("/"));
    expect(calls.sync).toHaveBeenCalledWith({
      id: "health",
      agent: "health",
      persona: "athena",
      apply: true,
    });
    expect(calls.setField).toHaveBeenCalledWith({ id: "health", field: "enabled", value: "true" });
  });

  test("重新整理回到同一步；每一步有「上一步」", async () => {
    renderAt(`${WELCOME_PATH}?step=start&job=life&persona=venus`);
    await waitFor(() => expect(step()).toBe("start"));
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("請維納斯當你的生活秘書");
    fireEvent.click(screen.getByTestId("welcome-back"));
    await waitFor(() => expect(step()).toBe("persona"));
    expect(nav.get().search).toEqual({ step: "persona", job: "life" });
    fireEvent.click(screen.getByTestId("welcome-back"));
    await waitFor(() => expect(step()).toBe("job"));
    fireEvent.click(screen.getByTestId("welcome-back"));
    await waitFor(() => expect(step()).toBe("intro"));
  });

  test("沒請成功：白話原因＋再試一次，留在這一步；再按一次成功就到今天頁", async () => {
    calls.sync.mockRejectedValueOnce(new Error("Failed to fetch"));
    renderAt(`${WELCOME_PATH}?step=start&job=work&persona=athena`);
    await waitFor(() => expect(step()).toBe("start"));
    fireEvent.click(screen.getByTestId("welcome-hire"));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("沒有請成功");
    expect(alert.textContent).not.toContain("Failed to fetch");
    expect(step()).toBe("start");
    expect(calls.setField).not.toHaveBeenCalled();
    const retry = screen.getByTestId("welcome-hire");
    expect(retry.textContent).toBe("再試一次");
    fireEvent.click(retry);
    await waitFor(() => expect(nav.get().pathname).toBe("/"));
    expect(calls.sync).toHaveBeenCalledTimes(2);
  });

  // 請成功之後名冊重讀失敗（剛好斷線）：快取裡「名冊是空的」那份舊答案已經丟掉，
  // 外殼沒有名冊時照常畫看板（welcomeGate），不會因為舊的空名冊被彈回歡迎頁第一步。
  test("請成功就到今天，不靠名冊重讀成功", async () => {
    renderAt(`${WELCOME_PATH}?step=start&job=health&persona=athena`);
    await waitFor(() => expect(step()).toBe("start"));
    // 外殼與今天頁讀過的空名冊（兩份快取）。
    client.setQueryData(["web.get_agents"], { agents: [] });
    client.setQueryData(["roster"], []);
    calls.getAgents.mockRejectedValue(new Error("Failed to fetch"));
    calls.getRoster.mockRejectedValue(new Error("Failed to fetch"));
    fireEvent.click(screen.getByTestId("welcome-hire"));
    await waitFor(() => expect(nav.get().pathname).toBe("/"));
    expect(client.getQueryData(["web.get_agents"])).toBeUndefined();
    expect(client.getQueryData(["roster"])).toBeUndefined();
    expect(welcomeGate(undefined, "/", false)).toBe("board");
  });

  // DEV-161：職位有可選知識，就在「請她開始上班」那一步的頭像下面，預設勾好；送出時一起存。
  describe("可選知識", () => {
    const lifeBlocks = [
      {
        id: "parenting",
        title: "育兒",
        summary: "依孩子的年齡陪你想",
        default: true,
        enabled: false,
      },
      {
        id: "balance",
        title: "工作與生活的平衡",
        summary: "工作吃掉生活時提醒一句",
        default: false,
        enabled: false,
      },
    ];
    const box = (id: string) =>
      document.querySelector(
        `[data-testid="knowledge-block"][data-block="${id}"]`,
      ) as HTMLInputElement;

    test("同一畫面、不多一步：預設勾好，改勾之後請她上班，先建好紀錄、存知識、再啟用", async () => {
      const order: string[] = [];
      calls.sync.mockImplementation(async () => void order.push("sync"));
      calls.knowledge.mockImplementation(async () => {
        order.push("knowledge");
        return { agent: "life", blocks: lifeBlocks };
      });
      calls.setKnowledge.mockImplementation(async () => {
        order.push("setKnowledge");
        return { agent: "life", blocks: lifeBlocks };
      });
      calls.setField.mockImplementation(async () => void order.push("enable"));
      renderAt(`${WELCOME_PATH}?step=start&job=life&persona=venus`);
      await waitFor(() => expect(step()).toBe("start"));
      expect(await screen.findByText("育兒")).toBeTruthy();
      expect(screen.getByText("她帶的知識")).toBeTruthy();
      expect(screen.getByText("依孩子的年齡陪你想")).toBeTruthy();
      expect(box("parenting").checked).toBe(true);
      expect(box("balance").checked).toBe(false);
      expect(screen.getByTestId("welcome-progress").textContent).toBe("第 4 步，共 4 步");
      expect(calls.knowledge).toHaveBeenCalledWith({ agent: "life" });
      fireEvent.click(box("balance"));
      fireEvent.click(screen.getByTestId("welcome-hire"));
      await waitFor(() => expect(nav.get().pathname).toBe("/"));
      expect(calls.setKnowledge).toHaveBeenCalledWith({
        agent: "life",
        enabled: ["parenting", "balance"],
      });
      expect(order).toEqual(["knowledge", "sync", "setKnowledge", "enable"]);
    });

    test("取消預設勾的，送出時就沒有它", async () => {
      calls.knowledge.mockResolvedValue({ agent: "life", blocks: lifeBlocks });
      renderAt(`${WELCOME_PATH}?step=start&job=life&persona=venus`);
      await screen.findByText("育兒");
      fireEvent.click(box("parenting"));
      fireEvent.click(screen.getByTestId("welcome-hire"));
      await waitFor(() => expect(nav.get().pathname).toBe("/"));
      expect(calls.setKnowledge).toHaveBeenCalledWith({
        agent: "life",
        enabled: [],
      });
    });

    test("沒有可選知識：不出現這塊，也不呼叫 set_knowledge", async () => {
      renderAt(`${WELCOME_PATH}?step=start&job=health&persona=athena`);
      await waitFor(() => expect(step()).toBe("start"));
      await waitFor(() => expect(calls.knowledge).toHaveBeenCalled());
      expect(screen.queryByTestId("knowledge")).toBeNull();
      fireEvent.click(screen.getByTestId("welcome-hire"));
      await waitFor(() => expect(nav.get().pathname).toBe("/"));
      expect(calls.setKnowledge).not.toHaveBeenCalled();
    });

    test("讀不到可選知識：不擋「開始上班」，用預設", async () => {
      calls.knowledge.mockRejectedValue(new Error("Failed to fetch"));
      renderAt(`${WELCOME_PATH}?step=start&job=life&persona=venus`);
      await waitFor(() => expect(step()).toBe("start"));
      await waitFor(() => expect(calls.knowledge).toHaveBeenCalled());
      expect(screen.queryByTestId("knowledge")).toBeNull();
      fireEvent.click(screen.getByTestId("welcome-hire"));
      await waitFor(() => expect(nav.get().pathname).toBe("/"));
      expect(calls.sync).toHaveBeenCalled();
      expect(calls.setField).toHaveBeenCalledWith({ id: "life", field: "enabled", value: "true" });
    });
  });

  test("這個家沒裝的職位那張不出現", async () => {
    calls.packages = [officialPackages[1]];
    renderAt(`${WELCOME_PATH}?step=job`);
    const jobs = await screen.findAllByTestId("welcome-job");
    expect(jobs.map((j) => j.getAttribute("data-job"))).toEqual(["life", "work"]);
  });

  test("沒有可以請的職位：說明＋「先進看板」，不把人卡在這一頁", async () => {
    calls.packages = [];
    renderAt(`${WELCOME_PATH}?step=job`);
    const dead = await screen.findByTestId("welcome-dead-end");
    expect(dead.textContent).toContain("這個家還沒有可以請的職位");
    fireEvent.click(screen.getByRole("button", { name: "先進看板" }));
    await waitFor(() => expect(nav.get().pathname).toBe("/settings/setup"));
    expect(sessionStorage.getItem("eg.welcome.skip.v1")).toBe("1");
  });
});
