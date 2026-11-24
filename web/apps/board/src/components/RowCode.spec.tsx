import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { modeQuery } from "../data/queries";
import { RowCode } from "./RowCode";

afterEach(cleanup);

function mount(mode: "simple" | "full") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(modeQuery().queryKey, mode);
  render(
    <QueryClientProvider client={client}>
      <p data-testid="row">
        <RowCode id="SEC-12" />
        標題
      </p>
    </QueryClientProvider>,
  );
}

// 清單列的編號（DEV-156 U-22，UXC-8）：簡單模式（預設）不露，完整模式才露，跟標題中間隔「 · 」。
test("TestRowCodeHiddenInSimpleMode", () => {
  mount("simple");
  expect(screen.getByTestId("row").textContent).toBe("標題");
  cleanup();
  mount("full");
  expect(screen.getByTestId("row").textContent).toBe("SEC-12 · 標題");
});
