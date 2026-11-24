import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import type { Site } from "../../data/sites";

const state = vi.hoisted(() => ({
  getSites: vi.fn(),
  setSites: vi.fn(),
  setAllowBoards: vi.fn(),
  setOwnerLogin: vi.fn(),
  reprobe: vi.fn(),
  sites: [] as Site[],
}));

vi.mock("@evergenie/sdk-web", async (actual) => {
  const real = await actual<typeof import("@evergenie/sdk-web")>();
  return {
    ...real,
    api: {
      ...real.api,
      web: {
        getSites: state.getSites,
        setSites: state.setSites,
        setAllowBoards: state.setAllowBoards,
        setOwnerLogin: state.setOwnerLogin,
      },
    },
  };
});

vi.mock("../../data/sites", async (actual) => {
  const real = await actual<typeof import("../../data/sites")>();
  return { ...real, useSites: () => state.sites, reprobeSites: state.reprobe };
});

import { agentFilterStore } from "../../data/agentFilter";
import { SitesSettings } from "./SitesSettings";

const OFFICE = "https://genie-amy-office.example.ts.net";
const DEV = "https://genie-dev-amy-office.example.ts.net";

const site = (over: Partial<Site>): Site => ({
  key: "",
  base: "",
  primary: false,
  label: "",
  machine: "",
  env: "prod",
  version: "",
  boardApi: 1,
  publicUrl: "",
  color: "",
  state: "ok",
  reason: "",
  ...over,
});

const primary = site({
  primary: true,
  label: "amy-mini",
  version: "1.4.0",
  publicUrl: "https://genie-amy-mini.example.ts.net",
});

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <SitesSettings />
    </QueryClientProvider>,
  );
}

const rowOf = (key: string) => {
  const row = screen.getAllByTestId("site-row").find((r) => r.getAttribute("data-site") === key);
  if (!row) throw new Error(`沒有 ${key} 這一列`);
  return row;
};

beforeEach(() => {
  for (const f of [
    state.getSites,
    state.setSites,
    state.setAllowBoards,
    state.setOwnerLogin,
    state.reprobe,
  ])
    f.mockReset();
  state.getSites.mockResolvedValue({
    owner_login: "me@example.com",
    allow_boards: [DEV],
    sites: [{ url: OFFICE, color: "#3b82f6" }, { url: DEV }],
  });
  state.setSites.mockResolvedValue({});
  state.setAllowBoards.mockResolvedValue({});
  state.setOwnerLogin.mockResolvedValue({});
  state.reprobe.mockResolvedValue(undefined);
  state.sites = [
    primary,
    // 別台用 denied／down（ok 的台會跨站去抓它的信任設定，那段由 e2e 驗）。
    site({
      key: "amy-office",
      base: OFFICE,
      label: "amy-office",
      color: "#3b82f6",
      env: "",
      state: "denied",
      reason: "這台只接受主人的裝置",
    }),
    site({ key: DEV, base: DEV, state: "down" }),
  ];
});
afterEach(cleanup);

describe("設定 → 電腦：看板連的電腦", () => {
  test("每台列出名字、網址、狀態；被拒的寫伺服器給的原因與要到那台允許這個看板", async () => {
    wrap();
    await screen.findAllByTestId("site-row");
    const me = rowOf("");
    expect(me.textContent).toContain("amy-mini");
    expect(me.textContent).toContain("這台");
    expect(me.textContent).toContain("版本 1.4.0");
    expect(within(me).queryByRole("button", { name: /拿掉/ })).toBeNull();
    const office = rowOf("amy-office");
    expect(within(office).getByTestId("site-state").textContent).toBe("被拒");
    expect(within(office).getByRole("status").textContent).toContain("這台只接受主人的裝置");
    expect(office.textContent).toContain(
      `把這個看板（${window.location.origin}）加進「誰可以連進這台」`,
    );
    expect(within(rowOf(DEV)).getByTestId("site-state").textContent).toBe("連不上");
  });

  test("拿掉一台：確認後整份換掉（其他台與顏色留著）、篩選裡那台的秘書一起清、重新試連", async () => {
    agentFilterStore.set(["code", "amy-office/code"]);
    wrap();
    await screen.findAllByTestId("site-row");
    await waitFor(() => expect(state.getSites).toHaveBeenCalled());
    fireEvent.click(
      within(rowOf("amy-office")).getByRole("button", { name: "拿掉「amy-office」" }),
    );
    expect(state.setSites).not.toHaveBeenCalled();
    fireEvent.click(within(rowOf("amy-office")).getByRole("button", { name: "確定拿掉" }));
    await waitFor(() => expect(state.setSites).toHaveBeenCalledWith({ sites: [{ url: DEV }] }));
    await waitFor(() => expect(agentFilterStore.get()).toEqual(["code"]));
    await waitFor(() => expect(state.reprobe).toHaveBeenCalled());
  });

  test("加一台：網址＋選的顏色接在最後送出，送完清空並重新試連；伺服器擋下時寫原因", async () => {
    wrap();
    await waitFor(() => expect(state.getSites).toHaveBeenCalled());
    const form = screen.getByTestId("site-add");
    fireEvent.change(within(form).getByLabelText(/那台看板的網址/), {
      target: { value: " https://genie-amy-x.example.ts.net " },
    });
    fireEvent.click(within(form).getByLabelText("綠"));
    fireEvent.click(within(form).getByRole("button", { name: "加入" }));
    await waitFor(() =>
      expect(state.setSites).toHaveBeenCalledWith({
        sites: [
          { url: OFFICE, color: "#3b82f6" },
          { url: DEV },
          { url: "https://genie-amy-x.example.ts.net", color: "#22c55e" },
        ],
      }),
    );
    await waitFor(() => expect(state.reprobe).toHaveBeenCalled());
    await waitFor(() =>
      expect((within(form).getByLabelText(/那台看板的網址/) as HTMLInputElement).value).toBe(""),
    );

    const { ApiError } = await import("@evergenie/sdk-web");
    state.setSites.mockRejectedValueOnce(new ApiError("invalid", "網址請寫 https://…"));
    fireEvent.change(within(form).getByLabelText(/那台看板的網址/), { target: { value: "x" } });
    fireEvent.click(within(form).getByRole("button", { name: "加入" }));
    expect((await within(form).findByRole("alert")).textContent).toContain("網址請寫 https://…");
  });
});

describe("設定 → 電腦：台清單整份換掉時不蓋掉別人改的", () => {
  test("台清單讀不到時不能加、不能拿掉，寫讀不到並可重試", async () => {
    state.getSites.mockRejectedValue(new Error("核心服務尚未啟動"));
    wrap();
    // 台清單與這台的信任設定都讀不到：兩區各一個「讀不到」。
    await waitFor(() => expect(screen.getAllByRole("alert")).toHaveLength(2));
    const form = screen.getByTestId("site-add");
    fireEvent.change(within(form).getByLabelText(/那台看板的網址/), {
      target: { value: "https://genie-amy-x.example.ts.net" },
    });
    expect((within(form).getByRole("button", { name: "加入" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(screen.queryByRole("button", { name: /拿掉「/ })).toBeNull();
    expect(state.setSites).not.toHaveBeenCalled();
  });

  test("加一台前先重讀最新那份：別處剛加的台不會被蓋掉", async () => {
    wrap();
    await waitFor(() => expect(state.getSites).toHaveBeenCalled());
    const NEW = "https://genie-amy-cli.example.ts.net";
    state.getSites.mockResolvedValue({ sites: [{ url: OFFICE }, { url: DEV }, { url: NEW }] });
    const form = screen.getByTestId("site-add");
    fireEvent.change(within(form).getByLabelText(/那台看板的網址/), {
      target: { value: "https://genie-amy-x.example.ts.net" },
    });
    fireEvent.click(within(form).getByRole("button", { name: "加入" }));
    await waitFor(() =>
      expect(state.setSites).toHaveBeenCalledWith({
        sites: [
          { url: OFFICE },
          { url: DEV },
          { url: NEW },
          { url: "https://genie-amy-x.example.ts.net" },
        ],
      }),
    );
  });

  test("正式／開發不同而被拒時，不叫人去那台允許這個看板（允許了也沒用）", async () => {
    state.sites = [
      primary,
      site({
        key: DEV,
        base: DEV,
        env: "dev",
        state: "denied",
        reason: "這是開發家，正式看板不看",
      }),
    ];
    wrap();
    const row = (await screen.findAllByTestId("site-row")).find(
      (r) => r.getAttribute("data-site") === DEV,
    );
    expect(row?.textContent).toContain("這是開發家，正式看板不看");
    expect(row?.textContent).not.toContain("加進「誰可以連進這台」");
  });
});

describe("設定 → 電腦：誰可以連進這台", () => {
  test("這台的主人登入與允許清單可以改（整份送出）", async () => {
    wrap();
    const box = await screen.findByTestId("local-trust");
    const owner = within(box).getByLabelText(/主人的 tailnet 登入/) as HTMLInputElement;
    expect(owner.value).toBe("me@example.com");
    fireEvent.change(owner, { target: { value: " other@example.com " } });
    fireEvent.click(within(box).getByRole("button", { name: "儲存" }));
    await waitFor(() =>
      expect(state.setOwnerLogin).toHaveBeenCalledWith({ owner_login: "other@example.com" }),
    );

    expect(
      within(box)
        .getAllByTestId("allow-board")
        .map((li) => li.textContent),
    ).toEqual([`${DEV}拿掉`]);
    fireEvent.change(within(box).getByLabelText("要允許的看板網址"), { target: { value: OFFICE } });
    fireEvent.click(within(box).getByRole("button", { name: "允許" }));
    await waitFor(() =>
      expect(state.setAllowBoards).toHaveBeenCalledWith({ allow_boards: [DEV, OFFICE] }),
    );
    fireEvent.click(within(box).getByRole("button", { name: `拿掉「${DEV}」` }));
    await waitFor(() =>
      expect(state.setAllowBoards).toHaveBeenLastCalledWith({ allow_boards: [] }),
    );
  });
});
