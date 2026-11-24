import { api, type Notification, type Task } from "@evergenie/sdk-web";
import { QueryClient } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { acceptTask, answerNotification, openTaskRoom, useFailures, useSettled } from "./answers";
import { apiFor, type SiteRef } from "./sites";

// 今天頁、訊息頁合併兩台（S3-7a）：在 A 的看板回答 B 的那則，動作要送到 B，不可以送到主伺服器（FEN-28）。
const B: SiteRef = { key: "amy-office", base: "https://genie-amy-office.example.ts.net" };

afterEach(() => vi.restoreAllMocks());

describe("在 A 的看板處理別台的事", () => {
  test("TestAnswerOtherSiteGoesToThatSite：回答送到那台、重抓那台的 key，主伺服器不動", async () => {
    const remote = vi.spyOn(apiFor(B).notify, "answer").mockResolvedValue({} as never);
    const local = vi.spyOn(api.notify, "answer").mockResolvedValue({} as never);
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const ok = await answerNotification(
      client,
      { id: "M-1", title: "要不要" } as Notification,
      "同意",
      "button",
      { site: B },
    );
    expect(ok).toBe(true);
    expect(remote).toHaveBeenCalledWith({ id: "M-1", text: "同意", via: "button" });
    expect(local).not.toHaveBeenCalled();
    const keys = invalidate.mock.calls.map((c) => c[0]?.queryKey);
    expect(keys).toContainEqual(["site", "amy-office", "notifications"]);
    expect(keys).not.toContainEqual(["notifications"]);
  });

  test("TestAcceptOtherSiteTaskGoesToThatSite：驗收別台的單送到那台", async () => {
    const remote = vi.spyOn(apiFor(B).tasks, "accept").mockResolvedValue({} as never);
    const local = vi.spyOn(api.tasks, "accept").mockResolvedValue({} as never);
    await acceptTask(new QueryClient(), { id: "SEC-9", title: "x" } as Task, { site: B });
    expect(remote).toHaveBeenCalledWith({ id: "SEC-9" });
    expect(local).not.toHaveBeenCalled();
  });

  test("TestOpenTaskRoomOnOtherSite：別台的單在那台開對話", async () => {
    const remote = vi
      .spyOn(apiFor(B).session, "open")
      .mockResolvedValue({ session: { name: "code-sec-9" } } as never);
    const local = vi.spyOn(api.session, "open");
    await expect(openTaskRoom({ id: "SEC-9", agent: "code" } as Task, B)).resolves.toBe(
      "code-sec-9",
    );
    expect(remote).toHaveBeenCalledWith({ agent: "code", topic: "SEC-9" });
    expect(local).not.toHaveBeenCalled();
  });
});

// 送到的時候這則已經被自動收掉（催的時候換成新的一則、那件事不用回了）：伺服器照舊回成功、回的是自動的那個回答。
// 不能說「已回」——主人的回答沒記上；當成沒送出、說原因（DEV-156 review）。
test("TestAnswerLandsOnRetiredNoticeIsNotSent", async () => {
  vi.spyOn(api.notify, "answer").mockResolvedValue({
    notification: { id: "M-1", answer: { text: "又提醒了一次，看新的那則", via: "auto" } },
    delivered: false,
  } as never);
  const ok = await answerNotification(
    new QueryClient(),
    { id: "M-1", title: "要不要改週四" } as Notification,
    "改週四",
    "chat",
    { settleAs: { key: "SEC-1", id: "SEC-1" } },
  );
  expect(ok).toBe(false);
  expect(renderHook(() => useSettled()).result.current.has("SEC-1")).toBe(false);
  expect(renderHook(() => useFailures()).result.current.get("SEC-1")).toBe(
    "沒送出去：這則已經自動收掉了（又提醒了一次，看新的那則）",
  );
});
