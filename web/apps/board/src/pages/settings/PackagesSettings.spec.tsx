import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { PackagesSettings } from "./PackagesSettings";

vi.mock("@evergenie/sdk-web", async (orig) => {
  const real = (await orig()) as Record<string, unknown>;
  const many = Array.from({ length: 12 }, (_, i) => ({
    id: `p${i}`,
    name: `套件 ${i}`,
    tier: "official",
    enabled: true,
  }));
  const pkg = new Proxy(
    { list: async () => ({ packages: many, warnings: [] }) } as Record<string, unknown>,
    { get: (t, k) => (k in t ? t[k as string] : async () => ({})) },
  );
  return { ...real, api: { package: pkg } };
});

const realScroll = Element.prototype.scrollIntoView;
afterEach(() => {
  cleanup();
  Element.prototype.scrollIntoView = realScroll;
});

// 套件很多時，「從資料夾／網址安裝」在清單最下面：頁首要有入口，按了捲到安裝那段（PKG-54）。
test("TestInstallEntryAtTopJumpsToInstallForms", async () => {
  const scroll = vi.fn();
  Element.prototype.scrollIntoView = scroll;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <PackagesSettings />
    </QueryClientProvider>,
  );
  const jump = await screen.findByTestId("packages-install-jump");
  await screen.findByText("套件 11");
  const install = screen.getByTestId("packages-install");
  // 入口在清單前面（手機一進來就看得到）
  const firstRow = screen.getByText("套件 0");
  expect(jump.compareDocumentPosition(firstRow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  fireEvent.click(jump);
  expect(scroll).toHaveBeenCalledTimes(1);
  expect(scroll.mock.contexts[0]).toBe(install);
});
