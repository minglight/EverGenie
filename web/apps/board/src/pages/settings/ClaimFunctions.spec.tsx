import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { ClaimFunctions } from "./ClaimFunctions";

vi.mock("@evergenie/sdk-web", () => ({
  api: {
    web: { getAgents: async () => ({ agents: [{ id: "code" }], accounts: ["main"] }) },
    personas: {
      list: async () => ({
        personas: [
          { slug: "athena", name: "雅典娜", status: "live", seats: ["code"], shadowed: false },
          {
            slug: "garden.fairy",
            name: "花仙子",
            source: "package",
            package: "garden",
            status: "live",
            seats: [],
            shadowed: false,
          },
        ],
      }),
    },
    package: {
      list: async () => ({
        packages: [
          {
            id: "garden",
            enabled: true,
            state: "running",
            agents: [{ id: "garden", duty: "照顧花園" }],
          },
          { id: "park", enabled: true, state: "running", agents: [{ id: "park", duty: "公園" }] },
          {
            id: "atelier",
            enabled: true,
            state: "running",
            agents: [{ id: "painter", duty: "畫畫：做新衣服", optional: true }],
          },
        ],
      }),
      sync: vi.fn(),
    },
    agents: { setField: vi.fn() },
  },
  ApiError: class ApiError extends Error {},
}));

afterEach(cleanup);

function renderIt(focus?: string, withOptional?: boolean) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ClaimFunctions focus={focus} withOptional={withOptional} />
    </QueryClientProvider>,
  );
}

describe("ClaimFunctions", () => {
  // Guards: REQ-342, REQ-346
  test("ClaimFunctions：清單含職位內角色 garden.fairy、預設選它；?claim= 的那張卡被標出來", async () => {
    renderIt("garden");
    const cards = await screen.findAllByTestId("function-unclaimed");
    expect(cards.map((c) => c.dataset.agent)).toEqual(["garden", "park"]);
    const garden = cards[0] as HTMLElement;
    expect(garden.dataset.focused).toBe("true");
    expect(cards[1]?.dataset.focused).toBeUndefined();
    const select = garden.querySelector("select") as HTMLSelectElement;
    expect([...select.options].map((o) => o.value)).toContain("garden.fairy");
    expect(select.value).toBe("garden.fairy");
  });

  // Guards: WEB-47
  test("選用職位：設定精靈（沒給 withOptional）不列；「職位與秘書」頁另一組列出、標「選用」，?claim= 照樣標出", async () => {
    renderIt();
    await screen.findAllByTestId("function-unclaimed");
    expect(screen.getAllByTestId("function-unclaimed").map((c) => c.dataset.agent)).toEqual([
      "garden",
      "park",
    ]);
    expect(screen.queryByTestId("functions-optional")).toBeNull();
    cleanup();

    renderIt("painter", true);
    const group = await screen.findByTestId("functions-optional");
    expect(group.textContent).toContain("選用的職位");
    const cards = screen.getAllByTestId("function-unclaimed");
    expect(cards.map((c) => c.dataset.agent)).toEqual(["garden", "park", "painter"]);
    const painter = cards[2] as HTMLElement;
    expect(group.contains(painter)).toBe(true);
    expect(painter.dataset.optional).toBe("true");
    expect(painter.dataset.focused).toBe("true");
    expect(painter.querySelector('[data-testid="function-optional-badge"]')?.textContent).toBe(
      "選用",
    );
    expect(cards[0]?.querySelector('[data-testid="function-optional-badge"]')).toBeNull();
  });
});
