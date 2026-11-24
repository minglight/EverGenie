import { describe, expect, it } from "vitest";
import { backTarget } from "./backTarget";

describe("backTarget", () => {
  it("今天是最上層，沒有上一頁", () => {
    expect(backTarget("/", {})).toBeNull();
  });

  it("詳情回自己的清單", () => {
    expect(backTarget("/tasks/DEV-1", {})).toEqual({ to: "/tasks" });
    expect(backTarget("/reminders/R-1", { home: "office" })).toEqual({ to: "/reminders" });
    expect(backTarget("/goals/G-1", {})).toEqual({ to: "/goals" });
    expect(backTarget("/chat/s1", {})).toEqual({ to: "/chat" });
  });

  it("訊息詳情回清單時留著從哪一道、哪一段來的", () => {
    expect(backTarget("/messages/n1", { show: "all", lane: "system", home: "x" })).toEqual({
      to: "/messages",
      search: { show: "all", lane: "system" },
    });
  });

  it("排程與角色卡的詳情回同一頁的清單", () => {
    expect(backTarget("/tasks/jobs", { job: "j1" })).toEqual({ to: "/tasks/jobs" });
    expect(backTarget("/tasks/jobs", {})).toEqual({ to: "/tasks" });
    // 一位角色一頁（DEV-143）：/personas/<代號>（不管在哪個分頁）回角色清單。
    expect(backTarget("/personas/kaguya", { tab: "demo.tab" })).toEqual({
      to: "/personas",
    });
  });

  it("其他頁回今天", () => {
    for (const path of ["/tasks", "/chat", "/settings/notify", "/policy", "/p/closet/outfit"]) {
      expect(backTarget(path, {})).toEqual({ to: "/" });
    }
  });

  // DEV-156 U-12：手機的設定首頁是清單，各頁的上一層是它；清單本身回今天。
  it("手機上設定的各頁回設定首頁，設定首頁回今天", () => {
    expect(backTarget("/settings/notify", {}, { settingsHome: true })).toEqual({ to: "/settings" });
    expect(backTarget("/settings/agents", { claim: "x" }, { settingsHome: true })).toEqual({
      to: "/settings",
    });
    expect(backTarget("/settings", {}, { settingsHome: true })).toEqual({ to: "/" });
  });

  it("沒有歷史時一路往上一定停在今天（不繞圈）", () => {
    const starts = [
      ["/messages/n1", { show: "all" }],
      ["/tasks/jobs", { job: "j1" }],
      ["/tasks/DEV-1", {}],
      ["/personas/x", { tab: "y" }],
      ["/p/habits/detail", { id: "1" }],
      ["/chat/s1/", {}],
    ] as const;
    for (const [path, search] of starts) {
      let at: { to: string; search?: Record<string, unknown> } | null = { to: path, search };
      let steps = 0;
      while (at && steps < 5) {
        at = backTarget(at.to, at.search ?? {});
        steps++;
      }
      expect(at, path).toBeNull();
    }
  });
});
