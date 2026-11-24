import { ApiError, type PolicyRequest } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import { decidedToastKey, failedToast, isNarrower, newestFirst } from "./policyText";

describe("權限申請頁", () => {
  // POL-15：送出失敗後卡片可能整張離開（已在別處決定），原因要放在留 10 秒的 toast，寫伺服器給的那句。
  test("送出失敗的 toast 寫伺服器的原因、留 10 秒", () => {
    const out = failedToast(new ApiError("conflict", "這張申請已經是「已拒絕」，不能再審"));
    expect(out.text).toBe("沒送出去：這張申請已經是「已拒絕」，不能再審");
    expect(out.durationMs).toBe(10000);
    expect(failedToast(new Error("連不上")).text).toContain("連不上");
  });

  // Guards: REQ-169
  test("改後核准只能改窄：原範圍本身或底下的資料夾", () => {
    expect(isNarrower("~/Workspace/amy/evergenie", "~/Workspace/amy")).toBe(true);
    expect(isNarrower("~/Workspace/amy/", "~/Workspace/amy")).toBe(true);
    expect(isNarrower("~/Workspace/evergenie", "~/Workspace/amy")).toBe(false);
    expect(isNarrower("~/Documents", "~/Workspace")).toBe(false);
    expect(isNarrower("~/Documents", "")).toBe(true);
    expect(isNarrower("", "~/Workspace")).toBe(true);
  });

  // POL-14：核准、拒絕、撤銷後卡片會消失，要有一句結果；套用失敗講明。
  test("按完照伺服器回的狀態說結果，沒回狀態就不提示", () => {
    const q = (status?: PolicyRequest["status"]) =>
      ({ id: "P-1", status, broad: false, permanent: true }) as PolicyRequest;
    expect(decidedToastKey(q("applied"))).toBe("policy.toast.applied");
    expect(decidedToastKey(q("rejected"))).toBe("policy.toast.rejected");
    expect(decidedToastKey(q("failed"))).toBe("policy.toast.failed");
    expect(decidedToastKey(q("revoked"))).toBe("policy.toast.revoked");
    expect(decidedToastKey(q(undefined))).toBeNull();
    expect(decidedToastKey(undefined)).toBeNull();
  });

  // Guards: REQ-169
  test("清單新的在前", () => {
    const list = [
      { id: "P-1", created_at: "2026-10-01T00:00:00Z", broad: false, permanent: false },
      { id: "P-2", created_at: "2026-10-02T00:00:00Z", broad: false, permanent: false },
    ];
    expect(newestFirst(list).map((q) => q.id)).toEqual(["P-2", "P-1"]);
  });
});
