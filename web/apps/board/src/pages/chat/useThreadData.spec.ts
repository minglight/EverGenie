import { describe, expect, it } from "vitest";
import { type ChatState, reduce } from "./useThreadData";

const empty: ChatState = { messages: [], queued: [], draft: "" };

describe("還在打的那段（DEV-150）", () => {
  it("draft 只換掉淡色那段、不進訊息流；空字串收掉", () => {
    let s = reduce(empty, {
      type: "message",
      message: { kind: "assistant", text: "收到，我先", draft: true, is_error: false },
    });
    expect(s.draft).toBe("收到，我先");
    expect(s.messages).toHaveLength(0);
    s = reduce(s, {
      type: "message",
      message: { kind: "assistant", text: "", draft: true, is_error: false },
    });
    expect(s.draft).toBe("");
  });
  it("正式的秘書或主人訊息到了就收掉，提示類訊息不收", () => {
    const d = reduce(empty, {
      type: "message",
      message: { kind: "assistant", text: "收到", draft: true, is_error: false },
    });
    expect(
      reduce(d, {
        type: "message",
        message: { uuid: "n1", kind: "notice", text: "提示", is_error: false },
      }).draft,
    ).toBe("收到");
    expect(
      reduce(d, {
        type: "message",
        message: { uuid: "a1", kind: "assistant", text: "收到，我先記下來。", is_error: false },
      }).draft,
    ).toBe("");
    expect(
      reduce(d, {
        type: "message",
        message: { uuid: "u1", kind: "user", text: "好", is_error: false },
      }).draft,
    ).toBe("");
  });
  it("重連清空時也收掉", () => {
    const d = reduce(empty, {
      type: "message",
      message: { kind: "assistant", text: "收到", draft: true, is_error: false },
    });
    expect(reduce(d, { type: "reset" }).draft).toBe("");
  });
});
