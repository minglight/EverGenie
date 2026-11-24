import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { DoctorFull, sortChecks } from "./DoctorFull";

const state = vi.hoisted(() => ({ doctor: vi.fn(), mode: "simple" }));

vi.mock("@evergenie/sdk-web", () => ({ api: { system: { doctor: state.doctor } } }));
vi.mock("../../data/queries", () => ({ useShowCodes: () => state.mode === "full" }));

const checks = [
  { id: "config.version", group: "config", ok: true, severity: "ok", detail: "核心版本：1.2.3" },
  { id: "push.keys", group: "push", ok: false, severity: "warn", detail: "還沒有裝置開啟推播" },
  {
    id: "sessions.resident",
    group: "sessions",
    ok: false,
    severity: "error",
    detail: "生活秘書沒有在跑",
    fix: "evergenie session open life",
  },
  { id: "data.ok", group: "data", ok: true, severity: "ok", detail: "資料格式都正確" },
];

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <DoctorFull />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.doctor.mockReset();
  state.doctor.mockResolvedValue({ checks });
  state.mode = "simple";
});
afterEach(cleanup);

describe("完整檢查", () => {
  test("TestSortChecksProblemsFirst", () => {
    expect(sortChecks(checks as never).map((c) => c.id)).toEqual([
      "sessions.resident",
      "push.keys",
      "config.version",
      "data.ok",
    ]);
  });

  // 按了才跑；有問題的排前面、寫出是哪一類；正常的收成一行，要看再展開；簡單模式不顯示指令。
  test("TestDoctorFullListsProblemsFirstAndFoldsOk", async () => {
    mount();
    expect(state.doctor).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("doctor-run"));
    const summary = await screen.findByTestId("doctor-summary");
    expect(summary.textContent).toBe("有 2 項要看一下，其他 2 項正常");
    let rows = screen.getAllByTestId("doctor-check");
    expect(rows.map((r) => r.dataset.severity)).toEqual(["error", "warn"]);
    expect(rows[0]?.textContent).toContain("秘書的對話・要處理");
    expect(rows[0]?.textContent).toContain("生活秘書沒有在跑");
    expect(rows[0]?.textContent).not.toContain("evergenie session open");
    expect(screen.getByTestId("doctor-result").textContent).toContain("跟任何一位秘書說");

    fireEvent.click(screen.getByTestId("doctor-ok-toggle"));
    rows = screen.getAllByTestId("doctor-check");
    expect(rows.map((r) => r.dataset.severity)).toEqual(["error", "warn", "ok", "ok"]);
    expect(within(rows[2] as HTMLElement).getByText("核心版本：1.2.3")).toBeTruthy();
  });

  test("TestDoctorFullShowsFixInFullMode", async () => {
    state.mode = "full";
    mount();
    fireEvent.click(screen.getByTestId("doctor-run"));
    await screen.findByTestId("doctor-summary");
    expect(screen.getAllByTestId("doctor-check")[0]?.textContent).toContain(
      "下一步：evergenie session open life",
    );
  });

  test("TestDoctorFullAllOk", async () => {
    state.doctor.mockResolvedValue({ checks: checks.filter((c) => c.ok) });
    mount();
    fireEvent.click(screen.getByTestId("doctor-run"));
    expect((await screen.findByTestId("doctor-summary")).textContent).toBe("2 項都正常");
    expect(screen.queryAllByTestId("doctor-check")).toHaveLength(0);
  });
});
