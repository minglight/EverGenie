import type { MessageWait } from "@evergenie/sdk-web";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import type { QueuedMessage } from "./flow";
import { QueuedBubble } from "./QueuedBubble";

afterEach(() => cleanup());

function mount(item: QueuedMessage) {
  const onResend = vi.fn();
  const onSayAgain = vi.fn();
  render(
    <QueuedBubble item={item} agentName="雅典娜" onResend={onResend} onSayAgain={onSayAgain} />,
  );
  return { onResend, onSayAgain };
}

describe("沒送到、這一輪沒做完的泡泡（WEB-67）", () => {
  // Guards: REQ-371
  test("沒送到：紅字「這句沒送到」＋原因，可以重送", () => {
    const { onResend, onSayAgain } = mount({
      key: "a",
      text: "跑測試",
      status: "failed",
      error: "貼不進她的對話（她可能沒在跑）",
    });
    expect(screen.getByRole("alert").textContent).toContain("這句沒送到：貼不進她的對話");
    fireEvent.click(screen.getByRole("button", { name: "重送" }));
    expect(onResend).toHaveBeenCalledTimes(1);
    expect(onSayAgain).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "再說一次" })).toBeNull();
  });

  test("這一輪沒做完：紅字「她沒回完這一句：<原因>」，沒有重送鈕，只能「再說一次」", () => {
    const { onResend, onSayAgain } = mount({
      key: "b",
      text: "跑測試",
      status: "interrupted",
      error: "AI 平台暫時忙不過來",
      kind: "overloaded",
    });
    const note = screen.getByRole("status");
    expect(note.textContent).toContain("雅典娜 沒回完這一句：AI 平台暫時忙不過來");
    expect(note.className).toContain("text-danger");
    expect(screen.queryByRole("button", { name: "重送" })).toBeNull();
    expect(screen.queryByRole("link", { name: "看怎麼處理" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "再說一次" }));
    expect(onSayAgain).toHaveBeenCalledTimes(1);
    expect(onResend).not.toHaveBeenCalled();
    cleanup();
    mount({ key: "b2", text: "跑測試", status: "interrupted" });
    const bare = screen.getByRole("status").textContent ?? "";
    expect(bare).toContain("雅典娜 沒回完這一句");
    expect(bare).not.toContain("：");
  });

  // DEV-156 U-08：登入過期、帳單這種再說一次也沒用——不給「再說一次」，給「看怎麼處理」（到訊息頁看那則提醒）。
  test("帳號登入過期、帳單有問題：不給「再說一次」，改給「看怎麼處理」", () => {
    for (const kind of ["auth", "billing"]) {
      mount({ key: kind, text: "跑測試", status: "interrupted", error: "AI 帳號登入過期了", kind });
      expect(screen.queryByRole("button", { name: "再說一次" })).toBeNull();
      expect(screen.getByRole("link", { name: "看怎麼處理" }).getAttribute("href")).toBe(
        "/messages",
      );
      cleanup();
    }
  });

  // DEV-156 U-08：平台的原話（多半是英文）預設不在畫面上，按「詳細」才展開；同一件事只講一遍。
  test("平台原話收在「詳細」：預設看不到，按了才展開", () => {
    mount({
      key: "x",
      text: "跑測試",
      status: "interrupted",
      error: "AI 平台暫時忙不過來",
      detail: "API Error: 529 Overloaded",
      kind: "overloaded",
    });
    expect(screen.queryByText(/529 Overloaded/)).toBeNull();
    expect(screen.getByRole("status").textContent?.match(/忙不過來/g)).toHaveLength(1);
    const more = screen.getByRole("button", { name: "詳細" });
    expect(more.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(more);
    expect(screen.getByTestId("interrupted-detail").textContent).toContain(
      "API Error: 529 Overloaded",
    );
    cleanup();
    mount({ key: "y", text: "跑測試", status: "interrupted", error: "你叫停了" });
    expect(screen.queryByRole("button", { name: "詳細" })).toBeNull();
  });

  // WEB-119：那句已經在對話記錄裡（她收到了才出錯），只留紅字與「再說一次」，不再畫第二個一樣的泡泡；按鈕不換行。
  test("這一輪沒做完、那句已在對話記錄裡：不再畫一次那句，只留紅字和不換行的「再說一次」", () => {
    mount({
      key: "d",
      text: "跑測試 abc",
      status: "interrupted",
      error: "Overloaded",
      inLog: true,
    });
    expect(screen.queryByText("跑測試 abc")).toBeNull();
    expect(screen.getByRole("status").textContent).toContain("雅典娜 沒回完這一句：Overloaded");
    expect(screen.getByRole("button", { name: "再說一次" }).className).toContain(
      "whitespace-nowrap",
    );
    cleanup();
    mount({ key: "e", text: "跑測試 abc", status: "interrupted", error: "Overloaded" });
    expect(screen.getByText("跑測試 abc")).toBeTruthy();
  });

  // Guards: REQ-371
  test("還在送的：沒有紅字、沒有按鈕", () => {
    mount({ key: "c", text: "跑測試", status: "queued" });
    expect(screen.getByText("傳送中…")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("送出後 3 秒規則的泡泡（WEB-71）", () => {
  // Guards: REQ-384
  test("還沒收到：寫伺服器給的原因（等你回答、正在叫醒她、在收前一句）", () => {
    mount({ key: "d", text: "順便看信", status: "sent", wait: "answer" });
    expect(screen.getByText("雅典娜 在等你回答上面的問題，這句答完就送")).toBeTruthy();
    cleanup();
    mount({ key: "e", text: "醒醒", status: "sent", wait: "waking" });
    expect(screen.getByText("正在叫醒 雅典娜，醒了就送")).toBeTruthy();
    cleanup();
    mount({ key: "f", text: "再一句", status: "sent", wait: "busy" });
    expect(screen.getByText("雅典娜 正在收前一句，接著就送")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    cleanup();
    // 引擎說上一次沒貼進去、派送器在重試（S5）：灰字，不是紅字、沒有按鈕。
    mount({ key: "g", text: "再試", status: "sent", wait: "retrying" });
    const retrying = screen.getByText("還沒貼進 雅典娜 的對話，正在重試");
    expect(retrying.className).toContain("text-muted");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  // Guards: REQ-384

  test("不認得的原因（含舊版那台送來的 late）：照「已送出」灰字，沒有紅字、沒有按鈕", () => {
    mount({ key: "h", text: "在嗎", status: "sent", wait: "late" as MessageWait });
    expect(screen.getByText("已送出").className).toContain("text-muted");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  // Guards: REQ-384
  test("沒有原因、還在 3 秒內：只寫「已送出」，不自己猜「可能在忙」", () => {
    mount({ key: "i", text: "在嗎", status: "sent", sentAt: Date.now() - 60_000 });
    expect(screen.getByText("已送出")).toBeTruthy();
    expect(screen.queryByText(/可能正在忙/)).toBeNull();
  });
});

describe("額度用完的泡泡（WEB-73）", () => {
  // Guards: REQ-378
  test("灰字「額度用完，13:40 恢復，到時就送」，不是出錯、沒有重送", () => {
    const at = new Date();
    at.setHours(13, 40, 0, 0);
    mount({
      key: "l",
      text: "幫我看備份",
      status: "sent",
      wait: "limited",
      waitUntil: at.toISOString(),
    });
    const hint = screen.getByText(
      "額度用完，13:40 恢復，到時就送。這是 AI 訂閱的用量上限，不是壞掉",
    );
    expect(hint.className).toContain("text-muted");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    cleanup();
    mount({ key: "m", text: "在嗎", status: "sent", wait: "limited" });
    expect(screen.getByText("額度用完，恢復了就送。這是 AI 訂閱的用量上限，不是壞掉")).toBeTruthy();
  });
});
