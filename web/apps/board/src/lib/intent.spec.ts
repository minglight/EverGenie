import { describe, expect, test } from "vitest";
import { type AgentRef, matchAgent, parseIntent } from "./intent";

// 沒有裝 @types/node（FEN-1 的套件清單裡沒有）：只宣告這一個用到的全域。
declare const process: { env: Record<string, string | undefined> };
process.env.TZ = "Asia/Taipei";
const now = new Date(2026, 9, 24, 15, 30, 0);
const agents: AgentRef[] = [
  { id: "life", name: "維納斯" },
  { id: "code", name: "Athena" },
  { id: "work", name: "阿媞米斯" },
];

describe("parseIntent", () => {
  // Guards: REQ-59
  test("一般的句子是新增待辦：時間從內容拿掉", () => {
    const intent = parseIntent("明天 9 點 打給牙醫", agents, { now });
    expect(intent).toMatchObject({ kind: "todo", content: "打給牙醫", matched: "明天 9 點" });
    if (intent.kind === "todo")
      expect(intent.at?.getTime()).toBe(new Date(2026, 9, 25, 9).getTime());
  });

  // Guards: REQ-59
  test("沒有時間：at 是 null，內容是整句", () => {
    expect(parseIntent("買牛奶", agents, { now })).toEqual({
      kind: "todo",
      content: "買牛奶",
      at: null,
      matched: null,
    });
  });

  // Guards: REQ-59
  test("按了時間膠囊的 ×：不再抓時間，整句都是內容", () => {
    expect(parseIntent("明天 9 點 打給牙醫", agents, { now, ignoreWhen: true })).toEqual({
      kind: "todo",
      content: "明天 9 點 打給牙醫",
      at: null,
      matched: null,
    });
  });

  // RMD-22：開頭「交代」是交代任務（開任務單），不是待辦；「交代」拿掉、不抓時間。
  test("「交代 整理照片」是交代任務", () => {
    expect(parseIntent("交代 整理照片", [])).toMatchObject({
      kind: "todo",
      task: true,
      content: "整理照片",
      at: null,
    });
    expect(parseIntent("交代：明天 整理照片", [])).toMatchObject({
      task: true,
      content: "明天 整理照片",
      at: null,
    });
    expect(parseIntent("交代事情的方法", [])).not.toHaveProperty("task");
  });

  // Guards: REQ-59
  test("空白是 empty", () => {
    expect(parseIntent("   ", agents)).toEqual({ kind: "empty" });
  });

  // Guards: REQ-142
  test("@ 開頭是傳話：名字對人名或職位 id，不分大小寫，前綴也算", () => {
    expect(parseIntent("@athena 進度？", agents)).toMatchObject({
      kind: "say",
      agent: { id: "code" },
      body: "進度？",
      named: true,
    });
    expect(parseIntent("@維 訂 4 位", agents)).toMatchObject({
      agent: { id: "life" },
      body: "訂 4 位",
    });
    expect(parseIntent("@WORK 週報", agents)).toMatchObject({ agent: { id: "work" } });
    expect(parseIntent("＠life 嗨", agents)).toMatchObject({ agent: { id: "life" } });
  });

  // Guards: REQ-142
  test("名字還在打：列出符合的秘書；打 @ 就列全部；找不到沒有建議", () => {
    const all = parseIntent("@", agents);
    expect(all).toMatchObject({ kind: "say", agent: null, named: false });
    if (all.kind === "say") expect(all.suggestions).toHaveLength(3);
    const some = parseIntent("@a", agents);
    if (some.kind === "say") expect(some.suggestions.map((a) => a.id)).toEqual(["code"]);
    const none = parseIntent("@沒這個人 你好", agents);
    expect(none).toMatchObject({ kind: "say", agent: null, named: true, suggestions: [] });
  });

  // Guards: REQ-142
  test("傳話的內容不會被當成時間", () => {
    expect(parseIntent("@維納斯 明天 9 點 訂位", agents, { now })).toMatchObject({
      kind: "say",
      body: "明天 9 點 訂位",
    });
  });
});

describe("matchAgent", () => {
  // Guards: REQ-142
  test("完全相同優先於前綴", () => {
    const list: AgentRef[] = [
      { id: "co", name: "Coco" },
      { id: "code", name: "Athena" },
    ];
    expect(matchAgent("co", list)?.id).toBe("co");
    expect(matchAgent("", list)).toBeNull();
  });
});

// 看板連多台：別台秘書的 id 是「台名/職位 id」。
describe("matchAgent 別台的秘書", () => {
  const list: AgentRef[] = [
    { id: "code", name: "Athena" },
    { id: "amy-office/code", name: "Athena" },
    { id: "amy-office/work", name: "阿媞米斯" },
  ];

  test("打台名的字頭不會對到那台隨便一位，也不列建議", () => {
    expect(matchAgent("am", list)).toBeNull();
    expect(matchAgent("amy-office/", list)).toBeNull();
    expect(parseIntent("@am", list)).toMatchObject({ kind: "say", suggestions: [] });
  });

  test("「台名/職位 id」整串打完才對到那台那位；職位 id 的字頭照樣算", () => {
    expect(matchAgent("amy-office/code", list)?.id).toBe("amy-office/code");
    expect(matchAgent("wo", list)?.id).toBe("amy-office/work");
    expect(matchAgent("Athena", list)?.id).toBe("code");
  });
});
