import type { Job } from "@evergenie/sdk-web";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test } from "vitest";
import { JobLastRun } from "./JobLastRun";
import { lastRunView } from "./jobs";

const now = new Date(2026, 10, 2, 15, 30, 0);
const at = (d: number, h: number, m = 0) => new Date(2026, 10, d, h, m).toISOString();
const job = (extra: Partial<Job>): Job => ({
  id: "life/morning",
  name: "早安簡報",
  enabled: true,
  archived: false,
  ...extra,
});

afterEach(cleanup);

describe("排程最近一次的文字", () => {
  test("今天跑過寫「今天 hh:mm 跑過」，之前的寫「上次 …」", () => {
    expect(lastRunView(job({ last_run: at(2, 8), last_result: "ok" }), now)).toEqual({
      text: "今天 08:00 跑過",
      tone: "idle",
    });
    expect(lastRunView(job({ last_run: at(1, 8), last_result: "ok" }), now)?.text).toBe(
      "上次 昨天 08:00 跑過",
    );
    expect(lastRunView(job({ last_run: at(-2, 8), last_result: "ok" }), now)?.text).toBe(
      "上次 10/29 08:00 跑過",
    );
  });

  test("系統級只知道觸發了，一樣寫跑過；還在做、略過各有寫法", () => {
    expect(lastRunView(job({ last_run: at(2, 3, 30), last_result: "fired" }), now)?.text).toBe(
      "今天 03:30 跑過",
    );
    expect(lastRunView(job({ last_run: at(2, 15), last_result: "running" }), now)?.text).toBe(
      "今天 15:00 送出，還在做",
    );
    expect(
      lastRunView(
        job({ last_run: at(2, 8), last_result: "skipped", last_reason: "錯過時間，不補跑" }),
        now,
      ),
    ).toEqual({ text: "今天 08:00 略過：錯過時間，不補跑", tone: "idle" });
  });

  test("今天跑了兩次以上，後面加次數", () => {
    expect(
      lastRunView(job({ last_run: at(2, 14), last_result: "ok", runs_today: 3 }), now)?.text,
    ).toBe("今天 14:00 跑過 · 共 3 次");
  });

  test("顏色照狀態五色：還在做是藍、沒跑成是紅、跑過與略過是灰", () => {
    expect(
      lastRunView(job({ last_run: at(2, 15), last_result: "running", runs_today: 2 }), now)?.tone,
    ).toBe("busy");
  });

  test("從沒跑過、或舊伺服器沒帶欄位：不顯示", () => {
    expect(lastRunView(job({}), now)).toBeNull();
    expect(lastRunView(job({ last_run: at(2, 8) }), now)).toBeNull();
  });
});

describe("排程清單的最近一次那一行", () => {
  test("沒跑成用紅字寫原因", () => {
    render(
      <JobLastRun
        job={job({ last_run: at(2, 8), last_result: "failed", last_reason: "做到一半停了" })}
        now={now}
      />,
    );
    const line = screen.getByTestId("job-last-run");
    expect(line.textContent).toBe("今天 08:00 沒跑成：做到一半停了");
    expect(line.dataset.tone).toBe("trouble");
    expect(line.className).toContain("text-danger");
  });

  test("從沒跑過什麼都不畫", () => {
    const { container } = render(<JobLastRun job={job({})} now={now} />);
    expect(container.innerHTML).toBe("");
  });
});
