import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

const sites = vi.hoisted(() => ({
  // 別台（amy-office）的名冊；預設只有主伺服器一台，跟 S3 之前一樣。
  remote: null as null | { id: string; name: string; enabled: boolean }[],
}));

vi.mock("./agents", async (actual) => {
  const real = await actual<typeof import("./agents")>();
  const list = [
    { id: "a", name: "甲", enabled: true },
    { id: "b", name: "乙", enabled: true },
    { id: "c", name: "丙", enabled: true },
  ];
  const view = (id?: string) => ({ id, name: id ? `人${id}` : "系統" });
  return {
    ...real,
    useSiteAgents: () => [
      { site: { key: "" }, list, view },
      ...(sites.remote
        ? [
            {
              site: { key: "amy-office" },
              list: sites.remote,
              view: (id?: string) => ({ id, name: `office 的${id}` }),
            },
          ]
        : []),
    ],
    // 「d」是跟「a」同一位角色的另一個職位（合成一張卡，WEB-49）；只有主伺服器有這個合併。
    primaryOf: (id: string, site = "") => (site === "" && id === "d" ? "a" : id),
  };
});

import {
  AGENT_FILTER_KEY,
  AGENT_FILTER_KEY_V1,
  agentFilterStore,
  countByAgent,
  forgetSiteInAgentFilter,
  matchAgent,
  normalizePicked,
  parseStored,
  pickedOf,
  reloadAgentFilter,
  useAgentFilter,
  usePickedNames,
} from "./agentFilter";

beforeEach(() => {
  sites.remote = null;
  localStorage.clear();
  reloadAgentFilter();
});

const only = (...ids: string[]) => new Map([["", ids]]);

const stored = () => JSON.parse(localStorage.getItem(AGENT_FILTER_KEY) ?? "null");

describe("秘書篩選的狀態", () => {
  // Guards: REQ-155
  test("一次只看一位：點別人換成她、點已選的那位回到全部", () => {
    const { result } = renderHook(() => useAgentFilter());
    expect(result.current.picked).toEqual([]);
    act(() => result.current.toggle("c"));
    act(() => result.current.toggle("a"));
    expect(result.current.picked).toEqual(["a"]);
    expect(result.current.isOn("a")).toBe(true);
    expect(result.current.isOn("c")).toBe(false);
    act(() => result.current.toggle("a"));
    expect(result.current.picked).toEqual([]);
    expect(stored()).toEqual([]);
  });

  test("以前存的多選只留第一位（照名冊順序）", () => {
    localStorage.setItem(AGENT_FILTER_KEY, JSON.stringify(["c", "a"]));
    reloadAgentFilter();
    const { result } = renderHook(() => useAgentFilter());
    expect(result.current.picked).toEqual(["a"]);
    act(() => result.current.toggle("c"));
    expect(stored()).toEqual(["c"]);
  });

  // Guards: REQ-155
  test("只看這一位：不管之前選了誰，換成只剩她一位（今天的秘書卡片進她的首頁）", () => {
    const { result } = renderHook(() => useAgentFilter());
    act(() => result.current.toggle("a"));
    act(() => result.current.toggle("c"));
    act(() => result.current.only("b"));
    expect(result.current.picked).toEqual(["b"]);
    expect(localStorage.getItem(AGENT_FILTER_KEY)).toBe(JSON.stringify(["b"]));
  });

  // Guards: REQ-155
  test("清空＝全部，而且存回 localStorage", () => {
    const { result } = renderHook(() => useAgentFilter());
    act(() => result.current.toggle("b"));
    expect(stored()).toEqual(["b"]);
    act(() => result.current.clear());
    expect(result.current.picked).toEqual([]);
    expect(stored()).toEqual([]);
  });

  // Guards: REQ-155
  test("上次的選擇從 localStorage 讀回來；其他分頁改了靠 storage 事件同步", () => {
    localStorage.setItem(AGENT_FILTER_KEY, JSON.stringify(["c", "a"]));
    reloadAgentFilter();
    const { result } = renderHook(() => useAgentFilter());
    expect(result.current.picked).toEqual(["a"]);

    localStorage.setItem(AGENT_FILTER_KEY, JSON.stringify(["b"]));
    act(() => {
      window.dispatchEvent(new StorageEvent("storage", { key: AGENT_FILTER_KEY }));
    });
    expect(result.current.picked).toEqual(["b"]);
    expect(agentFilterStore.get()).toEqual(["b"]);
  });

  // Guards: REQ-155
  test("壞掉的 JSON、不是陣列、不是字串都當全部", () => {
    expect(parseStored("{不是 json")).toEqual([]);
    expect(parseStored('{"a":1}')).toEqual([]);
    expect(parseStored(null)).toEqual([]);
    expect(parseStored('["a",3,null,"","a","b"]')).toEqual(["a", "b"]);
    localStorage.setItem(AGENT_FILTER_KEY, "壞掉");
    reloadAgentFilter();
    const { result } = renderHook(() => useAgentFilter());
    expect(result.current.picked).toEqual([]);
  });

  // Guards: REQ-155
  test("名冊裡沒有的 id 被丟掉（秘書被停用或改名之後不會卡在看不見的選擇）", () => {
    expect(normalizePicked(["x", "b", "a"], only("a", "b", "c"))).toEqual(["a", "b"]);
    // 名冊還沒載入時原樣保留，載入後才丟。
    expect(normalizePicked(["x", "b"], only())).toEqual(["x", "b"]);
    localStorage.setItem(AGENT_FILTER_KEY, JSON.stringify(["x"]));
    reloadAgentFilter();
    const { result } = renderHook(() => useAgentFilter());
    expect(result.current.picked).toEqual([]);
    act(() => result.current.toggle("b"));
    expect(stored()).toEqual(["b"]);
  });
});

describe("matchAgent", () => {
  // Guards: REQ-155
  test("沒選＝全部都算；有選時，沒有秘書或不在選擇裡的不算", () => {
    expect(matchAgent([], "a")).toBe(true);
    expect(matchAgent([], undefined)).toBe(true);
    expect(matchAgent(["a"], "a")).toBe(true);
    expect(matchAgent(["a"], "b")).toBe(false);
    expect(matchAgent(["a"], undefined)).toBe(false);
    expect(matchAgent(["a"], "")).toBe(false);
  });

  // Guards: REQ-155
  test("每位秘書的件數：沒有秘書的資料不算", () => {
    const rows = [{ o: "a" }, { o: "a" }, { o: "b" }, { o: "" }, {}];
    expect(countByAgent(rows, (r: { o?: string }) => r.o)).toEqual({ a: 2, b: 1 });
  });

  // Guards: REQ-267
  test("同一位角色的另一個職位算在那張卡：選了卡就看得到，件數也加在卡上", () => {
    expect(matchAgent(["a"], "d")).toBe(true);
    expect(matchAgent(["b"], "d")).toBe(false);
    expect(countByAgent([{ o: "a" }, { o: "d" }], (r: { o?: string }) => r.o)).toEqual({ a: 2 });
  });
});

describe("兩台（S3）", () => {
  test("篩選從 v1 升到 v2：S3 之前存的職位 id 照樣是主伺服器的那幾位，寫成 v2、刪掉 v1", () => {
    localStorage.setItem(AGENT_FILTER_KEY_V1, JSON.stringify(["c", "a"]));
    reloadAgentFilter();
    const { result } = renderHook(() => useAgentFilter());
    expect(result.current.picked).toEqual(["a"]);
    expect(stored()).toEqual(["c", "a"]);
    expect(localStorage.getItem(AGENT_FILTER_KEY_V1)).toBeNull();
  });

  test("已經有 v2 時不看 v1（不會用舊的蓋掉新的選擇）", () => {
    localStorage.setItem(AGENT_FILTER_KEY, JSON.stringify(["b"]));
    localStorage.setItem(AGENT_FILTER_KEY_V1, JSON.stringify(["a"]));
    reloadAgentFilter();
    const { result } = renderHook(() => useAgentFilter());
    expect(result.current.picked).toEqual(["b"]);
  });

  test("兩台同一個職位算同一位：點哪一台的頭像都是兩台的她，再點一次回到全部；資料的 key 照舊分台", () => {
    sites.remote = [{ id: "a", name: "別台的甲", enabled: true }];
    const { result } = renderHook(() => useAgentFilter());
    act(() => result.current.toggle("amy-office/a"));
    // 照台排：主伺服器在前；one 是主伺服器那台的。
    expect(result.current.picked).toEqual(["a", "amy-office/a"]);
    expect(result.current.one).toBe("a");
    expect(result.current.isOn("a")).toBe(true);
    expect(matchAgent(result.current.picked, "a")).toBe(true);
    expect(matchAgent(result.current.picked, "a", "amy-office")).toBe(true);
    expect(matchAgent(result.current.picked, "b")).toBe(false);
    act(() => result.current.toggle("a"));
    expect(result.current.picked).toEqual([]);
    // 只有一台有的那位：只有她那台的 key。
    act(() => result.current.toggle("b"));
    expect(result.current.picked).toEqual(["b"]);
    expect(
      countByAgent([{ o: "a" }, { o: "a" }], (r: { o?: string }) => r.o, "amy-office"),
    ).toEqual({
      "amy-office/a": 2,
    });
    // 主伺服器的角色合併（d 併進 a）不套用到別台。
    expect(matchAgent(["a"], "d")).toBe(true);
    expect(matchAgent(["amy-office/a"], "d", "amy-office")).toBe(false);
  });

  test("「一位」固定是主伺服器那台的她：別台名冊先到、主伺服器的還沒到時也一樣", () => {
    // 主伺服器名冊還空著：她存的 key 排在後面，但 one 仍是主伺服器的（建單、待辦負責人不會指到別台）。
    const keys = pickedOf(
      ["a"],
      new Map([
        ["", []],
        ["amy-office", ["amy-office/a"]],
      ]),
    );
    expect(keys).toEqual(["amy-office/a", "a"]);
    sites.remote = [{ id: "a", name: "別台的甲", enabled: true }];
    localStorage.setItem(AGENT_FILTER_KEY, JSON.stringify(["amy-office/a"]));
    reloadAgentFilter();
    const { result } = renderHook(() => useAgentFilter());
    expect(result.current.one).toBe("a");
  });

  test("她存的那台沒有她了、別台還有同一個職位：照樣選著別台的她，不悄悄回到全部", () => {
    const rosters = new Map([
      ["", ["b"]],
      ["amy-office", ["amy-office/a"]],
    ]);
    expect(pickedOf(["a"], rosters)).toEqual(["amy-office/a"]);
    expect(pickedOf(["x"], rosters)).toEqual([]);
    expect(pickedOf([], rosters)).toEqual([]);
  });

  test("別台暫時連不上（名冊沒載入）時，選了它的秘書不會被丟掉", () => {
    localStorage.setItem(AGENT_FILTER_KEY, JSON.stringify(["amy-office/a", "x", "b"]));
    reloadAgentFilter();
    const { result } = renderHook(() => useAgentFilter());
    // 一次一位：照台排的第一位（主伺服器的 b）；別台那位存著的 key 沒被丟。
    expect(result.current.picked).toEqual(["b"]);
    expect(stored()).toEqual(["amy-office/a", "x", "b"]);
    expect(
      normalizePicked(
        ["amy-office/z", "amy-office/a"],
        new Map([
          ["", ["a"]],
          ["amy-office", ["amy-office/a"]],
        ]),
      ),
    ).toEqual(["amy-office/a"]);
  });

  test("選中的人名照那台的名冊", () => {
    sites.remote = [{ id: "a", name: "別台的甲", enabled: true }];
    const { result } = renderHook(() => usePickedNames(["b", "amy-office/a"]));
    expect(result.current).toBe("人b、office 的a");
  });

  test("設定頁拿掉一台時，篩選裡只清那台的秘書（主伺服器與別的台留著）", () => {
    localStorage.setItem(
      AGENT_FILTER_KEY,
      JSON.stringify(["a", "amy-office/a", "amy-office/b", "amy-dev/a"]),
    );
    reloadAgentFilter();
    forgetSiteInAgentFilter("amy-office");
    expect(agentFilterStore.get()).toEqual(["a", "amy-dev/a"]);
    expect(stored()).toEqual(["a", "amy-dev/a"]);
    // 主伺服器（""）不清；沒有那台的 key 時不寫。
    forgetSiteInAgentFilter("");
    forgetSiteInAgentFilter("nobody");
    expect(stored()).toEqual(["a", "amy-dev/a"]);
  });
});
