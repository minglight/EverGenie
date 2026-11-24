import type { ChatMessage, SessionMessage } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import {
  avatarKeys,
  buildFlow,
  coreAside,
  dropReceived,
  matchQueued,
  mergePending,
  type QueuedMessage,
  routineLabel,
  withoutTools,
  withTimeMarks,
} from "./flow";

const msg = (uuid: string, kind: ChatMessage["kind"], text = "", tool = ""): ChatMessage => ({
  uuid,
  kind,
  text,
  tool,
  is_error: false,
});

describe("對話記錄 → 畫面項目", () => {
  // Guards: REQ-81
  test("連續的工具呼叫收成一項，只留工具名稱；工具的輸出完全不出現", () => {
    const flow = buildFlow(
      [
        msg("1", "user", "幫我看看"),
        msg("2", "tool_call", "", "Read"),
        msg("3", "tool_result", "SECRET-OUTPUT"),
        msg("4", "tool_call", "", "Bash"),
        msg("5", "tool_result", "SECRET-OUTPUT-2"),
        msg("6", "assistant", "看完了"),
        msg("7", "tool_call", "", "Grep"),
      ],
      [],
      "（被中斷）",
    );
    expect(flow.map((i) => i.type)).toEqual(["user", "tools", "assistant", "tools"]);
    expect(flow[1]).toMatchObject({
      count: 2,
      tools: [
        { label: "Read", count: 1 },
        { label: "Bash", count: 1 },
      ],
    });
    expect(flow[3]).toMatchObject({ count: 1, tools: [{ label: "Grep", count: 1 }] });
    expect(JSON.stringify(flow)).not.toContain("SECRET");
  });

  // WEB-125：用了幾十個工具時清單不要一長串——同一種白話合併成一列記次數，順序照第一次出現；總數照實算。
  test("工具清單用引擎給的白話，同一種合併記次數、照第一次出現的順序", () => {
    const call = (uuid: string, tool: string, label: string): ChatMessage => ({
      ...msg(uuid, "tool_call", "", tool),
      tool_label: label,
    });
    const flow = buildFlow(
      [
        call("1", "Read", "讀檔"),
        call("2", "Bash", "跑指令"),
        msg("3", "tool_result", "x"),
        call("4", "Bash", "跑指令"),
        call("5", "Edit", "改檔"),
        call("6", "MultiEdit", "改檔"),
        call("7", "Bash", "跑指令"),
        call("8", "Read", "讀檔"),
      ],
      [],
      "（被中斷）",
    );
    expect(flow).toHaveLength(1);
    expect(flow[0]).toMatchObject({
      type: "tools",
      count: 7,
      tools: [
        { label: "讀檔", count: 2 },
        { label: "跑指令", count: 3 },
        { label: "改檔", count: 2 },
      ],
    });
  });

  // WEB-126：舊資料（升級前的對話記錄、別台舊版引擎）沒有 tool_label，退回工具原名、不會壞；連原名都沒有寫「?」。
  test("沒有白話欄位的舊資料退回工具原名", () => {
    const flow = buildFlow(
      [
        msg("1", "tool_call", "", "Read"),
        { ...msg("2", "tool_call", "", "Read"), tool_label: "讀檔" },
        { ...msg("3", "tool_call", "", "Bash"), tool_label: "  " },
        msg("4", "tool_call", "", ""),
        msg("5", "tool_call", "", "Read"),
      ],
      [],
      "（被中斷）",
    );
    expect(flow[0]).toMatchObject({
      count: 5,
      tools: [
        { label: "Read", count: 2 },
        { label: "讀檔", count: 1 },
        { label: "Bash", count: 1 },
        { label: "?", count: 1 },
      ],
    });
  });

  // Guards: REQ-81
  test("interrupt 變成「（被中斷）」，from_session 放秘書那邊", () => {
    const flow = buildFlow(
      [{ ...msg("1", "user", "接力"), from_session: "life" }, msg("2", "interrupt")],
      [],
      "（被中斷）",
    );
    expect(flow[0]).toMatchObject({ type: "aside", from: "life", label: "接力" });
    expect(flow[1]).toMatchObject({ type: "note", text: "（被中斷）" });
  });

  // Guards: REQ-142
  test("本地的狀態小膠囊排在它的錨點那則訊息之後，後來的訊息接在膠囊後面", () => {
    const flow = buildFlow(
      [msg("1", "user", "好"), msg("2", "assistant", "要執行"), msg("3", "assistant", "做完了")],
      [{ id: "a", anchor: "2", text: "已允許：建立檔案" }],
      "（被中斷）",
    );
    expect(flow.map((i) => (i.type === "note" ? i.text : i.type))).toEqual([
      "user",
      "assistant",
      "已允許：建立檔案",
      "assistant",
    ]);
  });
});

describe("排隊中的泡泡", () => {
  const q = (key: string, text: string, status: QueuedMessage["status"] = "queued") => ({
    key,
    text,
    status,
  });

  // Guards: REQ-142
  test("用 trim() 後完全相等比對；同一段文字送兩次時依序對到第一個還沒對上的", () => {
    const queued = [q("a", "哈囉"), q("b", "哈囉"), q("c", "別的")];
    const afterFirst = matchQueued(queued, "哈囉\n");
    expect(afterFirst.map((x) => x.key)).toEqual(["b", "c"]);
    expect(matchQueued(afterFirst, " 哈囉").map((x) => x.key)).toEqual(["c"]);
    expect(matchQueued(afterFirst, "不同").map((x) => x.key)).toEqual(["b", "c"]);
  });

  // Guards: REQ-142
  test("送出失敗的泡泡不會被別的訊息對掉", () => {
    const queued = [q("a", "哈囉", "failed")];
    expect(matchQueued(queued, "哈囉")).toHaveLength(1);
  });
});

describe("訊息旁的頭像", () => {
  // Guards: REQ-156
  test("秘書連續的訊息只有最後一則放頭像；使用者的話與膠囊不放，換人說話後重新算", () => {
    const flow = buildFlow(
      [
        msg("1", "user", "嗨"),
        msg("2", "assistant", "第一句"),
        msg("3", "tool_call", "", "Read"),
        msg("4", "assistant", "第二句"),
        msg("5", "user", "好"),
        msg("6", "assistant", "收到"),
        msg("7", "tool_call", "", "Bash"),
      ],
      [{ id: "n", anchor: "6", text: "已允許" }],
      "（被中斷）",
    );
    // 6 之後有本地膠囊，把「收到」與後面的工具隔開：兩邊各自是一段。
    expect(flow.map((i) => i.type)).toEqual([
      "user",
      "assistant",
      "tools",
      "assistant",
      "user",
      "assistant",
      "note",
      "tools",
    ]);
    const keys = avatarKeys(flow);
    expect([...keys]).toEqual([3, 5, 7].map((i) => flow[i]?.key));
  });

  // Guards: REQ-156
  test("沒有訊息、只有使用者的話：沒有頭像", () => {
    expect(avatarKeys([]).size).toBe(0);
    expect(avatarKeys(buildFlow([msg("1", "user", "嗨")], [], "").slice()).size).toBe(0);
  });
});

describe("時間標記", () => {
  const at = (h: number, mi: number, d = 24) => new Date(2026, 9, d, h, mi).toISOString();
  const withTs = (uuid: string, kind: ChatMessage["kind"], ts?: string): ChatMessage => ({
    ...msg(uuid, kind, "x", kind === "tool_call" ? "Read" : ""),
    ts,
  });
  const types = (messages: ChatMessage[]) =>
    withTimeMarks(buildFlow(messages, [], "（被中斷）")).map((i) =>
      i.type === "time" ? "time" : i.key,
    );

  // Guards: REQ-157
  test("TestTimeMarksGapAndDay：第一則、相隔 10 分鐘以上、跨日才標；9 分鐘與沒有 ts 的不標", () => {
    expect(
      types([
        withTs("a", "user", at(9, 0)),
        withTs("b", "assistant", at(9, 9)),
        withTs("c", "user", at(9, 19)),
        withTs("d", "assistant", at(9, 20)),
      ]),
    ).toEqual(["time", "a", "b", "time", "c", "d"]);
    // 跨日：只差 5 分鐘也標。
    expect(
      types([withTs("a", "user", at(23, 58)), withTs("b", "assistant", at(0, 3, 25))]),
    ).toEqual(["time", "a", "time", "b"]);
    // 沒有 ts 的不加也不算：第一個有 ts 的才標，且拿它的前一個有 ts 的項目比。
    expect(
      types([
        withTs("a", "user"),
        withTs("b", "assistant", at(9, 0)),
        withTs("c", "user"),
        withTs("d", "assistant", at(9, 5)),
      ]),
    ).toEqual(["a", "time", "b", "c", "d"]);
    // 工具串取第一個 tool_call 的時間。
    expect(
      types([
        withTs("a", "user", at(9, 0)),
        withTs("t1", "tool_call", at(9, 30)),
        withTs("t2", "tool_call", at(9, 31)),
      ]),
    ).toEqual(["time", "a", "time", "tools-t1"]);
    // 本地膠囊沒有 ts，不影響前後。
    const flow = withTimeMarks([
      { type: "user", key: "a", text: "x", ts: at(9, 0) },
      { type: "note", key: "n", text: "已允許" },
      { type: "assistant", key: "b", text: "y", ts: at(9, 2) },
    ]);
    expect(flow.map((i) => i.type)).toEqual(["time", "user", "note", "assistant"]);
  });

  test("時間往回跳的項目不加標記、不把基準拉回去（下面不會出現比上面早的時間）", () => {
    const flow = withTimeMarks([
      { type: "user", key: "a", text: "x", ts: at(9, 0) },
      { type: "assistant", key: "b", text: "y", ts: at(10, 0) },
      { type: "user", key: "c", text: "補送的", ts: at(9, 20) },
      { type: "assistant", key: "d", text: "z", ts: at(10, 5) },
      { type: "assistant", key: "e", text: "w", ts: at(10, 30) },
    ]);
    expect(flow.map((i) => i.key)).toEqual(["time-a", "a", "time-b", "b", "c", "d", "time-e", "e"]);
  });
});

const sm = (id: string, lifecycle: string, extra: Partial<SessionMessage> = {}): SessionMessage =>
  ({ id, from: "owner(web)", body: `話 ${id}`, lifecycle, ...extra }) as SessionMessage;

describe("退出再進來：伺服器記得的話畫回泡泡", () => {
  // Guards: REQ-302
  test("這次畫面沒有、還在排隊／等她讀到／送失敗的主人的話補成泡泡；她收到之後的不畫", () => {
    const out = mergePending(
      [],
      [
        sm("a", "queued"),
        sm("b", "notified", { interject: true }),
        sm("c", "failed", { reason: "貼不進她的對話" }),
        sm("d", "working"),
        sm("e", "queued", { from: "system" }),
      ],
    );
    expect(out.map((q) => [q.id, q.status, q.interject ?? false, q.fromServer])).toEqual([
      ["a", "sent", false, true],
      ["b", "sent", true, true],
      ["c", "failed", false, true],
    ]);
    expect(out[2]?.error).toBe("貼不進她的對話");
  });

  // Guards: REQ-302, REQ-301
  test("這次送的跟著伺服器改：送失敗、中途補話；不重複畫", () => {
    const local: QueuedMessage[] = [
      { key: "q-1", text: "話 a", status: "sent", id: "a" },
      { key: "q-2", text: "話 b", status: "sent", id: "b" },
    ];
    const out = mergePending(local, [
      sm("a", "failed", { reason: "沒送到" }),
      sm("b", "notified", { interject: true }),
    ]);
    expect(out).toHaveLength(2);
    expect(out[0]).toMatchObject({ key: "q-1", status: "failed", error: "沒送到" });
    expect(out[1]).toMatchObject({ key: "q-2", status: "sent", interject: true });
  });
});

describe("沒送到與這一輪沒做完", () => {
  test("這一輪沒做完的那句（interrupted）補成 interrupted 泡泡、帶原因，跟沒送到分開；對話記錄有同一句也不被對掉", () => {
    const why = "AI 平台暫時忙不過來";
    const out = mergePending(
      [{ key: "q-1", text: "話 a", status: "sent", id: "a" }],
      [
        sm("a", "interrupted", { reason: "中斷" }),
        sm("b", "interrupted", {
          reason: why,
          reason_detail: "Overloaded",
          reason_kind: "overloaded",
        }),
        sm("c", "failed", { reason: "貼不進她的對話" }),
        sm("d", "replied"),
      ],
    );
    expect(out.map((q) => [q.id, q.status, q.error])).toEqual([
      ["a", "interrupted", "中斷"],
      ["b", "interrupted", why],
      ["c", "failed", "貼不進她的對話"],
    ]);
    // 平台原話與錯誤種類跟著帶（原話收在「詳細」、種類決定給不給「再說一次」，DEV-156）。
    expect(out.map((q) => [q.detail, q.kind])).toEqual([
      [undefined, undefined],
      ["Overloaded", "overloaded"],
      [undefined, undefined],
    ]);
    expect(matchQueued(out, "話 b").map((q) => q.id)).toEqual(["a", "b", "c"]);
  });

  test("舊版那台送來的 lost 旗標不認得：做完的（replied）不畫泡泡，不畫成紅字", () => {
    const old = { ...sm("b", "replied"), lost: true } as SessionMessage;
    expect(mergePending([], [old])).toEqual([]);
  });
});

describe("送出後 3 秒規則：原因跟著伺服器", () => {
  // Guards: REQ-384, REQ-385
  test("這次送的與伺服器記得的都帶伺服器給的原因；原因沒了（她收到前）就拿掉", () => {
    const local: QueuedMessage[] = [{ key: "q-1", text: "話 a", status: "sent", id: "a" }];
    expect(mergePending(local, [sm("a", "queued", { wait: "answer" })])[0]?.wait).toBe("answer");
    expect(mergePending(local, [sm("a", "queued", { wait: "retrying" })])[0]?.wait).toBe(
      "retrying",
    );
    expect(mergePending(local, [sm("a", "notified")])[0]?.wait).toBeUndefined();
    const server = mergePending([], [sm("b", "queued", { wait: "waking" })]);
    expect(server[0]).toMatchObject({ id: "b", status: "sent", wait: "waking", fromServer: true });
  });
});

// WEB-90：叫她停下時送進去的那句，核心記「這句還沒開始，可以重送」，平台卻接著做了——對話記錄裡有同一句，不要再畫一次紅字。
describe("dropReceived", () => {
  const at = (iso: string, text: string, extra: Partial<ChatMessage> = {}): ChatMessage => ({
    ...msg(`u-${iso}`, "user", text),
    ts: iso,
    ...extra,
  });
  const failed = (text: string, createdAt?: string, sentAt?: number): QueuedMessage => ({
    key: `k-${text}`,
    text,
    status: "failed",
    error: "這句還沒開始，可以重送",
    fromServer: true,
    createdAt,
    sentAt,
  });

  test("送失敗的那句之後在對話記錄裡出現了，就不再畫沒送到", () => {
    const pending = [failed("補一句", "2026-11-03T01:00:00Z")];
    expect(dropReceived(pending, [at("2026-11-03T01:00:05Z", "補一句 ")])).toEqual([]);
  });

  test("對話記錄裡同一句是送出之前的、別台秘書傳的、內容不同，都照舊畫", () => {
    const pending = [failed("補一句", "2026-11-03T01:00:00Z")];
    expect(dropReceived(pending, [at("2026-11-03T00:59:00Z", "補一句")])).toHaveLength(1);
    expect(
      dropReceived(pending, [at("2026-11-03T01:00:05Z", "補一句", { from_session: "life" })]),
    ).toHaveLength(1);
    expect(dropReceived(pending, [at("2026-11-03T01:00:05Z", "別的")])).toHaveLength(1);
  });

  test("沒有伺服器時間時用這次畫面送出的時間；都沒有就照舊畫", () => {
    const sent = Date.parse("2026-11-03T01:00:00Z");
    const msgs = [at("2026-11-03T01:00:05Z", "補一句")];
    expect(dropReceived([failed("補一句", undefined, sent)], msgs)).toEqual([]);
    expect(dropReceived([failed("補一句")], msgs)).toHaveLength(1);
  });

  test("這一輪沒做完、還在等的泡泡不受影響", () => {
    const msgs = [at("2026-11-03T01:00:05Z", "補一句")];
    const interrupted: QueuedMessage = {
      ...failed("補一句", "2026-11-03T01:00:00Z"),
      status: "interrupted",
    };
    const sent: QueuedMessage = { ...failed("補一句", "2026-11-03T01:00:00Z"), status: "sent" };
    expect(dropReceived([interrupted, sent], msgs)).toHaveLength(2);
  });

  // WEB-119：她收到了才出錯，對話記錄裡已經有那句——「這一輪沒做完」照舊留著，但標 inLog，畫面不再畫第二次。
  test("這一輪沒做完的那句已在對話記錄裡：留著但標 inLog；送出之前的同一句不算", () => {
    const interrupted: QueuedMessage = {
      ...failed("壞掉", "2026-11-03T01:00:00Z"),
      status: "interrupted",
    };
    expect(dropReceived([interrupted], [at("2026-11-03T01:00:01Z", "壞掉")])).toEqual([
      { ...interrupted, inLog: true },
    ]);
    expect(dropReceived([interrupted], [at("2026-11-03T00:59:00Z", "壞掉")])).toEqual([
      interrupted,
    ]);
  });

  test("mergePending 把伺服器的送出時間帶到失敗的泡泡上", () => {
    const server: SessionMessage[] = [
      {
        id: "x",
        from: "owner(web)",
        body: "補一句",
        lifecycle: "failed",
        reason: "這句還沒開始，可以重送",
        created_at: "2026-11-03T01:00:00Z",
      },
    ];
    expect(mergePending([], server)[0]?.createdAt).toBe("2026-11-03T01:00:00Z");
    const local: QueuedMessage[] = [{ key: "q", id: "x", text: "補一句", status: "sent" }];
    expect(mergePending(local, server)[0]).toMatchObject({
      status: "failed",
      createdAt: "2026-11-03T01:00:00Z",
    });
  });
});

describe("排程送進來的指令", () => {
  // 排程的整段指令不畫成主人的泡泡，只留秘書那邊一小排「排程：<標題>」（WEB-156、WEB-158）。
  test("有 # 標題用標題，沒有用排程代號；別的秘書傳的、一般的話照舊", () => {
    const body = "［排程 art/nightly］# 夜間整理（每天凌晨）\n\n這是系統的整理工作";
    expect(routineLabel(body)).toBe("夜間整理（每天凌晨）");
    expect(routineLabel("［排程 work/daily-report］寫今天的報告")).toBe("work/daily-report");
    expect(routineLabel("幫我看一下［排程 x］")).toBeNull();
    const flow = buildFlow(
      [
        msg("1", "user", body),
        { ...msg("2", "user", "［排程 a］轉傳"), from_session: "code" },
        msg("3", "assistant", "好"),
      ],
      [],
      "",
      (label) => `排程：${label}`,
    );
    expect(flow.map((i) => [i.type, i.type === "aside" ? i.label : ""])).toEqual([
      ["aside", "排程：夜間整理（每天凌晨）"],
      ["aside", "［排程 a］轉傳"],
      ["assistant", ""],
    ]);
    expect(flow[0]).not.toHaveProperty("detail");
  });
});

describe("不是主人說的話放秘書那邊", () => {
  const flowOf = (text: string) => buildFlow([msg("1", "user", text)], [], "");

  test("平台通知變一小排，放頭像、算時間，不像主人的話", () => {
    const flow = buildFlow(
      [
        { ...msg("1", "notice", "額度恢復了"), ts: "2026-11-04T01:00:00Z" },
        msg("2", "assistant", "好"),
      ],
      [],
      "",
    );
    expect(flow[0]).toMatchObject({ type: "aside", label: "額度恢復了" });
    expect(flow[0]).not.toHaveProperty("detail");
    expect(buildFlow([msg("1", "notice", "自動喚醒：檢查\n第二行")], [], "")[0]).toMatchObject({
      label: "自動喚醒：檢查",
      detail: "自動喚醒：檢查\n第二行",
    });
    expect([...avatarKeys(buildFlow([msg("1", "notice", "x")], [], ""))]).toEqual(["1"]);
    expect(withTimeMarks(flow)[0]?.type).toBe("time");
  });

  test("核心轉來的話去掉外框，太長的才留全文", () => {
    expect(coreAside("［派送通知］Athena 沒有回覆")).toEqual({
      label: "派送通知：Athena 沒有回覆",
    });
    expect(coreAside("［核心］第一行\n第二行")).toEqual({
      label: "核心：第一行",
      detail: "第一行\n第二行",
    });
    expect(coreAside("［雅典娜（code）回覆你］做好了")).toEqual({ label: "雅典娜：做好了" });
    expect(coreAside("［輝夜（admin）］幫我看")).toEqual({ label: "輝夜：幫我看" });
    // 舊對話的外框、其他核心寫的話
    expect(coreAside("［輝夜（admin）傳話］幫我看")?.label).toBe("輝夜：幫我看");
    expect(coreAside("［核心傳話］醒醒")?.label).toBe("核心：醒醒");
    expect(coreAside("［待辦 R-1］查一下")?.label).toBe("待辦 R-1：查一下");
    expect(coreAside("［單 DEV-3 回到進行中，請從上次的地方接著做］")?.label).toBe(
      "單 DEV-3 回到進行中，請從上次的地方接著做",
    );
    expect(coreAside("［GOAL-3］主人開了一個 Goal，指定你負責")?.label).toBe(
      "GOAL-3：主人開了一個 Goal，指定你負責",
    );
    // 圖片一小排寫「（圖片）」，全文留著
    expect(coreAside("［雅典娜（code）］![圖](/share/x.png)")).toEqual({
      label: "雅典娜：（圖片）",
      detail: "![圖](/share/x.png)",
    });
    const long = "長".repeat(15);
    expect(coreAside(`［派送通知］${long}`)?.detail).toBe(long);
    expect(flowOf("［雅典娜（code）回覆你］做好了")[0]).toMatchObject({ type: "aside" });
  });

  test("主人自己說的話、回答問題照舊是主人的泡泡", () => {
    for (const text of [
      "DEV-1：看板壞了",
      "［主人回答 Q-1］好",
      "幫我看［派送通知］這段",
      "［備註］明天",
      "［備註（明天）］買牛奶",
      "［DEV-3］看一下",
    ]) {
      expect(flowOf(text)[0]).toMatchObject({ type: "user", text });
    }
  });
});

describe("預設模式拿掉工具膠囊", () => {
  test("只拿掉工具，前後的話留著、順序不變", () => {
    const items = buildFlow(
      [
        { uuid: "a", kind: "assistant", text: "我查一下" },
        { uuid: "b", kind: "tool_call", tool: "Bash" },
        { uuid: "c", kind: "tool_result" },
        { uuid: "d", kind: "assistant", text: "查好了" },
      ] as ChatMessage[],
      [],
      "中斷",
    );
    expect(items.map((i) => i.type)).toEqual(["assistant", "tools", "assistant"]);
    expect(withoutTools(items).map((i) => i.type)).toEqual(["assistant", "assistant"]);
  });

  test("載入技能那一小排也拿掉，額度這類你該知道的通知留著", () => {
    const items = buildFlow(
      [
        { uuid: "a", kind: "notice", text: "載入技能 create-feature" },
        { uuid: "b", kind: "notice", text: "額度快用完了" },
      ] as ChatMessage[],
      [],
      "中斷",
    );
    expect(items).toHaveLength(2);
    const kept = withoutTools(items);
    expect(kept).toHaveLength(1);
    expect(kept[0]).toMatchObject({ type: "aside", label: "額度快用完了" });
  });
});
