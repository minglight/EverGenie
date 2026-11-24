import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { PackageOutdated } from "./PackageOutdated";
import { PackageRow } from "./PackageRow";
import { PackageUrlInstall } from "./PackageUrlInstall";
import { sourceLabel } from "./packageSource";

const state = vi.hoisted(() => ({
  preview: vi.fn(),
  install: vi.fn(),
  outdated: vi.fn(),
  upgrade: vi.fn(),
}));

vi.mock("@evergenie/sdk-web", () => ({
  api: {
    package: {
      preview: state.preview,
      install: state.install,
      outdated: state.outdated,
      upgrade: state.upgrade,
      list: async () => ({ packages: [] }),
      listExtensions: async () => ({}),
    },
  },
  ApiError: class ApiError extends Error {},
}));

const okPreview = {
  id: "reading",
  name: "閱讀清單",
  version: "1.2.0",
  description: "記下想讀的書",
  kind: "program",
  runs_program: true,
  agents: ["reader"],
  pages: ["書架"],
  jobs: [],
  data_kinds: ["reading.book"],
  reads: [],
  writes: ["reading.book"],
  exists: false,
};

function wrap(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

function openAndLook(url = "https://github.com/someone/reading.git") {
  wrap(<PackageUrlInstall />);
  fireEvent.click(screen.getByRole("button", { name: "從網址安裝" }));
  fireEvent.change(screen.getByLabelText("套件的網址"), { target: { value: url } });
  fireEvent.click(screen.getByRole("button", { name: "看看" }));
}

beforeEach(() => {
  for (const f of Object.values(state)) f.mockReset();
  state.preview.mockResolvedValue(okPreview);
  state.install.mockResolvedValue({ package: { id: "reading", name: "閱讀清單" } });
  state.outdated.mockResolvedValue({ packages: [], failed: [] });
  state.upgrade.mockResolvedValue({ from: "1.0.0", to: "1.2.0" });
});
afterEach(() => cleanup());

describe("PackageUrlInstall", () => {
  // Guards: REQ-282
  test("TestPreviewShowsWhatItBrings：先預覽，列出名稱、版本、會執行程式、帶來的東西，還沒有安裝", async () => {
    openAndLook();
    const card = await screen.findByTestId("package-url-preview");
    expect(state.preview).toHaveBeenCalledWith({
      source: "https://github.com/someone/reading.git",
      ref: undefined,
    });
    expect(within(card).getByText("閱讀清單")).not.toBeNull();
    expect(within(card).getByText("版本 1.2.0")).not.toBeNull();
    expect(within(card).getByTestId("package-url-runs").textContent).toContain("在你的電腦上執行");
    expect(within(card).getByText("秘書：reader")).not.toBeNull();
    expect(within(card).getByText("頁面：書架")).not.toBeNull();
    expect(within(card).getByText("會寫：reading.book")).not.toBeNull();
    expect(state.install).not.toHaveBeenCalled();
  });

  // Guards: REQ-282
  test("TestProblemsBlockInstall：預覽有問題就紅字列出，安裝鈕停用，不會送安裝", async () => {
    state.preview.mockResolvedValue({
      ...okPreview,
      runs_program: false,
      problems: ["這個套件要 EverGenie >=9.0.0，你現在是 0.2.0"],
    });
    openAndLook();
    const problems = await screen.findByTestId("package-url-problems");
    expect(problems.textContent).toContain("要 EverGenie >=9.0.0");
    expect(screen.queryByTestId("package-url-runs")).toBeNull();
    const install = screen.getByRole("button", { name: "安裝" }) as HTMLButtonElement;
    expect(install.disabled).toBe(true);
    fireEvent.click(install);
    expect(state.install).not.toHaveBeenCalled();
  });

  // Guards: REQ-282
  test("TestInstallSendsConfirmed：預覽後按安裝，送出網址、版本與 confirmed: true，只送一次", async () => {
    wrap(<PackageUrlInstall />);
    fireEvent.click(screen.getByRole("button", { name: "從網址安裝" }));
    fireEvent.change(screen.getByLabelText("套件的網址"), {
      target: { value: " https://github.com/someone/reading " },
    });
    fireEvent.change(screen.getByLabelText("指定版本（選填）"), { target: { value: "v1.2.0" } });
    fireEvent.click(screen.getByRole("button", { name: "看看" }));
    await screen.findByTestId("package-url-preview");
    const install = screen.getByRole("button", { name: "安裝" });
    fireEvent.click(install);
    fireEvent.click(install);
    await waitFor(() => expect(screen.queryByTestId("package-url-preview")).toBeNull());
    expect(state.install).toHaveBeenCalledTimes(1);
    expect(state.install).toHaveBeenCalledWith({
      source: "https://github.com/someone/reading",
      ref: "v1.2.0",
      confirmed: true,
    });
  });

  // Guards: REQ-282
  test("TestEditingUrlDropsPreview：改了網址，舊的預覽就收掉，要重新看過才能裝", async () => {
    openAndLook();
    await screen.findByTestId("package-url-preview");
    fireEvent.change(screen.getByLabelText("套件的網址"), {
      target: { value: "https://github.com/other/pack" },
    });
    expect(screen.queryByTestId("package-url-preview")).toBeNull();
    expect(screen.queryByRole("button", { name: "安裝" })).toBeNull();
  });

  // Guards: REQ-282
  test("TestAlreadyInstalledBlocks：已經裝了同一個，說明用更新，不能再裝", async () => {
    state.preview.mockResolvedValue({ ...okPreview, exists: true, warnings: ["已經裝了"] });
    openAndLook();
    await screen.findByTestId("package-url-preview");
    expect(screen.getByText(/已經裝了。要換新版請用上面的「更新」/)).not.toBeNull();
    expect((screen.getByRole("button", { name: "安裝" }) as HTMLButtonElement).disabled).toBe(true);
  });

  // Guards: REQ-282
  test("預覽失敗顯示白話原因，沒有預覽卡", async () => {
    state.preview.mockRejectedValue(new Error("連不上"));
    openAndLook();
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("沒做成功"));
    expect(screen.queryByTestId("package-url-preview")).toBeNull();
  });
});

describe("PackageOutdated", () => {
  // Guards: REQ-282
  test("TestUpgradeConfirmsFirst：有新版列出〔更新到〕，先確認，確認後才送 upgrade", async () => {
    state.outdated.mockResolvedValue({
      packages: [{ id: "reading", name: "閱讀清單", current: "1.0.0", latest: "1.2.0" }],
      failed: [],
    });
    wrap(<PackageOutdated />);
    expect((await screen.findByTestId("packages-outdated")).textContent).toContain(
      "閱讀清單：1.0.0 → 1.2.0",
    );
    fireEvent.click(screen.getByRole("button", { name: "更新到 1.2.0" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("你的資料會保留");
    expect(state.upgrade).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "先不要" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(state.upgrade).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "更新到 1.2.0" }));
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "更新" }),
    );
    await waitFor(() => expect(state.upgrade).toHaveBeenCalledTimes(1));
    expect(state.upgrade).toHaveBeenCalledWith({ id: "reading", ref: "1.2.0" });
  });

  // Guards: REQ-282
  test("沒有新版、也沒有查失敗的，整塊不出現", async () => {
    wrap(<PackageOutdated />);
    await waitFor(() => expect(state.outdated).toHaveBeenCalled());
    expect(screen.queryByTestId("packages-outdated")).toBeNull();
  });

  // Guards: REQ-282
  test("查不到新版只輕輕提一句，不當錯誤", async () => {
    state.outdated.mockResolvedValue({ packages: [], failed: ["reading"] });
    wrap(<PackageOutdated />);
    expect((await screen.findByTestId("packages-outdated")).textContent).toContain(
      "有 1 個套件這次沒查到新版",
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("來源標示", () => {
  // Guards: REQ-282
  test("TestSourceShown：從網址裝的套件列標「來自 主機/路徑」，官方與本機資料夾裝的各有說法", () => {
    expect(sourceLabel("https://github.com/someone/reading.git")).toBe(
      "來自 github.com/someone/reading",
    );
    expect(sourceLabel("git@github.com:someone/reading.git")).toBe(
      "來自 github.com/someone/reading",
    );
    expect(sourceLabel("ssh://git@host.example/team/pack/")).toBe("來自 host.example/team/pack");
    expect(sourceLabel("file:///Users/x/pack")).toBe("來自本機資料夾");
    expect(sourceLabel(undefined)).toBe("");

    wrap(
      <PackageRow
        pkg={{
          id: "reading",
          name: "閱讀清單",
          version: "1.0.0",
          tier: "installed",
          state: "running",
          enabled: true,
          source: {
            url: "https://github.com/someone/reading.git",
            installed_at: "2026-10-29T00:00:00Z",
          },
        }}
      />,
    );
    expect(screen.getByTestId("package-source-reading").textContent).toBe(
      "來自 github.com/someone/reading",
    );
  });

  // Guards: REQ-282
  test("沒有 source 的套件不顯示來源", () => {
    wrap(
      <PackageRow
        pkg={{ id: "habits", name: "習慣", version: "1.0.0", tier: "official", enabled: true }}
      />,
    );
    expect(screen.queryByTestId("package-source-habits")).toBeNull();
  });
});
