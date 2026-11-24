import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { OptionalPackages } from "./OptionalPackages";
import { SetupSettings } from "./SetupSettings";

const state = vi.hoisted(() => ({
  agents: [] as unknown[],
  packages: [] as unknown[],
  setEnabled: vi.fn(),
  sync: vi.fn(),
  setField: vi.fn(),
}));

vi.mock("@evergenie/sdk-web", () => ({
  api: {
    web: {
      getAgents: async () => ({ agents: state.agents, accounts: ["main"] }),
      getRoster: async () => ({ agents: [] }),
    },
    personas: {
      list: async () => ({
        personas: [
          { slug: "athena", name: "雅典娜", status: "live", seats: ["code"], shadowed: false },
          { slug: "kaguya", name: "輝夜", status: "live", seats: [], shadowed: false },
          { slug: "draft", name: "草稿", status: "design", seats: [], shadowed: false },
          {
            slug: "secretaries.fairy",
            name: "小仙子",
            source: "package",
            package: "secretaries",
            status: "live",
            seats: [],
            shadowed: false,
          },
          {
            slug: "off.ghost",
            name: "停用套件的角色",
            source: "package",
            package: "off",
            status: "live",
            unassignable: "已停用",
            seats: [],
            shadowed: false,
          },
        ],
      }),
    },
    package: {
      list: async () => ({ packages: state.packages }),
      setEnabled: state.setEnabled,
      sync: state.sync,
    },
    agents: { setField: state.setField, setAccount: vi.fn(), assign: vi.fn() },
  },
  ApiError: class ApiError extends Error {},
}));
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
}));

function renderWizard() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <SetupSettings />
    </QueryClientProvider>,
  );
}

// 選用套件的例子：開發工坊（admin）。closet-mine 2026-11-03 起預設啟用、不再是選用（DEV-112）。
const optionalPkg = {
  id: "admin",
  name: "開發工坊",
  description: "開發 EverGenie 用的工具",
  optional: true,
  enabled: false,
  state: "stopped",
};

const secretaries = {
  id: "secretaries",
  name: "基本秘書",
  enabled: true,
  state: "running",
  agents: [
    { id: "code", duty: "寫程式：讀 code" },
    { id: "work", duty: "工作：行程、會議" },
  ],
};

beforeEach(() => {
  state.agents = [
    { id: "code", duty: "寫程式：讀 code", persona: "athena", name: "雅典娜", enabled: true },
  ];
  state.packages = [];
  state.setEnabled.mockReset();
  state.sync.mockReset();
  state.setField.mockReset();
});
afterEach(() => cleanup());

function renderOptional(packages: unknown[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <OptionalPackages packages={packages as never} />
    </QueryClientProvider>,
  );
}

describe("SetupSettings：兩步", () => {
  // 作者 2026-10-28：選職位＋每個職位選角色就能開始用；權限、套件、帳號、模型之後再談。
  // Guards: REQ-238, REQ-237
  test("TestWizardTwoSteps：只有「選職位與角色」和「開始用」，沒有權限、套件、介面", async () => {
    renderWizard();
    const step1 = await screen.findByTestId("setup-step-agents");
    expect(step1.textContent).toContain("1. 選職位，每個職位請一位角色擔任");
    expect(screen.getByTestId("setup-step-start").textContent).toContain("2. 開始用");
    expect(screen.getByText("到今天").getAttribute("href")).toBe("/");
    for (const gone of [
      "setup-step-permission",
      "setup-step-packages",
      "setup-step-optional",
      "setup-step-mode",
    ]) {
      expect(screen.queryByTestId(gone)).toBeNull();
    }
    // 精靈裡的職位卡只有負責的人與開關，沒有帳號、模型、思考力度。
    expect(screen.getByTestId("agent-row").getAttribute("data-agent")).toBe("code");
    expect(screen.getByTestId("agent-model").closest(".hidden")).not.toBeNull();
    expect(screen.getByTestId("setup-push").textContent).toContain("選用");
  });

  // Guards: REQ-238
  test("TestWizardClaimsFunction：還沒人負責的職位選角色、按「請〈角色名字〉上班」→ 建好並啟用", async () => {
    state.packages = [secretaries];
    state.sync.mockResolvedValue({ applied: true, agents: [{ id: "work", action: "create" }] });
    state.setField.mockResolvedValue({});
    renderWizard();
    const card = await screen.findByTestId("function-unclaimed");
    expect(card.getAttribute("data-agent")).toBe("work");
    expect(card.textContent).toContain("工作");
    // 預設是還沒坐位置的角色；設計中的不列。
    const select = screen.getByTestId("claim-persona") as HTMLSelectElement;
    // 職位 work 自己帶的角色優先（SEC-302 S3）；設計中、所屬套件停用的不列；依來源分組（我的在前、套件在後）。
    expect(select.value).toBe("secretaries.fairy");
    expect([...select.options].map((o) => o.value)).toEqual([
      "athena",
      "kaguya",
      "secretaries.fairy",
    ]);
    expect([...select.querySelectorAll("optgroup")].map((g) => g.label)).toEqual([
      "我的",
      "套件 secretaries",
    ]);
    // 鈕上寫預設那位的名字；換了角色，鈕跟著換名字（不寫「她」，DEV-156 U-24）。
    expect(screen.getByRole("button", { name: "請小仙子上班" })).toBeTruthy();
    fireEvent.change(select, { target: { value: "athena" } });
    fireEvent.click(screen.getByRole("button", { name: "請雅典娜上班" }));
    await waitFor(() =>
      expect(state.setField).toHaveBeenCalledWith({ id: "work", field: "enabled", value: "true" }),
    );
    expect(state.sync).toHaveBeenCalledWith({
      id: "secretaries",
      agent: "work",
      persona: "athena",
      apply: true,
    });
  });

  // Guards: REQ-238
  test("TestWizardWaitsForFirstAgent：還沒有秘書上班時，第二步請他先選", async () => {
    state.agents = [];
    state.packages = [secretaries];
    renderWizard();
    await screen.findByTestId("setup-step-start");
    expect(screen.queryByText("到今天")).toBeNull();
    expect(screen.getByTestId("setup-step-start").textContent).toContain(
      "先在上面請至少一位秘書上班",
    );
    expect(
      (await screen.findAllByTestId("function-unclaimed")).map((c) => c.getAttribute("data-agent")),
    ).toEqual(["code", "work"]);
  });
});

describe("選用套件（設定 → 套件最上面的「進階套件」）", () => {
  // Guards: REQ-176
  test("TestOptionalListsPackagesAndToggles：列出選用套件，開關送 set_enabled", async () => {
    state.setEnabled.mockResolvedValue({
      package: { ...optionalPkg, enabled: true, state: "running" },
    });
    renderOptional([optionalPkg]);
    expect(screen.getByTestId("optional-admin").textContent).toContain("開發工坊");
    const toggle = screen.getByRole("switch", { name: "啟用 開發工坊" });
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(toggle);
    await waitFor(() =>
      expect(state.setEnabled).toHaveBeenCalledWith({ id: "admin", enabled: true }),
    );
  });

  // Guards: REQ-176
  test("TestOptionalShowsRejectReason：被拒（依賴沒開）顯示原因", async () => {
    state.setEnabled.mockResolvedValue({
      package: {
        ...optionalPkg,
        enabled: true,
        state: "rejected",
        error: "需要套件「habits」，但它已停用",
      },
    });
    renderOptional([optionalPkg]);
    fireEvent.click(await screen.findByRole("switch", { name: "啟用 開發工坊" }));
    await waitFor(() => expect(screen.getByText("需要套件「habits」，但它已停用")).not.toBeNull());
  });
});
