import type { ThreadSummary } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, test, vi } from "vitest";
import {
  agentKey,
  groupRoster,
  presenceOf,
  primaryOf,
  splitAgentKey,
  useSiteAgents,
} from "./agents";
import { SITES_CONFIG_KEY } from "./sites";

const thread = (
  name: string,
  agent: string,
  status: ThreadSummary["status"],
  at = "",
): ThreadSummary => ({
  name,
  agent,
  status,
  last_assistant_at: at,
  unread: 0,
  queued: 0,
  background_tasks: 0,
});

describe("秘書狀態", () => {
  // Guards: REQ-142
  test("有串在等你 → 等你；有串在工作 → 工作中；其他 → 有空", () => {
    const threads = [
      thread("a-1", "a", "waiting_user"),
      thread("b-1", "b", "working"),
      thread("c-1", "c", "idle", "2026-10-24T10:00:00Z"),
      thread("c-2", "c", "done", "2026-10-24T11:00:00Z"),
    ];
    expect(presenceOf(threads, "a")).toEqual({ presence: "waiting", session: "a-1" });
    expect(presenceOf(threads, "b")).toEqual({ presence: "working", session: "b-1" });
    expect(presenceOf(threads, "c")).toEqual({ presence: "idle", session: "c-2" });
    expect(presenceOf(threads, "d")).toEqual({ presence: "idle", session: undefined });
  });

  // Guards: REQ-142
  test("等你比工作中優先", () => {
    const threads = [thread("a-1", "a", "working"), thread("a-2", "a", "waiting_user")];
    expect(presenceOf(threads, "a").presence).toBe("waiting");
  });
});

describe("看板上的秘書名單（WEB-49）", () => {
  // Guards: REQ-267
  test("同一位角色坐兩個職位合成一張，職責併在一起", () => {
    const { people, merged } = groupRoster([
      { id: "code", name: "雅典娜", persona: "athena", enabled: true, duty: "寫程式" },
      { id: "life", name: "維納斯", persona: "venus", enabled: true, duty: "生活" },
      { id: "system", name: "輝夜", persona: "kaguya", enabled: true, duty: "電腦" },
      { id: "work", name: "雅典娜", persona: "athena", enabled: true, duty: "工作" },
    ]);
    expect(people.map((p) => [p.id, p.duty])).toEqual([
      ["code", "寫程式；工作"],
      ["life", "生活"],
      ["system", "電腦"],
    ]);
    expect([...merged]).toEqual([["work", "code"]]);
  });
});

describe("兩台的秘書（S3）", () => {
  afterEach(() => vi.unstubAllGlobals());

  test("agentKey：主伺服器就是職位 id，別台是「台名/職位 id」，拆得回來", () => {
    expect(agentKey("", "code")).toBe("code");
    expect(agentKey("amy-office", "code")).toBe("amy-office/code");
    expect(splitAgentKey("code")).toEqual({ site: "", id: "code" });
    expect(splitAgentKey("amy-office/code")).toEqual({ site: "amy-office", id: "code" });
  });

  test("每台各自的名冊與合併表；別台頭像補成那台的網址；連不上、太舊的台不抓名冊", async () => {
    const OFFICE = "https://genie-amy-office.example.ts.net";
    const OLD = "https://old.example";
    const DEAD = "https://dead.example";
    const json = (body: unknown) =>
      new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
    const fetchMock = vi.fn<typeof fetch>(async (input) => {
      const url = String(input);
      if (url === "/api/system/get_mode") return json({ label: "amy-mini", board_api: 2 });
      if (url === "/api/web/get_roster")
        return json({
          agents: [
            { id: "a", name: "甲", persona: "p1", enabled: true, avatar_url: "/avatars/a.png" },
            { id: "d", name: "甲", persona: "p1", enabled: true },
          ],
        });
      if (url === `${OFFICE}/api/system/get_mode`)
        return json({ label: "amy-office", board_api: 2 });
      if (url === `${OFFICE}/api/web/get_roster`)
        return json({
          agents: [
            {
              id: "a",
              name: "別台的甲",
              persona: "p1",
              enabled: true,
              avatar_url: "/avatars/a.png",
            },
            { id: "d", name: "丁", persona: "p2", enabled: true },
          ],
        });
      if (url === `${OLD}/api/system/get_mode`) return json({ label: "amy-old", board_api: 1 });
      throw new TypeError("Failed to fetch");
    });
    vi.stubGlobal("fetch", fetchMock);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(SITES_CONFIG_KEY, [{ url: OFFICE }, { url: OLD }, { url: DEAD }]);
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client }, children);
    const { result } = renderHook(() => useSiteAgents(), { wrapper });
    await waitFor(() => {
      expect(result.current.map((s) => [s.site.key, s.list.map((a) => a.id)])).toEqual([
        ["", ["a"]],
        ["amy-office", ["a", "d"]],
      ]);
    });
    const [mini, office] = result.current;
    // 主伺服器的 d 併進 a；別台的 d 是另一位，不套用主伺服器的合併。
    expect(primaryOf("d")).toBe("a");
    expect(primaryOf("d", "amy-office")).toBe("d");
    expect(mini?.view("a")).toMatchObject({ key: "a", avatarUrl: "/avatars/a.png" });
    expect(office?.view("a")).toMatchObject({
      key: "amy-office/a",
      name: "別台的甲",
      avatarUrl: `${OFFICE}/avatars/a.png`,
    });
    // 連不上的台試過 manifest 了，太舊、連不上的台都沒打名冊。
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some((c) => String(c[0]) === `${DEAD}/manifest.webmanifest`),
      ).toBe(true),
    );
    const asked = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(asked).not.toContain(`${OLD}/api/web/get_roster`);
    expect(asked).not.toContain(`${DEAD}/api/web/get_roster`);
  });
});
