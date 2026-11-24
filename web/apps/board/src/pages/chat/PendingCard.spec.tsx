import type { Pending } from "@evergenie/sdk-web";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { t } from "../../i18n/zh-TW";
import { PendingCard } from "./PendingCard";

const state = vi.hoisted(() => ({
  resolve: vi.fn(async (_req: unknown) => ({ applied: true })),
}));
vi.mock("@evergenie/sdk-web", () => ({
  api: { session: { resolve: state.resolve } },
  ApiError: class ApiError extends Error {},
}));

afterEach(() => {
  cleanup();
  state.resolve.mockClear();
});

const card = (pending: Pending) =>
  render(
    <PendingCard
      session="code"
      pending={pending}
      agentName="雅典娜"
      onHold={() => {}}
      onAnswered={() => {}}
    />,
  );

describe("逾時後在她畫面上等的確認（SES-48、WEB-74）", () => {
  // Guards: REQ-376, REQ-377
  test("權限確認：顯示「在她畫面上也開著」，拒絕直接送出、不問理由", async () => {
    card({ id: "p1", kind: "permission", summary: "git push", native: true } as Pending);
    expect(screen.getByText(t("pending.native", { name: "雅典娜" }))).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: t("pending.deny") }));
    await waitFor(() => expect(state.resolve).toHaveBeenCalledTimes(1));
    expect(state.resolve.mock.calls[0]?.[0]).toMatchObject({
      session: "code",
      pending_id: "p1",
      decision: { allow: false },
    });
    expect(screen.queryByLabelText(t("pending.reason"))).toBeNull();
  });

  // 交回她畫面的選擇題，一題單選：照樣能按（引擎在她畫面上代按，WEB-134；作者 2026-11-03「這個卡住我真的快瘋了」）。
  test("選擇題（交回、一題單選）：選項照樣能按，按了就送出", () => {
    card({
      id: "p2",
      kind: "question",
      native: true,
      questions: [
        { text: "要哪個？", options: [{ label: "A", description: "A 的說明" }, { label: "B" }] },
      ],
    } as Pending);
    expect(screen.getByText("A 的說明")).toBeTruthy();
    expect(screen.queryByRole("note")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^A/ }));
    expect(state.resolve).toHaveBeenCalledWith({
      session: "code",
      pending_id: "p2",
      decision: { allow: true, answers: { "要哪個？": "A" } },
    });
  });

  // Guards: REQ-376
  test("選擇題（交回、複選）：照樣顯示題目與每個選項，提示到她的對話裡回答，選項不能按", () => {
    card({
      id: "p2",
      kind: "question",
      native: true,
      questions: [
        {
          text: "要哪個？",
          multi_select: true,
          options: [{ label: "A", description: "A 的說明" }, { label: "B" }],
        },
      ],
    } as Pending);
    expect(screen.getByText("要哪個？")).toBeTruthy();
    expect(screen.getByText("A")).toBeTruthy();
    expect(screen.getByText("A 的說明")).toBeTruthy();
    expect(screen.getByText("B")).toBeTruthy();
    expect(screen.getByRole("note").textContent).toBe(
      t("pending.nativeQuestion", { name: "雅典娜" }),
    );
    expect(t("pending.nativeQuestion")).not.toContain("終端機");
    expect(screen.queryByRole("button", { name: "A" })).toBeNull();
  });

  // Guards: REQ-376
  test("選擇題（交回）：小標、多題、同名選項、沒有選項、空白說明都不炸", () => {
    card({
      id: "p3",
      kind: "question",
      native: true,
      questions: [
        {
          header: "範圍",
          text: "先做哪個？",
          options: [
            { label: "同名", description: "   " },
            { label: "同名", description: "第二個" },
          ],
        },
        { text: "沒選項的題" },
        { text: "空選項的題", options: [] },
      ],
    } as Pending);
    expect(screen.getByText("範圍")).toBeTruthy();
    expect(screen.getAllByText("同名")).toHaveLength(2);
    expect(screen.getByText("第二個")).toBeTruthy();
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByText("沒選項的題")).toBeTruthy();
    expect(screen.getByText("空選項的題")).toBeTruthy();
    expect(screen.queryAllByRole("button")).toHaveLength(0);
  });

  // Guards: REQ-376
  test("還在等核心的確認不顯示提示，拒絕照舊先問理由", () => {
    card({ id: "p3", kind: "permission", summary: "git push" } as Pending);
    expect(screen.queryByText(t("pending.native", { name: "雅典娜" }))).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: t("pending.deny") }));
    expect(screen.getByLabelText(t("pending.reason"))).toBeTruthy();
    expect(state.resolve).not.toHaveBeenCalled();
  });
});

// 作者 2026-11-02「彈出確認視窗缺了很多資訊：一開始總說明還有每一項的子說明」。
// 主人打字蓋過選擇題（SES-70、WEB-166）
describe("選擇題提示可以打字跳過", () => {
  test("選擇題卡片底下有提示，權限確認沒有", () => {
    card({
      id: "q9",
      kind: "question",
      questions: [{ text: "選哪個？", options: [{ label: "A" }] }],
    } as Pending);
    expect(screen.getByTestId("type-to-skip").textContent).toBe(t("pending.typeToSkip"));
    cleanup();
    card({ id: "p9", kind: "permission", summary: "git push" } as Pending);
    expect(screen.queryByTestId("type-to-skip")).toBeNull();
  });
});

describe("選擇題卡片帶著說明", () => {
  const question = {
    id: "q1",
    kind: "question",
    summary: "希臘神話裡有三位已經在用，我推薦瑟西。",
    questions: [
      {
        header: "選角色",
        text: "你想做哪一位？",
        multi_select: false,
        allow_free_text: true,
        options: [
          { label: "瑟西", description: "優雅腹黑的魔女" },
          { label: "卡珊德拉", description: "清醒的預言家" },
        ],
      },
    ],
  } as Pending;

  test("題目上面列出她問之前說的那段話", () => {
    card(question);
    expect(screen.getByTestId("pending-intro").textContent).toBe(
      "希臘神話裡有三位已經在用，我推薦瑟西。",
    );
  });

  test("每個選項的說明寫在名字下面，點了照樣送出名字", async () => {
    card(question);
    const circe = screen.getByRole("button", { name: /^瑟西/ });
    expect(circe.textContent).toContain("優雅腹黑的魔女");
    expect(screen.getAllByTestId("option-description")).toHaveLength(2);
    fireEvent.click(circe);
    await waitFor(() => expect(state.resolve).toHaveBeenCalledTimes(1));
    expect(state.resolve.mock.calls[0]?.[0]).toMatchObject({
      decision: { allow: true, answers: { "你想做哪一位？": "瑟西" } },
    });
  });

  test("沒有說明的選項照舊排成一排、沒有說明列；沒有開場白就不畫那段", () => {
    card({
      ...question,
      summary: "",
      questions: [{ ...question.questions![0]!, options: [{ label: "好" }, { label: "不要" }] }],
    } as Pending);
    expect(screen.queryByTestId("option-description")).toBeNull();
    expect(screen.queryByTestId("pending-intro")).toBeNull();
  });
});

// WEB-122、WEB-157：好幾題、說明很長時卡片跟著訊息區捲，自己不捲（手機上會卡住）；可以多選的要寫出來。
describe("好幾題的選擇題卡片", () => {
  const many = {
    id: "m1",
    kind: "question",
    questions: [
      {
        header: "範圍",
        text: "要做哪些？",
        multi_select: true,
        options: [{ label: "甲" }, { label: "乙" }, { label: "丙" }],
      },
      { header: "時間", text: "什麼時候？", options: [{ label: "今天" }, { label: "明天" }] },
    ],
  } as Pending;

  test("卡片不限高、不自己捲（整張交給訊息區）", () => {
    card(many);
    const box = screen.getByRole("region", { name: t("pending.question", { name: "雅典娜" }) });
    expect(box.className).not.toContain("max-h");
    expect(box.className).not.toContain("overflow-y-auto");
    expect(box.hasAttribute("data-pending-card")).toBe(true);
  });

  // WEB-127：卡片的送出跟下面輸入框的送出鍵以前都叫「送出」，讀螢幕的人分不出是送回答還是送一則新訊息。
  test("卡片的送出鍵叫「送出回答」，跟輸入框的「送出訊息」不同名", () => {
    card(many);
    expect(screen.getByRole("button", { name: "送出回答" })).toBeTruthy();
    expect(t("chat.send")).toBe("送出訊息");
    expect(screen.getByTestId("multi-hint").textContent).toContain("「送出回答」");
  });

  test("可以多選的那題寫「可以選好幾個」，小標在題目上面；選兩個再送出，答案用逗號接起來", async () => {
    card(many);
    expect(screen.getAllByTestId("multi-hint")).toHaveLength(1);
    const legend = screen.getByText("要做哪些？").closest("legend");
    expect(legend?.textContent).toBe("範圍要做哪些？");
    fireEvent.click(screen.getByRole("button", { name: "甲" }));
    fireEvent.click(screen.getByRole("button", { name: "丙" }));
    fireEvent.click(screen.getByRole("button", { name: "明天" }));
    fireEvent.click(screen.getByRole("button", { name: t("pending.submit") }));
    await waitFor(() => expect(state.resolve).toHaveBeenCalledTimes(1));
    expect(state.resolve.mock.calls[0]?.[0]).toMatchObject({
      decision: { allow: true, answers: { "要做哪些？": "甲, 丙", "什麼時候？": "明天" } },
    });
  });
});
