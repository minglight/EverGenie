import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { DevStrip, HomeLabel, homeTitle, useDevThemeColor } from "./HomeMark";

const state = vi.hoisted(() => ({
  home: { env: "prod", label: "amy-home", machine: "home" },
  sites: 2,
}));

vi.mock("../data/queries", () => ({
  homeQuery: () => ({ queryKey: ["home"], queryFn: async () => state.home }),
}));
vi.mock("../data/sites", () => ({
  useSites: () => Array.from({ length: state.sites }, (_, i) => ({ key: String(i) })),
}));

function ThemeProbe({ dev }: { dev: boolean }) {
  useDevThemeColor(dev);
  return null;
}

function mount(node: React.ReactNode) {
  return render(<QueryClientProvider client={new QueryClient()}>{node}</QueryClientProvider>);
}

beforeEach(() => {
  state.sites = 2;
  document.head.innerHTML =
    '<meta name="theme-color" content="#f5f3f9" /><meta name="theme-color" content="#14121c" />';
});
afterEach(() => cleanup());

describe("HomeMark", () => {
  // DEV-156 U-07：只有一個家時家名是雜訊；開發家的橘色標示是主人要的，一個家也照畫。
  test("只有一個家：正式家不畫標籤，開發家照畫", async () => {
    state.sites = 1;
    state.home = { env: "prod", label: "amy-home", machine: "home" };
    const { container } = mount(<HomeLabel />);
    await new Promise((r) => setTimeout(r, 20));
    expect(container.textContent).toBe("");
    cleanup();
    state.home = { env: "dev", label: "amy-home@dev", machine: "home" };
    mount(<HomeLabel />);
    expect((await screen.findByTestId("home-label")).className).toContain("bg-attention-soft");
  });

  test("兩個家以上的正式家：標籤是 owner-machine、中性色，沒有橘色橫條", async () => {
    state.home = { env: "prod", label: "amy-home", machine: "home" };
    mount(
      <>
        <DevStrip />
        <HomeLabel />
      </>,
    );
    const label = await screen.findByTestId("home-label");
    expect(label.textContent).toBe("amy-home");
    expect(label.getAttribute("data-env")).toBe("prod");
    expect(label.className).not.toContain("attention");
    expect(screen.queryByTestId("dev-strip")).toBeNull();
  });

  test("開發家：標籤帶 @dev、橘色，最上面有橘色橫條", async () => {
    state.home = { env: "dev", label: "amy-home@dev", machine: "home" };
    mount(
      <>
        <DevStrip />
        <HomeLabel />
      </>,
    );
    const label = await screen.findByTestId("home-label");
    expect(label.textContent).toBe("amy-home@dev");
    expect(label.className).toContain("bg-attention-soft");
    expect((await screen.findByTestId("dev-strip")).className).toContain("bg-attention");
  });

  test("讀不到家的身分：不畫標籤", () => {
    state.home = { env: "prod", label: "", machine: "" };
    const { container } = mount(<HomeLabel />);
    expect(container.textContent).toBe("");
  });

  test("分頁標題：開發家前面加「開發｜」，正式家不變", () => {
    expect(homeTitle(true, "今天 · 看板")).toBe("開發｜今天 · 看板");
    expect(homeTitle(false, "今天 · 看板")).toBe("今天 · 看板");
  });

  test("開發家把兩個 theme-color 換成橘色，正式家不動", async () => {
    const { unmount } = mount(<ThemeProbe dev={false} />);
    const metas = () =>
      [...document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')].map(
        (m) => m.content,
      );
    expect(metas()).toEqual(["#f5f3f9", "#14121c"]);
    unmount();
    mount(<ThemeProbe dev />);
    await waitFor(() => expect(metas()).toEqual(["#f59e0b", "#f59e0b"]));
  });
});
