import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, test, vi } from "vitest";

const state = vi.hoisted(() => ({ notification: null as Record<string, unknown> | null }));

vi.mock("@evergenie/sdk-web", async () => {
  const actual = await vi.importActual<typeof import("@evergenie/sdk-web")>("@evergenie/sdk-web");
  return {
    ...actual,
    api: {
      ...actual.api,
      notify: {
        ...actual.api.notify,
        get: vi.fn(async () => ({ notification: state.notification })),
        markRead: vi.fn(async () => ({})),
        list: vi.fn(async () => ({ notifications: [] })),
      },
    },
  };
});
vi.mock("@tanstack/react-router", () => ({
  // 連結照 to＋search 組出 href（測按鈕連到哪），其他照舊。
  Link: ({
    children,
    to,
    search,
    ...rest
  }: {
    children: ReactNode;
    to?: string;
    search?: Record<string, string>;
    "data-testid"?: string;
  }) => {
    const q = search && typeof search === "object" ? new URLSearchParams(search).toString() : "";
    return (
      <a href={`${to ?? "/"}${q ? `?${q}` : ""}`} data-testid={rest["data-testid"]}>
        {children}
      </a>
    );
  },
  useNavigate: () => () => {},
}));

import { NotificationView } from "./NotificationView";

function renderView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <NotificationView id="M-7" />
    </QueryClientProvider>,
  );
}

const base = {
  id: "M-7",
  category: "approve",
  title: "確認主人的 Goal：省電",
  todo: "請確認主人的 Goal 與授權",
  options: ["確認", "不要"],
  agent: "work",
  created_at: "2026-10-30T08:00:00Z",
  urgent: false,
};

afterEach(() => {
  cleanup();
  state.notification = null;
});

describe("點進去那件事已經沒了（DEV-49 S2）", () => {
  // Guards: REQ-375
  test("伺服器已經收掉（自動回答）的那則：只顯示「這件已經處理了」，沒有選項按鈕", async () => {
    state.notification = {
      ...base,
      state: "answered",
      answer: { text: "已經不用回了", via: "auto", at: "2026-10-30T08:01:00Z" },
    };
    renderView();
    expect((await screen.findByTestId("message-settled")).textContent).toBe("這件已經處理了");
    expect(screen.queryByRole("button", { name: "確認" })).toBeNull();
  });

  // Guards: REQ-375
  test("還在等的那則照常有選項按鈕", async () => {
    state.notification = { ...base, state: "unread" };
    renderView();
    expect(await screen.findByRole("button", { name: "確認" })).toBeTruthy();
    expect(screen.queryByTestId("message-settled")).toBeNull();
  });
});

describe("「要你做的事」只有還要你回答時是橘色（WEB-70）", () => {
  // RMD-5：到期待辦有三顆一鍵回覆；沒有選項的舊通知還是「只是通知」。
  test("到期待辦的通知有一鍵回覆", async () => {
    state.notification = {
      ...base,
      category: "due",
      state: "unread",
      options: ["做了", "晚點", "今天不做"],
    };
    renderView();
    for (const name of ["做了", "晚點", "今天不做"]) {
      expect(await screen.findByRole("button", { name })).toBeTruthy();
    }
    expect(screen.queryByRole("textbox")).toBeNull();
    cleanup();
    state.notification = { ...base, category: "due", state: "unread", options: [] };
    renderView();
    expect(await screen.findByText("這則只是通知，不用回答。")).toBeTruthy();
  });

  // Guards: REQ-383
  test("還在等你回答：橘色區塊", async () => {
    state.notification = { ...base, state: "read" };
    renderView();
    const todo = await screen.findByTestId("message-todo");
    expect(todo.dataset.tone).toBe("reply");
    expect(todo.className).toContain("bg-attention-soft");
  });

  // Guards: REQ-383
  test("已經回答過、過期、被自動收掉：不是橘色（灰）", async () => {
    for (const done of [
      { state: "answered", answer: { text: "確認", via: "button", at: "2026-10-30T08:01:00Z" } },
      { state: "expired" },
      {
        state: "answered",
        answer: { text: "已經不用回了", via: "auto", at: "2026-10-30T08:01:00Z" },
      },
    ]) {
      state.notification = { ...base, ...done };
      renderView();
      const todo = await screen.findByTestId("message-todo");
      expect(todo.dataset.tone, JSON.stringify(done)).toBe("idle");
      expect(todo.className).not.toContain("attention");
      expect(todo.querySelector("h2")?.className).not.toContain("attention");
      cleanup();
    }
  });

  test("沒事要做（伺服器存「無」）：不畫這一塊", async () => {
    state.notification = {
      ...base,
      category: "done",
      options: undefined,
      todo: "無",
      state: "read",
    };
    renderView();
    expect(await screen.findByText(base.title ?? "")).toBeTruthy();
    expect(screen.queryByTestId("message-todo")).toBeNull();
  });
});

describe("回答過之後那一行小字（NTF-59）", () => {
  const answered = { text: "知道了", via: "button", at: "2026-10-30T08:01:00Z" };
  test("系統提醒這種沒有秘書要讀的（delivers_answer = false）：不說「秘書還沒讀到」", async () => {
    state.notification = {
      ...base,
      agent: "system",
      source: "system.alert",
      options: ["知道了"],
      state: "answered",
      delivers_answer: false,
      answer: answered,
    };
    renderView();
    const row = (await screen.findByText("已回：知道了")).closest('[role="status"]');
    expect(row).toBeTruthy();
    expect(row?.textContent).toContain("你按了按鈕回答");
    expect(row?.textContent).not.toContain("秘書還沒讀到");
  });

  test("秘書問的（delivers_answer = true）還沒讀到：照舊說等她下次開工；舊版的台沒給這欄也一樣", async () => {
    for (const flag of [true, undefined]) {
      state.notification = { ...base, state: "answered", delivers_answer: flag, answer: answered };
      renderView();
      const row = (await screen.findByText("已回：知道了")).closest('[role="status"]');
      expect(row?.textContent).toContain("秘書還沒讀到");
      cleanup();
    }
  });
});

describe("只收打字回答的通知（WEB-112、D-57）", () => {
  test("等你決定的任務問句（typed）：沒有「好／不要」，只有打字回答", async () => {
    state.notification = {
      ...base,
      title: "晚餐訂哪家",
      todo: "要日式還是義式？",
      options: undefined,
      typed: true,
      task: "SEC-9",
      state: "unread",
    };
    renderView();
    expect(await screen.findByText("你的回答")).toBeTruthy();
    expect(screen.queryByText("或打字回答")).toBeNull();
    expect(screen.queryByRole("button", { name: "好" })).toBeNull();
    expect(screen.queryByRole("button", { name: "不要" })).toBeNull();
  });

  test("手機版：問句在上、輸入框在下（先看到問題才知道怎麼答）", async () => {
    state.notification = {
      ...base,
      todo: "要日式還是義式？",
      options: undefined,
      typed: true,
      state: "unread",
    };
    renderView();
    const answer = await screen.findByText("你的回答");
    const question = screen.getByText("要日式還是義式？");
    expect(
      question.compareDocumentPosition(answer) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  test("沒標 typed、也沒給選項的：照舊補「好／不要」", async () => {
    state.notification = { ...base, options: undefined, state: "unread" };
    renderView();
    expect(await screen.findByRole("button", { name: "好" })).toBeTruthy();
  });
});

describe("新角色來打招呼：她的頭像與名字、去角色庫的按鈕（NTF-66、WEB-150，DEV-115）", () => {
  const hello = {
    id: "M-7",
    category: "opening",
    title: "露娜 來打招呼",
    context: "晚安……你來啦。我是露娜，月光下安靜的圖書館員。",
    todo: "無",
    agent: "system",
    speaker: "露娜",
    image: "/avatars/persona/luna.png",
    link: { label: "去角色頁看看", url: "/personas?card=luna" },
    created_at: "2026-11-04T05:30:00Z",
    urgent: false,
    state: "unread",
  };

  test("訊息上畫出「去角色頁看看」按鈕，連到角色庫那張卡", async () => {
    state.notification = hello;
    renderView();
    const button = await screen.findByTestId("message-link-button");
    expect(button.textContent).toBe("去角色頁看看");
    expect(button.getAttribute("href")).toBe("/personas?card=luna");
  });

  test("寄件人寫她的名字、標題旁是她的頭像；說話的不是秘書，不給「跟秘書聊」", async () => {
    state.notification = hello;
    renderView();
    const face = await screen.findByRole("img", { name: "露娜" });
    expect(face.querySelector("img")?.getAttribute("src")).toBe("/avatars/persona/luna.png");
    expect(screen.queryByTestId("message-chat-link")).toBeNull();
    expect(screen.getByText("晚安……你來啦。我是露娜，月光下安靜的圖書館員。")).toBeTruthy();
    // 頭像網址不是附圖：不會多一塊「附圖」。
    expect(screen.queryByText("附圖")).toBeNull();
  });

  test("不是站內路徑的連結不畫成按鈕（含正規化後變成別的網站的）", async () => {
    for (const url of ["https://evil.example/x", "//evil.example/x", "/.//evil.example/x"]) {
      state.notification = { ...hello, link: { label: "外面", url } };
      renderView();
      await screen.findByText("露娜 來打招呼");
      expect(screen.queryByTestId("message-link-button"), url).toBeNull();
      cleanup();
    }
  });

  test("套件的通知帶的站內按鈕照樣畫（習慣的「到展示間」）；指回這則自己的不畫（DEV-157）", async () => {
    state.notification = {
      ...hello,
      speaker: undefined,
      image: undefined,
      agent: "life",
      category: "done",
      title: "送你 1 點",
      link: { label: "到展示間", url: "/p/closet/showroom" },
    };
    renderView();
    const button = await screen.findByTestId("message-link-button");
    expect(button.textContent).toBe("到展示間");
    expect(button.getAttribute("href")).toBe("/p/closet/showroom");
    cleanup();
    for (const url of ["/messages/M-7", "/messages/M-7/"]) {
      state.notification = { ...hello, link: { label: "看這則", url } };
      renderView();
      await screen.findByText("露娜 來打招呼");
      expect(screen.queryByTestId("message-link-button"), url).toBeNull();
      cleanup();
    }
  });
});
