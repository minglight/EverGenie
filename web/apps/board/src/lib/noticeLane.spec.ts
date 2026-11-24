import type { Notification } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import { collapseRuns, countsInBadge, laneOf, noticeKind } from "./noticeLane";

const n = (over: Partial<Notification>): Notification =>
  ({
    id: "M-1",
    agent: "admin",
    category: "done",
    title: "x",
    urgent: false,
    ...over,
  }) as Notification;

describe("訊息分兩道（D-47、WEB-86）", () => {
  test("照核心給的 lane；舊版的台沒給時只看 source", () => {
    expect(laneOf(n({ lane: "system" }))).toBe("system");
    expect(laneOf(n({ lane: "agent", source: "system.alert" }))).toBe("agent");
    expect(laneOf(n({ source: "system.alert" }))).toBe("system");
    expect(laneOf(n({ source: "package" }))).toBe("system");
    // 沒有 lane 的舊台：看板不自己認秘書 id，admin 的也先算秘書。
    expect(laneOf(n({}))).toBe("agent");
  });

  test("徽章照核心給的 badge；舊版的台沒給時，不是討論串內提問的未讀都算", () => {
    expect(countsInBadge(n({ lane: "system", state: "unread", badge: false }))).toBe(false);
    expect(countsInBadge(n({ lane: "system", state: "unread", badge: true }))).toBe(true);
    const old = (over: Partial<Notification>) => {
      const x = n(over) as Partial<Notification>;
      delete x.badge;
      return x as Notification;
    };
    expect(countsInBadge(old({ state: "unread" }))).toBe(true);
    expect(countsInBadge(old({ state: "read" }))).toBe(false);
    expect(countsInBadge(old({ state: "unread", inline: true }))).toBe(false);
  });

  test("同一種：拿掉 commit 編號與數字，不同秘書或分類就不是同一種", () => {
    const a = noticeKind(n({ title: "每晚測試沒過：8c4e971f（false）" }));
    expect(noticeKind(n({ title: "每晚測試沒過：c703aef8（false）" }))).toBe(a);
    expect(
      noticeKind(n({ title: "每晚測試沒過：6c27669e1234567890abcdef1234567890abcdef（false）" })),
    ).toBe(a);
    expect(noticeKind(n({ title: "每晚測試沒過：8c4e971f（true）" }))).not.toBe(a);
    expect(noticeKind(n({ agent: "system", title: "每晚測試沒過：8c4e971f（false）" }))).not.toBe(
      a,
    );
    expect(
      noticeKind(n({ category: "digest", title: "每晚測試沒過：8c4e971f（false）" })),
    ).not.toBe(a);
    // 全是 a～f 字母的英文字（例 defaced）沒有數字，不當成編號。
    expect(noticeKind(n({ title: "網頁被 defaced 了" }))).toContain("defaced");
    expect(noticeKind(n({ title: "第 3 次備份" }))).toBe(noticeKind(n({ title: "第 12 次備份" })));
  });

  test("只收連在一起的；keyOf 回 null 的自己一段", () => {
    const runs = collapseRuns(["a1", "a2", "b1", "a3", "x", "x"], (s) =>
      s === "x" ? null : s.slice(0, 1),
    );
    expect(runs).toEqual([["a1", "a2"], ["b1"], ["a3"], ["x"], ["x"]]);
    expect(collapseRuns([], () => "k")).toEqual([]);
  });
});
