import type { Task } from "@evergenie/sdk-web";
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, test } from "vitest";
import { addToTaskLists, taskRequest } from "./taskCreate";

describe("看板開任務單的請求", () => {
  // Guards: REQ-186
  test("只送要的欄位：新單是待做（DEV-124），空的說明與 Goal 不送", () => {
    expect(
      taskRequest({ title: "  整理桌面  ", agent: "code", type: "task", body: "  ", goal: "" }),
    ).toEqual({
      title: "整理桌面",
      agent: "code",
      type: "task",
      status: "idea",
    });
    const full = taskRequest({
      title: "查一下",
      agent: "life",
      type: "ask",
      body: "背景",
      goal: "GOAL-3",
    });
    expect(full).toEqual({
      title: "查一下",
      agent: "life",
      type: "ask",
      status: "idea",
      body: "背景",
      goal: "GOAL-3",
    });
  });

  // 附件（TSK-28）：有才送，空的清單不送。
  test("附件有才帶 attachments", () => {
    expect(
      taskRequest({
        title: "看圖",
        agent: "code",
        type: "task",
        attachments: ["/share/a.png", "/share/b.pdf"],
      }).attachments,
    ).toEqual(["/share/a.png", "/share/b.pdf"]);
    expect(
      "attachments" in taskRequest({ title: "看圖", agent: "code", type: "task", attachments: [] }),
    ).toBe(false);
  });

  // Guards: REQ-186
  test("開完單，全部未結的清單馬上有這張，等你的清單不動", () => {
    const client = new QueryClient();
    const old: Task = { id: "SEC-1", title: "舊的", archived: false, nudges: 0 };
    client.setQueryData(["tasks", "all"], [{ task: old, waitingDays: 0 }]);
    client.setQueryData(["tasks", "waiting"], []);
    const created: Task = { id: "SEC-2", title: "新的", archived: false, nudges: 0 };
    addToTaskLists(client, created);
    const rows = client.getQueryData<{ task: Task }[]>(["tasks", "all"]);
    expect(rows?.map((r) => r.task.id)).toEqual(["SEC-2", "SEC-1"]);
    expect(client.getQueryData(["tasks", "waiting"])).toEqual([]);
    // 沒載入過的檢視不憑空建出來。
    expect(client.getQueryData(["tasks", "closed"])).toBeUndefined();
  });
});
