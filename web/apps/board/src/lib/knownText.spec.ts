import { describe, expect, test } from "vitest";
import { t } from "../i18n/zh-TW";
import { knownText } from "./knownText";
import { statusKey } from "./taskText";

describe("knownText：狀態字", () => {
  test("認得的狀態照表翻成白話", () => {
    expect(knownText(statusKey, "doing")).toBe(t("task.status.doing"));
  });

  test("別台回了看板不認得的狀態：照原字顯示，不留空白、不當成 Object 的屬性", () => {
    expect(knownText(statusKey, "stuck")).toBe("stuck");
    expect(knownText(statusKey, "toString")).toBe("toString");
  });
});
