import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { AgentKnowledge } from "./AgentKnowledge";

// 介面 1 的假伺服器：記住勾了哪幾個，knowledge／set_knowledge 都回同一份。
const server = vi.hoisted(() => ({
  knowledge: vi.fn(),
  setKnowledge: vi.fn(),
  enabled: [] as string[],
  blocks: [] as { id: string; title: string; summary: string; default: boolean }[],
}));

vi.mock("@evergenie/sdk-web", () => ({
  api: { agents: { knowledge: server.knowledge, setKnowledge: server.setKnowledge } },
  ApiError: class ApiError extends Error {},
}));

const lifeBlocks = [
  { id: "parenting", title: "育兒", summary: "依孩子的年齡陪你想", default: true },
  { id: "balance", title: "工作與生活的平衡", summary: "工作吃掉生活時提醒一句", default: false },
];

beforeEach(() => {
  server.blocks = lifeBlocks;
  server.enabled = ["parenting"];
  const read = () => ({
    agent: "life",
    blocks: server.blocks.map((b) => ({ ...b, enabled: server.enabled.includes(b.id) })),
  });
  server.knowledge.mockReset().mockImplementation(async () => read());
  server.setKnowledge.mockReset().mockImplementation(async (req: { enabled?: string[] }) => {
    server.enabled = req.enabled ?? [];
    return read();
  });
});
afterEach(cleanup);

function renderIt(agent = "life") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AgentKnowledge agent={agent} />
    </QueryClientProvider>,
  );
}

const box = (id: string) =>
  document.querySelector(`[data-testid="knowledge-block"][data-block="${id}"]`) as HTMLInputElement;

// Guards: DEV-161
describe("職位與秘書：她帶的知識", () => {
  test("列出可選知識（標題、一句說明、勾選框），勾了的是勾著的；說明文字在底下", async () => {
    renderIt();
    expect(await screen.findByText("她帶的知識")).toBeTruthy();
    expect(screen.getByText("育兒")).toBeTruthy();
    expect(screen.getByText("依孩子的年齡陪你想")).toBeTruthy();
    expect(screen.getByText("工作與生活的平衡")).toBeTruthy();
    expect(box("parenting").checked).toBe(true);
    expect(box("balance").checked).toBe(false);
    expect(
      screen.getByText("基本的一定帶；可選的勾了她才會用。第一天上班她也會跟你聊一下再幫你調。"),
    ).toBeTruthy();
    expect(server.knowledge).toHaveBeenCalledWith({ agent: "life" });
  });

  test("勾一個就存（照區塊原本的順序），再取消一個也存；存好重讀後畫面照伺服器的樣子", async () => {
    renderIt();
    await screen.findByText("育兒");
    fireEvent.click(box("balance"));
    await waitFor(() =>
      expect(server.setKnowledge).toHaveBeenCalledWith({
        agent: "life",
        enabled: ["parenting", "balance"],
      }),
    );
    await waitFor(() => expect(box("balance").checked).toBe(true));
    // 「已存」寫在清單底下（不是底部的提示）。
    expect((await screen.findByTestId("knowledge-saved")).textContent).toContain(
      "已存。下次開對話才生效。",
    );
    fireEvent.click(box("parenting"));
    await waitFor(() =>
      expect(server.setKnowledge).toHaveBeenCalledWith({
        agent: "life",
        enabled: ["balance"],
      }),
    );
    await waitFor(() => expect(box("parenting").checked).toBe(false));
    expect(box("balance").checked).toBe(true);
  });

  test("沒有可選知識：這塊整個不出現", async () => {
    server.blocks = [];
    server.enabled = [];
    renderIt("system");
    await waitFor(() => expect(server.knowledge).toHaveBeenCalled());
    expect(screen.queryByTestId("knowledge")).toBeNull();
    expect(screen.queryByText("她帶的知識")).toBeNull();
  });

  test("讀不到：不出現、也不擋卡片上別的設定", async () => {
    server.knowledge.mockRejectedValue(new Error("Failed to fetch"));
    renderIt();
    await waitFor(() => expect(server.knowledge).toHaveBeenCalled());
    expect(screen.queryByTestId("knowledge")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  test("存不成：白話原因，畫面回到伺服器真正的樣子", async () => {
    renderIt();
    await screen.findByText("育兒");
    server.setKnowledge.mockRejectedValue(new Error("Failed to fetch"));
    fireEvent.click(box("balance"));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("沒存成功");
    expect(alert.textContent).not.toContain("Failed to fetch");
    await waitFor(() => expect(box("balance").checked).toBe(false));
  });
});
