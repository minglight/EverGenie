import { describe, expect, test } from "vitest";
import {
  checkNewId,
  promoteFields,
  promotePreview,
  promotePreviewLines,
  suggestNewId,
  T,
  toPromoteRequest,
} from "./promote";

// 升成官方（CMI-16 的流程，DEV-160 審查 S1 搬到「修改」分頁）：從 closet-mine 的頁面原樣搬過來的純函式。
describe("升成官方", () => {
  test("新編號的建議值與檢查", () => {
    expect(suggestNewId("my-12")).toBe("outfit-12");
    expect(suggestNewId("my-seaside")).toBe("seaside");
    expect(suggestNewId("seaside")).toBe("seaside");
    for (const id of ["my-12", "outfit-12", "seaside"])
      expect(checkNewId(suggestNewId(id))).toBe("");
    expect(checkNewId("seaside-2")).toBe("");
    for (const bad of ["", "my-1", "base", "Sea", "海邊", "-a", "a b"])
      expect(checkNewId(bad)).toBe(T.badId);
    // 編號不照名字猜隱藏版（DEV-160）：照片全是隱藏版的不能升由程式照照片紀錄擋。
    expect(checkNewId("nsfw-1")).toBe("");
  });

  test("請求欄位：不帶上傳檔；預演才寫 dry_run", () => {
    expect(promoteFields("athena-my-12", " seaside ", true)).toEqual({
      type: "promote",
      outfit: "athena-my-12",
      new_id: "seaside",
      dry_run: true,
      state: "pending",
    });
    expect(promoteFields("athena-sea", "", false)).toEqual({
      type: "promote",
      outfit: "athena-sea",
      state: "pending",
    });
  });

  test("預演結果說成白話：變成什麼編號、搬幾個檔、舊的怎麼處理", () => {
    const detail = [
      "預演（什麼都沒動）：會把 athena-my-12 升成官方 athena-seaside",
      "  personas/mine/avatars/athena@my-12.png → personas/avatars/athena@my-12.png（舊檔改名 athena@my-12.png.bak-20261114-promote）",
      "  personas/mine/avatars/full/athena@my-12.png → personas/avatars/full/athena@my-12.png（舊檔改名 athena@my-12.png.bak-20261114-promote）",
      "  輪播設定 closet.rotation/athena 換成新的 id",
      "  照片紀錄 2 筆改成官方的（closet.photo，檔名不改）",
      "  注意：closet.photo/athena-seaside--half1-h 是自己的隱藏版的照片，不升，圖留在 personas/mine/（紀錄跟著換到新編號）",
      "要退回：",
      "  把 a → b 改回來",
    ].join("\n");
    const req = toPromoteRequest({
      id: "U-9",
      fields: { state: "done", result: "athena-seaside", detail },
    });
    expect(req).toMatchObject({ id: "U-9", state: "done", result: "athena-seaside" });
    expect(req.detail).toBe(detail);
    const p = promotePreview("athena-my-12", req.result, req.detail);
    expect(p).toEqual({ to: "athena-seaside", files: 2, rotations: 1, hidden: 1, renamed: true });
    expect(promotePreviewLines(p)).toEqual([
      "會變成官方的衣服，編號 athena-seaside。",
      "會把 2 個檔（照片、全身照、動畫）複製到官方的位置。",
      T.oldRenamed,
      T.rotations(1),
      T.hidden(1),
    ]);
    // 編號沒換、也沒有檔要搬
    const same = promotePreview(
      "athena-sea",
      "athena-sea",
      "預演（什麼都沒動）：會把 athena-sea 升成官方 athena-sea\n要退回：",
    );
    expect(promotePreviewLines(same)).toEqual([T.willBe("athena-sea"), T.files(0), T.oldKept]);
    // 被拒的請求照實帶原因；沒有欄位的當空的。
    expect(
      toPromoteRequest({ id: "U-1", fields: { state: "rejected", reason: " 不行 " } }),
    ).toEqual({ id: "U-1", state: "rejected", reason: "不行", result: "", detail: "" });
    expect(toPromoteRequest({})).toEqual({
      id: "",
      state: "",
      reason: "",
      result: "",
      detail: "",
    });
  });
});
