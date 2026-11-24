import type { Task } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import {
  compareClosed,
  compareOpen,
  inStatuses,
  matchesQuery,
  parseStatuses,
  taskSearchOf,
  toggleStatus,
} from "./taskViews";

const ids = (tasks: Task[], cmp: (a: Task, b: Task) => number) =>
  [...tasks].sort(cmp).map((t) => t.id);

// DEV-107（作者 2026-11-03 18:22）：分頁收成「未結／已結束」，狀態改成標籤篩選。
describe("「未結」的排法", () => {
  test("先照狀態：等你 → 進行中 → 等外部 → 待做（待做排最下面）", () => {
    const tasks = [
      { id: "idea", status: "idea", created_at: "2026-11-01T00:00:00Z" },
      { id: "ext", status: "waiting_external", updated_at: "2026-11-03T00:00:00Z" },
      { id: "doing", status: "doing", updated_at: "2026-11-02T00:00:00Z" },
      { id: "ask", status: "waiting_user", waiting_since: "2026-11-03T00:00:00Z" },
    ] as Task[];
    expect(ids(tasks, compareOpen)).toEqual(["ask", "doing", "ext", "idea"]);
  });

  test("等你：送驗收的在要回覆的前面，同一種等最久的在前", () => {
    const tasks = [
      { id: "ask-new", status: "waiting_user", waiting_since: "2026-11-03T00:00:00Z" },
      { id: "ask-old", status: "waiting_user", waiting_since: "2026-11-01T00:00:00Z" },
      {
        id: "review-new",
        status: "waiting_user",
        proposed: "done",
        waiting_since: "2026-11-03T00:00:00Z",
      },
      {
        id: "review-old",
        status: "waiting_user",
        proposed: "done",
        waiting_since: "2026-11-02T00:00:00Z",
      },
    ] as Task[];
    expect(ids(tasks, compareOpen)).toEqual(["review-old", "review-new", "ask-old", "ask-new"]);
  });

  test("進行中、等外部：最近更新的在前；待做：開單早的在前（放久的不沉掉）", () => {
    const tasks = [
      { id: "d-old", status: "doing", updated_at: "2026-11-01T00:00:00Z" },
      { id: "d-new", status: "doing", updated_at: "2026-11-03T00:00:00Z" },
      { id: "i-new", status: "idea", created_at: "2026-11-03T00:00:00Z" },
      { id: "i-old", status: "idea", created_at: "2026-10-01T00:00:00Z" },
    ] as Task[];
    expect(ids(tasks, compareOpen)).toEqual(["d-new", "d-old", "i-old", "i-new"]);
  });

  test("「已結束」：最近結的在前，沒有結束時間看更新時間", () => {
    const tasks = [
      { id: "a", closed_at: "2026-11-01T00:00:00Z" },
      { id: "b", updated_at: "2026-11-03T00:00:00Z" },
      { id: "c", closed_at: "2026-11-02T00:00:00Z" },
    ] as Task[];
    expect(ids(tasks, compareClosed)).toEqual(["b", "c", "a"]);
  });
});

describe("狀態標籤篩選", () => {
  const t = (status: Task["status"]) => ({ status }) as Task;

  test("一個都沒選＝全部；選了就只留那幾種", () => {
    expect(inStatuses([], t("idea"))).toBe(true);
    expect(inStatuses(["idea"], t("idea"))).toBe(true);
    expect(inStatuses(["idea"], t("doing"))).toBe(false);
    expect(inStatuses(["idea", "waiting_user"], t("waiting_user"))).toBe(true);
  });

  test("點一下加、再點拿掉，可以複選，照標籤順序", () => {
    expect(toggleStatus([], "idea")).toEqual(["idea"]);
    expect(toggleStatus(["idea"], "waiting_user")).toEqual(["waiting_user", "idea"]);
    expect(toggleStatus(["waiting_user", "idea"], "idea")).toEqual(["waiting_user"]);
  });

  test("網址的 ?status= 只認未結的四種，去重、照順序", () => {
    expect(parseStatuses("idea,waiting_user,idea,done,亂打")).toEqual(["waiting_user", "idea"]);
    expect(parseStatuses(undefined)).toEqual([]);
    expect(parseStatuses(42)).toEqual([]);
  });

  test("舊網址：?view=waiting 是「未結」＋「等你」；active、all 就是「未結」；closed 照舊", () => {
    expect(taskSearchOf("waiting", undefined)).toEqual({ status: "waiting_user" });
    expect(taskSearchOf("active", undefined)).toEqual({});
    expect(taskSearchOf("all", undefined)).toEqual({});
    expect(taskSearchOf("closed", "idea")).toEqual({ view: "closed" });
    expect(taskSearchOf("open", "idea,doing")).toEqual({ status: "doing,idea" });
    expect(taskSearchOf("waiting", "idea")).toEqual({ status: "idea" });
    expect(taskSearchOf("亂打", undefined)).toEqual({});
  });
});

describe("任務單搜尋", () => {
  const t1 = {
    id: "OF-13",
    title: "Controller Force Update",
    body: "PROJ-123 板子測試",
    result: "已合併 PR 1234",
  } as Task;
  test("編號、標題、需求都找得到，不分大小寫", () => {
    expect(matchesQuery(t1, "of-13")).toBe(true);
    expect(matchesQuery(t1, "controller")).toBe(true);
    expect(matchesQuery(t1, "板子")).toBe(true);
    expect(matchesQuery(t1, "迷宮")).toBe(false);
  });
  test("結果欄也找得到（找已結束的單最有線索）", () => {
    expect(matchesQuery(t1, "1234")).toBe(true);
  });
  test("多個字要全部出現；空白搜尋＝全部", () => {
    expect(matchesQuery(t1, "force 123")).toBe(true);
    expect(matchesQuery(t1, "force 迷宮")).toBe(false);
    expect(matchesQuery(t1, "   ")).toBe(true);
  });
});
