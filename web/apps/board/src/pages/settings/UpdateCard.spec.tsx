import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { UpdateCard } from "./UpdateCard";

const state = vi.hoisted(() => ({
  status: vi.fn(),
  check: vi.fn(),
  apply: vi.fn(),
  decide: vi.fn(),
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
        updateStatus: state.status,
        updateCheck: state.check,
        updateApply: state.apply,
        updateDecide: state.decide,
        updateRollback: state.rollback,
      },
    },
  };
});

const base = {
  current: "0.2.0",
  channel: "release",
  core_found: true,
  tonight_at: "03:30",
};
const newer = {
  ...base,
  target: "0.3.0",
  tag: "v0.3.0",
  notes: "## 新東西\n- 看板可以直接更新",
};

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <UpdateCard />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  for (const f of Object.values(state)) f.mockReset();
  state.status.mockResolvedValue(newer);
  state.check.mockResolvedValue(newer);
  state.apply.mockResolvedValue({ ...newer, state: undefined });
  state.decide.mockResolvedValue({ ...base });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("版本與更新卡（REQ-281）", () => {
  // Guards: REQ-281
  test("TestUpdateCardShowsNewVersion：現在版本、更新方式用白話、有新版時列版號與發版說明和三個按鈕", async () => {
    mount();
    const card = within(await screen.findByTestId("update-card"));
    expect(card.getByTestId("update-current").textContent).toBe("0.2.0");
    expect(card.getByTestId("update-channel").textContent).toBe("只升正式版");
    expect(card.getByText("有新版 0.3.0")).not.toBeNull();
    expect(card.getByText("看板可以直接更新")).not.toBeNull();
    expect(card.getByRole("button", { name: "現在更新" })).not.toBeNull();
    expect(card.getByRole("button", { name: "今晚 03:30 更新" })).not.toBeNull();
    expect(card.getByRole("button", { name: "跳過這版" })).not.toBeNull();
    expect(card.getByRole("button", { name: "檢查更新" })).not.toBeNull();
  });

  // Guards: REQ-281
  test("TestUpdateCardChannelWords：另外兩種更新方式也是白話", async () => {
    state.status.mockResolvedValue({ ...base, channel: "safe" });
    mount();
    expect((await screen.findByTestId("update-channel")).textContent).toBe("也升測試過的版本");
    cleanup();
    state.status.mockResolvedValue({ ...base, channel: "off" });
    mount();
    await waitFor(() =>
      expect(screen.getByTestId("update-channel").textContent).toBe("不自動檢查"),
    );
  });

  // Guards: REQ-281
  test("TestUpdateCardNoNewVersion：沒有新版就說已經最新（或後端給的原因），不出現三個按鈕", async () => {
    state.status.mockResolvedValue({ ...base, detail: "這版你跳過了" });
    mount();
    expect((await screen.findByTestId("update-latest")).textContent).toBe("這版你跳過了");
    expect(screen.queryByRole("button", { name: "現在更新" })).toBeNull();
    expect(screen.getByRole("button", { name: "檢查更新" })).not.toBeNull();
  });

  // Guards: REQ-281
  test("TestUpdateCardCoreNotFound：沒找到程式時只說明，沒有更新按鈕", async () => {
    state.status.mockResolvedValue({ ...base, core_found: false, target: "0.3.0" });
    mount();
    expect((await screen.findByTestId("update-no-core")).textContent).toBe(
      "這台不是用安裝程式裝的，沒辦法自動升級。",
    );
    expect(screen.queryByRole("button", { name: "現在更新" })).toBeNull();
    expect(screen.queryByRole("button", { name: "檢查更新" })).toBeNull();
  });

  // Guards: REQ-281
  test("TestUpdateCardNowAsksFirst：現在更新要先在畫面內確認，確認後才送，沒按確認不送", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "現在更新" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("現在更新到 0.3.0？")).not.toBeNull();
    expect(dialog.textContent).toContain("會重開 EverGenie");
    expect(dialog.textContent).toContain("沒成功會自動換回 0.2.0");
    expect(state.apply).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "先不要" }));
    expect(state.apply).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "現在更新" }));
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "更新" }),
    );
    await waitFor(() => expect(state.apply).toHaveBeenCalledWith({ version: "0.3.0" }));
    // 送出之後馬上進入「升級中」，不會讓人以為沒按成。
    expect(await screen.findByTestId("update-progress")).not.toBeNull();
    expect(screen.queryByRole("button", { name: "現在更新" })).toBeNull();
  });

  // Guards: REQ-281
  test("TestUpdateCardTonightAndSkip：今晚更新與跳過這版各送對的 decision 和版本", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "今晚 03:30 更新" }));
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith({ version: "0.3.0", decision: "tonight" }),
    );
    // 回來的現況沒有新版了（例如跳過）也能正常畫出來。
    await waitFor(() => expect(screen.queryByText("有新版 0.3.0")).toBeNull());

    cleanup();
    state.status.mockResolvedValue(newer);
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "跳過這版" }));
    await waitFor(() =>
      expect(state.decide).toHaveBeenLastCalledWith({ version: "0.3.0", decision: "skip" }),
    );
    expect(state.apply).not.toHaveBeenCalled();
  });

  // Guards: REQ-281
  test("TestUpdateCardTonightAlreadySet：已經排了今晚更新就寫出時間，不再給今晚的按鈕", async () => {
    state.status.mockResolvedValue({ ...newer, decision: "tonight" });
    mount();
    expect((await screen.findByTestId("update-tonight-set")).textContent).toBe(
      "已排好今晚 03:30 更新，到時候會自己重開。",
    );
    expect(screen.queryByRole("button", { name: "今晚 03:30 更新" })).toBeNull();
    expect(screen.getByRole("button", { name: "現在更新" })).not.toBeNull();
  });

  // Guards: REQ-281
  test("TestUpdateCardCheckNow：檢查更新送出後用回來的結果換掉畫面", async () => {
    state.status.mockResolvedValue({ ...base });
    state.check.mockResolvedValue(newer);
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "檢查更新" }));
    expect(await screen.findByText("有新版 0.3.0")).not.toBeNull();
    expect(state.check).toHaveBeenCalledTimes(1);
  });

  // Guards: REQ-281
  test("TestUpdateCardUpgrading：升級中顯示白話進度，每 3 秒再抓一次", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const running = (phase: string) => ({
      ...newer,
      state: { phase, from: "0.2.0", to: "0.3.0", ok: false },
    });
    state.status.mockResolvedValueOnce(running("fetch")).mockResolvedValue(running("build"));
    mount();
    expect((await screen.findByTestId("update-phase")).textContent).toBe("正在下載新版…");
    expect(screen.getByTestId("update-progress").textContent).toContain("正在更新到 0.3.0");
    expect(screen.queryByRole("button", { name: "現在更新" })).toBeNull();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_100);
    });
    await waitFor(() => expect(screen.getByTestId("update-phase").textContent).toBe("正在編譯…"));
    expect(state.status.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  // Guards: REQ-281
  test("TestUpdateCardDisconnected：升級中抓不到（看板在重開）不顯示錯誤，寫正在重開、會自己回來，回來後顯示結果", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const running = {
      ...newer,
      state: { phase: "restart", from: "0.2.0", to: "0.3.0", ok: false },
    };
    const done = {
      ...base,
      current: "0.3.0",
      state: { phase: "done", from: "0.2.0", to: "0.3.0", ok: true },
    };
    state.status
      .mockResolvedValueOnce(running)
      .mockRejectedValueOnce(new Error("Failed to fetch"))
      .mockResolvedValue(done);
    mount();
    await screen.findByTestId("update-progress");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_100);
    });
    expect((await screen.findByTestId("update-restarting")).textContent).toBe(
      "正在重開，等一下會自己回來。",
    );
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByTestId("update-progress")).not.toBeNull();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_100);
    });
    const result = await screen.findByTestId("update-result");
    expect(result.getAttribute("data-result")).toBe("done");
    expect(result.textContent).toBe("已經更新到 0.3.0。");
    expect(screen.queryByTestId("update-progress")).toBeNull();
  });

  // Guards: REQ-281
  test("TestUpdateCardRolledBack：沒成功已換回時寫出結果、原因與下一步，之後還能再按更新", async () => {
    state.status.mockResolvedValue({
      ...newer,
      state: {
        phase: "rolled_back",
        outcome: "rolled_back",
        from: "0.2.0",
        to: "0.3.0",
        ok: false,
        reason: "新版開不起來",
        next: "這一版先跳過",
      },
    });
    mount();
    const result = await screen.findByTestId("update-result");
    expect(result.getAttribute("data-result")).toBe("rolled_back");
    expect(screen.getByTestId("update-headline").textContent).toBe(
      "更新到 0.3.0 沒成功，已換回 0.2.0，你的資料沒動。",
    );
    expect(screen.getByTestId("update-reason").textContent).toBe("原因：新版開不起來");
    expect(screen.getByTestId("update-next").textContent).toBe("下一步：這一版先跳過");
    expect(screen.queryByTestId("update-progress")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // 三種結果各自的說法（UPD-14）：舊版照常、已換回（資料也回去）、也沒換回；比這個功能舊的 failed 不說有沒有換回。
  // Guards: REQ-281
  test("TestUpdateCardOutcomes：照 outcome 講舊版照常、已換回、也沒換回", async () => {
    const cases = [
      [
        { phase: "failed", outcome: "untouched" },
        "untouched",
        "更新到 0.3.0 沒成功，舊版 0.2.0 照常在跑。",
      ],
      [
        { phase: "rolled_back", outcome: "rolled_back", restored: true },
        "rolled_back",
        "更新到 0.3.0 沒成功，已換回 0.2.0，資料也回到更新前。",
      ],
      [
        { phase: "rolled_back", outcome: "untouched" },
        "untouched",
        "更新到 0.3.0 沒成功，舊版 0.2.0 照常在跑。",
      ],
      [
        { phase: "failed", outcome: "stranded" },
        "stranded",
        "更新到 0.3.0 沒成功，也沒能換回 0.2.0，要處理。",
      ],
      [{ phase: "failed" }, "failed", "更新到 0.3.0 沒成功。"],
      [
        { phase: "done", kind: "rollback", from: "0.3.0", to: "0.2.0", ok: true },
        "done",
        "已退回到 0.2.0。",
      ],
    ] as const;
    for (const [s, outcome, text] of cases) {
      state.status.mockResolvedValue({
        ...base,
        state: { from: "0.2.0", to: "0.3.0", ok: false, reason: "磁碟滿了", ...s },
      });
      const view = mount();
      const result = await screen.findByTestId("update-result");
      expect(result.getAttribute("data-result")).toBe(outcome);
      expect(screen.getByTestId("update-headline").textContent).toBe(text);
      view.unmount();
    }
  });

  // Guards: REQ-281
  test("TestUpdateCardApplyRefused：只有主人能按，被拒絕的白話原因寫在卡片裡", async () => {
    const { ApiError } = await import("@evergenie/sdk-web");
    state.apply.mockRejectedValue(new ApiError("forbidden", "只有你能更新 EverGenie"));
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "現在更新" }));
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "更新" }),
    );
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("沒有送出：只有你能更新 EverGenie");
  });
});
