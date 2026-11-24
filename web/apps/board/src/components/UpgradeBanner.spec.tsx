import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { UpgradeBanner } from "./UpgradeBanner";

const api = vi.hoisted(() => ({
  status: vi.fn(),
  apply: vi.fn(),
  rollback: vi.fn(),
}));

vi.mock("@evergenie/sdk-web", () => {
  class ApiError extends Error {
    constructor(
      readonly code: string,
      message: string,
    ) {
      super(message);
    }
  }
  return {
    ApiError,
    api: {
      system: {
        updateStatus: api.status,
        updateApply: api.apply,
        updateRollback: api.rollback,
      },
    },
  };
});

const recent = new Date(Date.now() - 10 * 60_000).toISOString();
const base = { current: "0.2.0", channel: "release", core_found: true, tonight_at: "03:30" };
const log = "/Users/me/evergenie/runtime/logs/upgrade.log";
const untouched = {
  ...base,
  target: "0.3.0",
  tag: "v0.3.0",
  state: {
    kind: "upgrade",
    phase: "failed",
    outcome: "untouched",
    from: "0.2.0",
    to: "0.3.0",
    ok: false,
    reason: "下載不到新版的檔案，沒有升級；舊版照常在跑",
    next: "等網路正常了再更新一次",
    log,
    started_at: recent,
    finished_at: recent,
  },
};
const rolledBack = {
  ...base,
  skipped: ["0.3.0"],
  state: {
    ...untouched.state,
    phase: "rolled_back",
    outcome: "rolled_back",
    reason: "新版檢查沒過：重開之後服務一直沒有起來",
    next: "這一版先跳過，已經請電腦秘書查原因；你不用做什麼，舊版照常可以用",
  },
};
const stranded = {
  ...base,
  current: "0.3.0",
  rescue: true,
  state: {
    ...untouched.state,
    outcome: "stranded",
    reason: "新版檢查沒過；換回時換不回舊版的資料夾",
    next: "到看板首頁按「退回上一版」再試一次換回；還是不行就請電腦秘書處理",
  },
};

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <UpgradeBanner />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset();
  try {
    window.localStorage.clear();
  } catch {
    // 測試環境沒有 localStorage 也照跑
  }
});
afterEach(() => cleanup());

describe("今天頁的升級結果橫幅（WEB-173）", () => {
  test("TestUpgradeBannerUntouched：舊版照常時寫結果、原因、下一步，再試一次要先確認、送目前的新版，之後就地顯示進度", async () => {
    api.status.mockResolvedValue(untouched);
    api.apply.mockResolvedValue({
      ...untouched,
      state: {
        ...untouched.state,
        phase: "fetch",
        outcome: undefined,
        started_at: new Date().toISOString(),
      },
    });
    mount();
    const banner = await screen.findByTestId("upgrade-banner");
    expect(banner.getAttribute("data-outcome")).toBe("untouched");
    expect(within(banner).getByTestId("update-headline").textContent).toBe(
      "更新到 0.3.0 沒成功，舊版 0.2.0 照常在跑。",
    );
    expect(within(banner).getByTestId("update-next").textContent).toBe(
      "下一步：等網路正常了再更新一次",
    );
    expect(within(banner).queryByRole("button", { name: "退回上一版" })).toBeNull();

    // 看細節：現在的版本、這次的版本、升級日誌的位置
    const more = within(banner).getByRole("button", { name: "看細節" });
    expect(more.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(more);
    const details = within(banner).getByTestId("upgrade-details");
    expect(details.textContent).toContain("現在的版本：0.2.0");
    expect(details.textContent).toContain("這次：0.2.0 → 0.3.0");
    expect(details.textContent).toContain(log);
    expect(
      within(banner).getByRole("button", { name: "收起細節" }).getAttribute("aria-expanded"),
    ).toBe("true");

    fireEvent.click(within(banner).getByRole("button", { name: "再試一次" }));
    const dialog = await screen.findByRole("dialog");
    expect(api.apply).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "更新" }));
    await waitFor(() => expect(api.apply).toHaveBeenCalledWith({ version: "0.3.0" }));
    expect((await screen.findByTestId("update-progress")).textContent).toContain(
      "正在更新到 0.3.0",
    );
  });

  test("TestUpgradeBannerRolledBack：已換回時說資料沒動，沒有再試一次（那一版跳過了）；知道了就收起來，重新整理也不再出現", async () => {
    api.status.mockResolvedValue(rolledBack);
    const view = mount();
    const banner = await screen.findByTestId("upgrade-banner");
    expect(banner.getAttribute("data-outcome")).toBe("rolled_back");
    expect(within(banner).getByTestId("update-headline").textContent).toBe(
      "更新到 0.3.0 沒成功，已換回 0.2.0，你的資料沒動。",
    );
    expect(within(banner).queryByRole("button", { name: "再試一次" })).toBeNull();
    fireEvent.click(within(banner).getByRole("button", { name: "知道了" }));
    expect(screen.queryByTestId("upgrade-banner")).toBeNull();
    view.unmount();
    mount();
    await waitFor(() => expect(api.status).toHaveBeenCalledTimes(2));
    expect(screen.queryByTestId("upgrade-banner")).toBeNull();
  });

  test("TestUpgradeBannerStranded：也沒換回時要處理：退回上一版要先確認、送 update_rollback、顯示正在換回；不給收起來", async () => {
    api.status.mockResolvedValue(stranded);
    api.rollback.mockResolvedValue({
      ...stranded,
      state: {
        kind: "rescue",
        phase: "build",
        from: "0.2.0",
        to: "0.3.0",
        ok: false,
        started_at: new Date().toISOString(),
      },
    });
    mount();
    const banner = await screen.findByTestId("upgrade-banner");
    expect(banner.getAttribute("data-outcome")).toBe("stranded");
    expect(within(banner).getByTestId("update-headline").textContent).toBe(
      "更新到 0.3.0 沒成功，也沒能換回 0.2.0，要處理。",
    );
    expect(within(banner).queryByRole("button", { name: "知道了" })).toBeNull();
    expect(within(banner).queryByRole("button", { name: "再試一次" })).toBeNull();
    fireEvent.click(within(banner).getByRole("button", { name: "退回上一版" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("退回 0.2.0？");
    expect(api.rollback).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "退回" }));
    await waitFor(() => expect(api.rollback).toHaveBeenCalledWith({}));
    expect((await screen.findByTestId("update-progress")).textContent).toContain("正在換回 0.2.0");
  });

  test("TestUpgradeBannerRefused：退回被拒絕時白話原因寫在橫幅裡，橫幅還在", async () => {
    const { ApiError } = await import("@evergenie/sdk-web");
    api.status.mockResolvedValue(stranded);
    api.rollback.mockRejectedValue(new ApiError("forbidden", "只有主人能換回舊版"));
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "退回上一版" }));
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "退回" }),
    );
    expect((await screen.findByRole("alert")).textContent).toBe("沒有送出：只有主人能換回舊版");
    expect(screen.getByTestId("upgrade-banner")).not.toBeNull();
  });

  test("TestUpgradeBannerRescueDone：退回上一版換回了：標題說已換回，不再重複寫原因，可以收起來", async () => {
    api.status.mockResolvedValue({
      ...base,
      state: {
        ...rolledBack.state,
        kind: "rescue",
        from: "0.2.0",
        to: "0.3.0",
        reason: "已換回 0.2.0；0.3.0 這一版先跳過",
      },
    });
    mount();
    const banner = await screen.findByTestId("upgrade-banner");
    expect(within(banner).getByTestId("update-headline").textContent).toBe(
      "已換回 0.2.0，0.3.0 這一版先跳過。",
    );
    expect(within(banner).queryByTestId("update-reason")).toBeNull();
    expect(within(banner).queryByRole("button", { name: "退回上一版" })).toBeNull();
    expect(within(banner).getByRole("button", { name: "知道了" })).not.toBeNull();
  });

  test("TestUpgradeBannerStrandedWithoutRescue：手動退回退到一半（沒有可以照著換回的紀錄）：沒有退回上一版，但可以收起來；看細節收起時不指到不存在的區塊", async () => {
    api.status.mockResolvedValue({
      ...base,
      state: {
        ...stranded.state,
        kind: "rollback",
        from: "0.3.0",
        to: "0.2.0",
        next: "請電腦秘書幫忙看（細節記在升級日誌）",
      },
    });
    mount();
    const banner = await screen.findByTestId("upgrade-banner");
    expect(banner.getAttribute("data-outcome")).toBe("stranded");
    expect(within(banner).getByTestId("update-headline").textContent).toBe(
      "退回 0.2.0 退到一半沒成功，要處理。",
    );
    expect(within(banner).queryByRole("button", { name: "退回上一版" })).toBeNull();
    const more = within(banner).getByRole("button", { name: "看細節" });
    expect(more.getAttribute("aria-controls")).toBeNull();
    fireEvent.click(more);
    const controls = within(banner)
      .getByRole("button", { name: "收起細節" })
      .getAttribute("aria-controls");
    expect(controls && document.getElementById(controls)).not.toBeNull();
    fireEvent.click(within(banner).getByRole("button", { name: "知道了" }));
    expect(screen.queryByTestId("upgrade-banner")).toBeNull();
  });

  test("TestUpgradeBannerQuiet：成功、沒有紀錄、一星期前的沒成功都不擺；也沒換回的過再久都擺", async () => {
    const old = new Date(Date.now() - 8 * 24 * 60 * 60_000).toISOString();
    for (const info of [
      {
        ...base,
        state: { phase: "done", from: "0.2.0", to: "0.3.0", ok: true, started_at: recent },
      },
      { ...base },
      { ...rolledBack, state: { ...rolledBack.state, started_at: old, finished_at: old } },
      { ...untouched, state: { ...untouched.state, phase: "fetch", outcome: undefined } },
    ]) {
      api.status.mockResolvedValue(info);
      const view = mount();
      await waitFor(() => expect(api.status).toHaveBeenCalled());
      await new Promise((r) => setTimeout(r, 0));
      expect(screen.queryByTestId("upgrade-banner")).toBeNull();
      view.unmount();
      api.status.mockReset();
    }
    api.status.mockResolvedValue({
      ...stranded,
      state: { ...stranded.state, started_at: old, finished_at: old },
    });
    mount();
    expect((await screen.findByTestId("upgrade-banner")).getAttribute("data-outcome")).toBe(
      "stranded",
    );
  });
});
