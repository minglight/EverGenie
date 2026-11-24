import { describe, expect, test } from "vitest";
import { followStep, followTarget } from "./filterFollow";

describe("篩選換人時對話要去哪一串（WEB-19）", () => {
  test("沒選人、或已經是她：不動", () => {
    expect(followTarget([], "code")).toBeNull();
    expect(followTarget(["code", "office/code"], "office/code")).toBeNull();
  });

  test("她在這一台有：留在這台", () => {
    expect(followTarget(["life", "office/life"], "office/code")).toEqual({
      session: "life",
      site: "office",
    });
  });

  test("這一台沒有她：到她第一台", () => {
    expect(followTarget(["office/work"], "code")).toEqual({ session: "work", site: "office" });
  });
});

describe("對話與篩選誰跟誰（WEB-19）", () => {
  const base = {
    picked: ["life"],
    current: "code",
    userPicked: false,
    currentChanged: false,
    aligned: true,
  };

  test("這個分頁在篩選換人：對話跟著換", () => {
    expect(followStep({ ...base, userPicked: true })).toBe("navigate");
  });

  test("剛打開別位的對話：篩選換成她", () => {
    expect(followStep({ ...base, aligned: false })).toBe("pick");
    expect(followStep({ ...base, currentChanged: true })).toBe("pick");
  });

  test("別的分頁改了篩選（不是這個分頁點的）：這個分頁的對話不被拉走、篩選也不改回來", () => {
    expect(followStep(base)).toBe("none");
  });

  test("篩選是全部、已經是她、秘書還對不上：不動", () => {
    expect(followStep({ ...base, picked: [], aligned: false })).toBe("none");
    expect(followStep({ ...base, picked: ["code", "office/code"], userPicked: true })).toBe("none");
    expect(followStep({ ...base, current: "", userPicked: true, aligned: false })).toBe("none");
  });
});
