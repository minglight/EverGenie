import { type Api, ApiError, type Session } from "@evergenie/sdk-web";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { restartSession } from "../../lib/restartSession";
import { RestartBar } from "./RestartBar";

afterEach(() => cleanup());

const session: Session = { name: "work", agent: "work", kind: "resident" };

function fakeApi(close: () => Promise<unknown>, open: () => Promise<unknown>) {
  const closeFn = vi.fn(close);
  const openFn = vi.fn(open);
  const api = { session: { close: closeFn, open: openFn } } as unknown as Api;
  return { api, closeFn, openFn };
}

function mount(api: Api, extra: { onDone?: () => void; onBusy?: (b: boolean) => void } = {}) {
  const onDone = extra.onDone ?? vi.fn();
  render(
    <RestartBar
      api={api}
      session={session}
      agentName="阿媞米斯"
      onClose={() => {}}
      onDone={onDone}
      onBusy={extra.onBusy}
    />,
  );
  return { onDone };
}

// 重開對話（DEV-106）：先關再開，名稱、職位、種類照原來的 session（不走 rotate）。
describe("重開對話（DEV-106）：restartSession", () => {
  test("先關再開，用原來的名稱、職位與種類", async () => {
    const { api, closeFn, openFn } = fakeApi(
      async () => ({}),
      async () => ({ session: { ...session, state: "starting" } }),
    );
    await restartSession(api, session);
    expect(closeFn).toHaveBeenCalledWith({ name: "work" });
    expect(openFn).toHaveBeenCalledWith({ name: "work", agent: "work", kind: "resident" });
    expect(closeFn.mock.invocationCallOrder[0] ?? 0).toBeLessThan(
      openFn.mock.invocationCallOrder[0] ?? 0,
    );
  });

  test("本來就沒在跑（關的時候 not_found）：直接開", async () => {
    const { api, openFn } = fakeApi(
      async () => {
        throw new ApiError("not_found", "沒有這個 session");
      },
      async () => ({ session }),
    );
    await restartSession(api, session);
    expect(openFn).toHaveBeenCalledTimes(1);
  });

  test("關的時候出別的錯：照實丟出、不往下開", async () => {
    const { api, openFn } = fakeApi(
      async () => {
        throw new ApiError("internal", "引擎沒回應");
      },
      async () => ({ session }),
    );
    await expect(restartSession(api, session)).rejects.toThrow("引擎沒回應");
    expect(openFn).not.toHaveBeenCalled();
  });
});

describe("重開對話（DEV-106）：說明列", () => {
  test("按「重開」才做：先關再開、參數對，做完寫「已重開」、通知重抓、焦點移到「知道了」", async () => {
    const { api, closeFn, openFn } = fakeApi(
      async () => ({}),
      async () => ({ session: { ...session, state: "starting" } }),
    );
    const { onDone } = mount(api);
    expect(screen.getByText("重開 阿媞米斯 的對話？")).toBeTruthy();
    expect(closeFn).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "重開" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("已重開"));
    expect(closeFn).toHaveBeenCalledWith({ name: "work" });
    expect(openFn).toHaveBeenCalledWith({ name: "work", agent: "work", kind: "resident" });
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "知道了" }));
  });

  test("說明裡講清楚：會中斷、換了帳號她從頭開始", () => {
    const { api } = fakeApi(
      async () => ({}),
      async () => ({ session }),
    );
    mount(api);
    const text = document.body.textContent ?? "";
    expect(text).toContain("中斷");
    expect(text).toContain("從頭開始");
  });

  test("排隊中：結果寫排隊，不說她已經開起來", async () => {
    const { api } = fakeApi(
      async () => ({}),
      async () => ({ session: { ...session, queued_at: "2026-11-03T12:00:00Z" } }),
    );
    mount(api);
    fireEvent.click(screen.getByRole("button", { name: "重開" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("排隊中"));
  });

  test("伺服器給的是零值時間（沒在排隊）：寫「已重開」，不說排隊", async () => {
    const { api } = fakeApi(
      async () => ({}),
      async () => ({
        session: { ...session, state: "idle", queued_at: "0001-01-01T00:00:00Z" },
      }),
    );
    mount(api);
    fireEvent.click(screen.getByRole("button", { name: "重開" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("已重開"));
    expect(screen.getByRole("status").textContent).not.toContain("排隊");
  });

  test("額度還沒恢復：結果寫要等額度回來", async () => {
    const { api } = fakeApi(
      async () => ({}),
      async () => ({ session: { ...session, state: "limited" } }),
    );
    mount(api);
    fireEvent.click(screen.getByRole("button", { name: "重開" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("額度還沒恢復"));
  });

  test("開不起來：顯示原因與「可以再按一次」，通知重抓，可以再按", async () => {
    const { api, closeFn } = fakeApi(
      async () => ({}),
      async () => {
        throw new ApiError("unavailable", "這台暫時連不上");
      },
    );
    const onBusy = vi.fn();
    const { onDone } = mount(api, { onBusy });
    fireEvent.click(screen.getByRole("button", { name: "重開" }));
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toContain("沒重開成功：這台暫時連不上"),
    );
    expect(screen.getByRole("alert").textContent).toContain("再按一次");
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(onBusy.mock.calls.map((c) => c[0])).toEqual([true, false]);
    expect((screen.getByRole("button", { name: "重開" }) as HTMLButtonElement).disabled).toBe(
      false,
    );
    fireEvent.click(screen.getByRole("button", { name: "重開" }));
    await waitFor(() => expect(closeFn).toHaveBeenCalledTimes(2));
  });

  test("關的時候出錯：不往下開，錯誤顯示在列上", async () => {
    const { api, openFn } = fakeApi(
      async () => {
        throw new ApiError("internal", "引擎沒回應");
      },
      async () => ({ session }),
    );
    mount(api);
    fireEvent.click(screen.getByRole("button", { name: "重開" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("引擎沒回應"));
    expect(openFn).not.toHaveBeenCalled();
  });

  test("伺服器沒給錯誤文字、或丟的是瀏覽器自己的錯：不空白也不露英文（UXC-8）", async () => {
    for (const err of [new ApiError("internal", ""), new TypeError("Failed to fetch")]) {
      const { api } = fakeApi(
        async () => ({}),
        async () => {
          throw err;
        },
      );
      mount(api);
      fireEvent.click(screen.getByRole("button", { name: "重開" }));
      await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("沒重開成功："));
      expect(screen.getByRole("alert").textContent).not.toContain("TypeError");
      expect(screen.getByRole("alert").textContent).not.toContain("Failed to fetch");
      cleanup();
    }
  });
});
