import { api, type Notification } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: unknown }) => children,
  useNavigate: () => () => {},
}));

import { useSettled } from "../data/answers";
import { AnswerTyped } from "./AnswerPanel";

const n = { id: "M-9", title: "要不要改週四", typed: true } as Notification;

// 跟任務單詳情頁、今天頁的卡一樣：收成「已回」時整個回答框拆掉，撤回後重新長出來。
function Host() {
  const settled = useSettled().get("SEC-9");
  if (settled) return <p>{settled.label}</p>;
  return <AnswerTyped notification={n} settleAs={{ key: "SEC-9", id: "SEC-9" }} />;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

// 打字回答送出時先收成「已回」、回答框拆掉；沒送出去再長回來時，打的字還在，不用重打（DEV-156 review）。
test("TestTypedAnswerDraftSurvivesFailedSend", async () => {
  let fail: (err: Error) => void = () => {};
  vi.spyOn(api.notify, "answer").mockReturnValue(
    new Promise((_, reject) => {
      fail = reject;
    }),
  );
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <Host />
    </QueryClientProvider>,
  );
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "改週四" } });
  fireEvent.click(screen.getByRole("button", { name: "送出" }));
  await waitFor(() => expect(screen.queryByRole("textbox")).toBeNull());
  fail(new TypeError("offline"));
  const box = await screen.findByRole("textbox");
  expect((box as HTMLInputElement).value).toBe("改週四");
});
