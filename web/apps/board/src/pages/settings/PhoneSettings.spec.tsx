import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { openedViaTailnet } from "../../data/phone";
import { FunnelBanner } from "../../shell/FunnelBanner";
import { PhoneSettings } from "./PhoneSettings";

const URL_ = "https://mac.example.ts.net";
const base = {
  local: true,
  tailscale: "ready",
  account: "owner@example.com",
  mounted: false,
  push_ready: false,
  funnel: false,
  download_url: "https://tailscale.com/download/mac",
};

const state = vi.hoisted(() => ({
  phone: {} as Record<string, unknown>,
  setup: vi.fn(),
  close: vi.fn(),
}));

vi.mock("@evergenie/sdk-web", () => ({
  api: {
    web: {
      getPhone: async () => ({ phone: state.phone }),
      phoneSetup: state.setup,
      phoneCloseFunnel: state.close,
    },
  },
  ApiError: class ApiError extends Error {},
}));
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, ...rest }: { children: ReactNode; to: string }) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
}));

function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

const stepState = (n: number) => screen.getByTestId(`phone-step-${n}`).getAttribute("data-state");

beforeEach(() => {
  state.phone = { ...base };
  state.setup.mockReset();
  state.close.mockReset();
});
afterEach(() => cleanup());

describe("設定 → 手機連線（DEV-153）", () => {
  test("沒裝 Tailscale：第 1 步還沒、給官網下載連結（不是 brew），第 2 步不能按", async () => {
    state.phone = { ...base, tailscale: "missing", account: "" };
    show(<PhoneSettings />);
    await screen.findByTestId("phone-step-1");
    expect(stepState(1)).toBe("todo");
    const link = screen.getByRole("link", { name: "到 Tailscale 官網下載" });
    expect(link.getAttribute("href")).toBe("https://tailscale.com/download/mac");
    expect(document.body.textContent).not.toContain("brew");
    expect(screen.queryByTestId("phone-setup")).toBeNull();
    expect(screen.queryByTestId("phone-qr")).toBeNull();
  });

  test("這台 Mac 本機：按「讓手機連得到」後第 2 步打勾，出現網址、QR code 與下一步的連結", async () => {
    state.setup.mockImplementation(async () => {
      state.phone = { ...base, mounted: true, url: URL_, push_ready: true };
      return { ok: true, phone: state.phone };
    });
    show(<PhoneSettings />);
    await screen.findByTestId("phone-setup");
    expect(stepState(1)).toBe("done");
    expect(stepState(2)).toBe("todo");
    fireEvent.click(screen.getByTestId("phone-setup"));
    await waitFor(() => expect(stepState(2)).toBe("done"));
    expect(screen.getByTestId("phone-url").textContent).toBe(URL_);
    expect(screen.getByTestId("phone-qr").querySelector("path")?.getAttribute("d")).toMatch(/^M/);
    expect(screen.getByTestId("phone-next").getAttribute("href")).toBe("/settings/devices");
    expect(screen.queryByTestId("phone-setup")).toBeNull();
    // 手機那兩步偵測不到：寫「照著做」，不假裝打勾。
    expect(stepState(3)).toBe("manual");
    expect(screen.getByTestId("phone-tailnet-note").textContent).toBe(
      "跟你在同一個 Tailscale 網路裡的人，都能打開你的看板；不要把別人加進你的 Tailscale 網路。",
    );
  });

  test("後台還沒開 HTTPS：白話說去哪按、給連結、按完再按一次", async () => {
    state.setup.mockResolvedValue({
      ok: false,
      reason: "https",
      message: "Tailscale 後台還沒開 HTTPS。",
      help_url: "https://login.tailscale.com/f/serve?node=abc",
      phone: { ...base },
    });
    show(<PhoneSettings />);
    fireEvent.click(await screen.findByTestId("phone-setup"));
    const box = await screen.findByTestId("phone-setup-failed");
    expect(box.textContent).toContain("Tailscale 後台還沒開 HTTPS。");
    expect(box.textContent).toContain("回來這裡再按一次");
    expect(box.querySelector("a")?.getAttribute("href")).toBe(
      "https://login.tailscale.com/f/serve?node=abc",
    );
    expect(screen.getByTestId("phone-setup").textContent).toBe("再做一次");
  });

  test("不是這台 Mac 本機（手機、別台）：只有狀態與說明，沒有按鈕", async () => {
    state.phone = { ...base, local: false };
    show(<PhoneSettings />);
    await screen.findByTestId("phone-not-here");
    expect(screen.queryByTestId("phone-setup")).toBeNull();
    expect(stepState(1)).toBe("done");
  });

  test("Funnel 開著：紅色警告與「關掉對外公開」，按了警告消失；手機、別台開的看板也能關", async () => {
    state.phone = {
      ...base,
      mounted: true,
      url: URL_,
      push_ready: true,
      funnel: true,
      funnel_url: URL_,
    };
    state.close.mockImplementation(async () => {
      state.phone = { ...state.phone, funnel: false, funnel_url: "" };
      return { phone: state.phone };
    });
    show(
      <>
        <FunnelBanner />
        <PhoneSettings />
      </>,
    );
    const warn = await screen.findByTestId("phone-funnel");
    expect(warn.textContent).toContain("你的看板現在整個網路都連得到");
    expect(screen.getByTestId("funnel-banner").textContent).toContain(
      "你的看板現在整個網路都連得到",
    );
    fireEvent.click(screen.getByTestId("phone-close-funnel"));
    await waitFor(() => expect(screen.queryByTestId("phone-funnel")).toBeNull());
    expect(screen.queryByTestId("funnel-banner")).toBeNull();
    expect(state.close).toHaveBeenCalledTimes(1);

    cleanup();
    state.phone = {
      ...base,
      local: false,
      mounted: true,
      url: URL_,
      funnel: true,
      funnel_url: URL_,
    };
    show(
      <>
        <FunnelBanner />
        <PhoneSettings />
      </>,
    );
    // 不是這台 Mac 本機開的看板（手機、別台）也能關：關掉是變安全的動作（SET-12）。
    await screen.findByTestId("phone-funnel");
    expect(screen.getByTestId("funnel-banner-close")).toBeTruthy();
    fireEvent.click(screen.getByTestId("phone-close-funnel"));
    await waitFor(() => expect(screen.queryByTestId("phone-funnel")).toBeNull());
    expect(screen.queryByTestId("funnel-banner")).toBeNull();
  });

  test("從看板的 Tailscale 網址打開（手機）：手機那兩步算做好了", () => {
    expect(openedViaTailnet({ local: false, url: URL_ }, URL_)).toBe(true);
    expect(openedViaTailnet({ local: true, url: URL_ }, "http://127.0.0.1:8850")).toBe(false);
    expect(openedViaTailnet({ local: false, url: "" }, "")).toBe(false);
  });
});
