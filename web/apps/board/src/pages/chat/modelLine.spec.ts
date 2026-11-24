import { describe, expect, test } from "vitest";
import { modelLine } from "./modelLine";

describe("討論串頂端的模型與思考力度（WEB-84）", () => {
  test("引擎給了名字就用名字，力度接在後面", () => {
    expect(modelLine({ model: "vendor-x-9", model_label: "X 9", effort: "medium" })?.text).toBe(
      "X 9 · 思考 medium",
    );
  });

  test("引擎沒給名字就顯示模型原字", () => {
    expect(modelLine({ model: "x-big", effort: "xhigh" })?.text).toBe("x-big · 思考 extra");
  });

  test("只知道一邊就只畫那一邊", () => {
    expect(modelLine({ model: "x-small" })?.text).toBe("x-small");
    expect(modelLine({ effort: "max" })?.text).toBe("思考 max");
  });

  test("都不知道就不畫（不猜一個預設）", () => {
    expect(modelLine({})).toBeNull();
    expect(modelLine({ model: " ", model_label: "", effort: "" })).toBeNull();
    expect(modelLine(null)).toBeNull();
    expect(modelLine(undefined)).toBeNull();
  });

  test("說明帶原始代號，並說點了可以換", () => {
    const line = modelLine({ model: "vendor-x-9", model_label: "X 9", effort: "high" });
    expect(line?.title).toContain("X 9（vendor-x-9）");
    expect(line?.title).toContain("思考力度：high");
    expect(line?.title).toContain("/model");
  });
});
