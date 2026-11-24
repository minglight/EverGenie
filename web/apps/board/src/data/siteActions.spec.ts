import { api, type Task } from "@evergenie/sdk-web";
import { QueryClient } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { acceptTask, openTaskRoom, useSettled } from "./answers";
import { apiFor, type SiteRef } from "./sites";
import { createTask } from "./taskCreate";

// 別台的單（S3-7b）：開單、驗收、開專屬對話都送到那台，快取與「已回」的標記帶台名，主伺服器不受影響。
const B: SiteRef = { key: "amy-e2e-b", base: "https://b.example.ts.net" };

afterEach(() => vi.restoreAllMocks());

describe("動作送到那筆所屬的台", () => {
  test("指給別台秘書的單開在那台：送那台的 api，加進那台的清單快取，主伺服器的清單不動", async () => {
    const created: Task = { id: "SEC-1", title: "給 B", agent: "code", archived: false, nudges: 0 };
    const there = vi.spyOn(apiFor(B).tasks, "create").mockResolvedValue({ task: created } as never);
    const here = vi.spyOn(api.tasks, "create");
    const client = new QueryClient();
    client.setQueryData(["site", B.key, "tasks", "all"], []);
    client.setQueryData(["tasks", "all"], []);
    await createTask(client, { title: "給 B", agent: "code", type: "task" }, "雅典娜", () => {}, B);
    expect(there).toHaveBeenCalledWith(expect.objectContaining({ title: "給 B", agent: "code" }));
    expect(here).not.toHaveBeenCalled();
    const rows = client.getQueryData<{ task: Task }[]>(["site", B.key, "tasks", "all"]);
    expect(rows?.map((r) => r.task.id)).toEqual(["SEC-1"]);
    expect(client.getQueryData(["tasks", "all"])).toEqual([]);
  });

  test("驗收別台的單送那台；「已回」的標記用台名/單號，不會蓋到主伺服器同號的單", async () => {
    const there = vi.spyOn(apiFor(B).tasks, "accept").mockResolvedValue({} as never);
    const here = vi.spyOn(api.tasks, "accept");
    const client = new QueryClient();
    const ok = await acceptTask(client, { id: "SEC-1", title: "x" } as Task, { site: B });
    expect(ok).toBe(true);
    expect(there).toHaveBeenCalledWith({ id: "SEC-1" });
    expect(here).not.toHaveBeenCalled();
    const settled = renderHook(() => useSettled()).result.current;
    expect(settled.has("amy-e2e-b/SEC-1")).toBe(true);
    expect(settled.has("SEC-1")).toBe(false);
  });

  test("別台的單開專屬對話開在那台", async () => {
    const there = vi
      .spyOn(apiFor(B).session, "open")
      .mockResolvedValue({ session: { name: "code-sec-1" } } as never);
    const here = vi.spyOn(api.session, "open");
    expect(await openTaskRoom({ id: "SEC-1", agent: "code" } as Task, B)).toBe("code-sec-1");
    expect(there).toHaveBeenCalledWith({ agent: "code", topic: "SEC-1" });
    expect(here).not.toHaveBeenCalled();
  });
});
