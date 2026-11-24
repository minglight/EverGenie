import type { Task } from "@evergenie/sdk-web";
import { api } from "@evergenie/sdk-web";
import { afterEach, describe, expect, test, vi } from "vitest";
import { openTaskRoom } from "./answers";

afterEach(() => vi.restoreAllMocks());

describe("開任務單的專屬對話", () => {
  // 用單號與負責秘書開（或接回）這張單的對話，回對話名稱給畫面導過去。
  test("回對話名稱", async () => {
    const open = vi
      .spyOn(api.session, "open")
      .mockResolvedValue({ session: { name: "code-sec-138" } } as never);
    const room = await openTaskRoom({ id: "SEC-138", agent: "code" } as Task);
    expect(room).toBe("code-sec-138");
    expect(open).toHaveBeenCalledWith({ agent: "code", topic: "SEC-138" });
  });

  test("沒有回 session 名稱就丟錯", async () => {
    vi.spyOn(api.session, "open").mockResolvedValue({} as never);
    await expect(openTaskRoom({ id: "SEC-1", agent: "code" } as Task)).rejects.toThrow();
  });
});
