import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { BootPromptBanner } from "./BootPromptBanner";

const state = vi.hoisted(() => ({
  prompts: [] as unknown[],
  answer: vi.fn(async () => ({})),
}));

vi.mock("../data/sites", () => ({
  PRIMARY: {},
  apiFor: () => ({
    web: {
      getBootPrompts: async () => ({ prompts: state.prompts }),
      answerBootPrompt: state.answer,
    },
  }),
}));

function mount() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <BootPromptBanner />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  state.answer.mockClear();
});

describe("BootPromptBanner", () => {
  test("沒有卡住的秘書：什麼都不畫", async () => {
    state.prompts = [];
    mount();
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByTestId("boot-prompts")).toBeNull();
  });

  test("卡在信任提示：顯示人名與選項，按了送出該 session 與選項", async () => {
    state.prompts = [
      {
        id: "admin#trust",
        session: "admin",
        agent_name: "輝夜",
        title: "要信任它嗎？",
        choices: ["信任這個資料夾", "不信任（離開）"],
      },
    ];
    mount();
    expect((await screen.findByRole("alert")).textContent).toContain("輝夜");
    fireEvent.click(screen.getByRole("button", { name: "信任這個資料夾" }));
    await waitFor(() =>
      expect(state.answer).toHaveBeenCalledWith({
        session: "admin",
        id: "admin#trust",
        choice: "信任這個資料夾",
      }),
    );
  });
});
