import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { createTask } from "../data/taskCreate";
import { QuickInput } from "./QuickInput";

const state = vi.hoisted(() => {
  const primary = { key: "", base: "", primary: true, label: "", state: "ok" };
  const office = {
    key: "amy-office",
    base: "https://office.example",
    primary: false,
    label: "amy-office",
    state: "ok",
  };
  const roster = (site: typeof primary, name: string) => ({
    site,
    list: [{ id: "life", name, enabled: true }],
    view: (id: string) => ({ id, name }),
  });
  return {
    upload: vi.fn(),
    say: vi.fn(),
    setAgent: vi.fn(),
    todo: vi.fn(),
    primary,
    office,
    roster,
    agents: [roster(primary, "維納斯")],
    multi: false,
  };
});
vi.mock("@evergenie/sdk-web", async () => {
  const actual = await vi.importActual<typeof import("@evergenie/sdk-web")>("@evergenie/sdk-web");
  return { ...actual, upload: (...a: unknown[]) => state.upload(...a) };
});
vi.mock("@tanstack/react-query", async () => ({
  ...(await vi.importActual<typeof import("@tanstack/react-query")>("@tanstack/react-query")),
  useQueryClient: () => ({}),
}));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("../data/agentFilter", () => ({ useAgentFilter: () => ({ picked: [], one: "" }) }));
vi.mock("../data/agents", () => ({
  agentKey: (site: string, id: string) => (site ? `${site}/${id}` : id),
  splitAgentKey: (key: string) => {
    const cut = key.indexOf("/");
    return cut < 0 ? { site: "", id: key } : { site: key.slice(0, cut), id: key.slice(cut + 1) };
  },
  useAgents: () => ({
    list: [{ id: "life", name: "維納斯", enabled: true }],
    view: (id: string) => ({ id, name: "維納斯" }),
  }),
}));
// 只有一台（S3-7b 的「建在哪台」不畫）：負責秘書就是那台的職位 id。
vi.mock("./SitePick", () => {
  const site = state.primary;
  return {
    useSiteTarget: (fallback: string) => ({
      agents: state.agents,
      sites: state.agents.map((a) => a.site),
      agent: fallback,
      agentId: fallback,
      agentName: "維納斯",
      site,
      ready: true,
      picked: false,
      setAgent: state.setAgent,
      setSite: vi.fn(),
    }),
    enabledAgentKeys: () => ["life"],
    // 照 agentKey 找台（別台是「台名/職位 id」），跟真的 resolveAgentKey 一樣找不到回 undefined。
    resolveAgentKey: (_: unknown, key: string) => {
      if (!key) return undefined;
      const cut = key.indexOf("/");
      const at = cut < 0 ? "" : key.slice(0, cut);
      const id = cut < 0 ? key : key.slice(cut + 1);
      const found = state.agents.find((a) => a.site.key === at);
      return found ? { site: found.site, id, name: found.view(id).name } : undefined;
    },
    SiteAgentPicker: ({
      label,
      agents,
      onChange,
    }: {
      label: string;
      agents: typeof state.agents;
      onChange: (key: string) => void;
    }) => (
      <fieldset aria-label={label}>
        {agents.flatMap((a) =>
          a.list.map((r) => (
            <button key={r.id} type="button" onClick={() => onChange(r.id)}>
              {r.name}
            </button>
          )),
        )}
      </fieldset>
    ),
  };
});
vi.mock("./SiteTag", () => ({
  SitePickChip: () => null,
  SiteTag: ({ site }: { site: { key: string } }) => <span data-testid="site-tag">{site.key}</span>,
  siteName: (s: { label?: string }) => s.label || "這台",
  useMultiSite: () => state.multi,
}));
vi.mock("../data/quick", () => ({
  sayToAgent: (...a: unknown[]) => state.say(...a),
  createReminder: (...a: unknown[]) => state.todo(...a),
}));
vi.mock("../data/taskCreate", () => ({ createTask: vi.fn() }));
vi.mock("../lib/media", () => ({ useIsDesktop: () => true }));

beforeEach(() => {
  state.upload.mockReset().mockResolvedValue({ name: "x", url: "/share/p.png", size: 3 });
  state.say.mockReset().mockResolvedValue(undefined);
  state.agents = [state.roster(state.primary, "維納斯")];
  state.multi = false;
});
afterEach(cleanup);

describe("QuickInput 貼圖", () => {
  // Guards: REQ-287
  test("TestQuickInputPasteImage：貼截圖會上傳，送給秘書時圖片行接在話的後面", async () => {
    render(<QuickInput variant="today" />);
    const box = screen.getByLabelText("快速輸入") as HTMLInputElement;
    fireEvent.change(box, { target: { value: "@維納斯 幫我看這張" } });
    const shot = new File(["x"], "image.png", { type: "image/png" });
    fireEvent.paste(box, { clipboardData: { files: [shot], types: ["Files"] } });
    await waitFor(() => expect(screen.getByRole("img")).toBeTruthy());
    expect(state.upload).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "送出" }));
    await waitFor(() => expect(state.say).toHaveBeenCalled());
    expect(state.say.mock.calls[0]?.[2]).toMatch(
      /^幫我看這張\n!\[paste-\d+\.png\]\(\/share\/p\.png\)$/,
    );
  });

  // DEV-104／TSK-28：「交代 …」開任務單時，貼的圖跟著進單的附件（以前只送標題，圖不見）。
  test("TestQuickInputAssignCarriesImages：交代開單時貼的圖變成單的附件", async () => {
    vi.mocked(createTask)
      .mockReset()
      .mockResolvedValue(undefined as never);
    render(<QuickInput variant="today" />);
    const box = screen.getByLabelText("快速輸入") as HTMLInputElement;
    fireEvent.change(box, { target: { value: "交代 照這張改版面" } });
    const shot = new File(["x"], "image.png", { type: "image/png" });
    fireEvent.paste(box, { clipboardData: { files: [shot], types: ["Files"] } });
    await waitFor(() => expect(screen.getByRole("img")).toBeTruthy());
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(createTask).toHaveBeenCalled());
    expect(vi.mocked(createTask).mock.calls[0]?.[1]).toMatchObject({
      title: "照這張改版面",
      attachments: ["/share/p.png"],
    });
  });

  // RMD-23：待辦的描述放在「更多選項」；貼的圖、選的檔變成待辦的附件，不再塞進描述。
  test("TestQuickInputTodoDescAndAttachments：記待辦時帶描述與附件", async () => {
    state.todo.mockReset().mockResolvedValue({ id: "R-1" });
    render(<QuickInput variant="reminders" />);
    const box = screen.getByLabelText("新增待辦") as HTMLInputElement;
    fireEvent.change(box, { target: { value: "看牙醫" } });
    fireEvent.click(screen.getByRole("button", { name: "更多選項" }));
    fireEvent.change(screen.getByLabelText("描述"), { target: { value: "  帶健保卡  " } });
    const shot = new File(["x"], "image.png", { type: "image/png" });
    fireEvent.paste(box, { clipboardData: { files: [shot], types: ["Files"] } });
    const doc = new File(["%PDF"], "掛號單.pdf", { type: "application/pdf" });
    fireEvent.change(screen.getByTestId("quick-file"), { target: { files: [doc] } });
    await waitFor(() => expect(state.upload).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "送出" })).not.toHaveProperty("disabled", true),
    );
    fireEvent.click(screen.getByRole("button", { name: "送出" }));
    await waitFor(() => expect(state.todo).toHaveBeenCalled());
    const input = state.todo.mock.calls[0]?.[1] as { body?: string; attachments?: string[] };
    expect(input.body).toBe("帶健保卡");
    expect(input.attachments).toEqual(["/share/p.png", "/share/p.png"]);
  });

  // DEV-148：待辦頁「更多選項」裡就能選負責秘書，不用先打字再點預覽列的「負責：…」。
  test("TestQuickInputMoreHasOwner：更多選項有負責秘書", () => {
    render(<QuickInput variant="reminders" />);
    state.setAgent.mockReset();
    fireEvent.click(screen.getByRole("button", { name: "更多選項" }));
    const group = screen.getByRole("group", { name: "負責秘書" });
    fireEvent.click(within(group).getByRole("button", { name: "維納斯" }));
    expect(state.setAgent).toHaveBeenCalledWith("life");
  });

  // RMD-23／TSK-28：待辦頁寫了描述再「交代 …」，描述跟著進單的說明，不默默丟掉。
  test("TestQuickInputAssignKeepsDesc：待辦頁交代開單時帶上描述", async () => {
    vi.mocked(createTask)
      .mockReset()
      .mockResolvedValue(undefined as never);
    render(<QuickInput variant="reminders" />);
    const box = screen.getByLabelText("新增待辦") as HTMLInputElement;
    fireEvent.click(screen.getByRole("button", { name: "更多選項" }));
    fireEvent.change(screen.getByLabelText("描述"), { target: { value: "上次的版本在雲端" } });
    fireEvent.change(box, { target: { value: "交代 整理照片" } });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(createTask).toHaveBeenCalled());
    expect(vi.mocked(createTask).mock.calls[0]?.[1]).toMatchObject({
      title: "整理照片",
      body: "上次的版本在雲端",
    });
  });

  // TSK-28：有檔案沒傳成就擋下來說明，不默默少送一個。
  test("TestQuickInputBlocksFailedUpload：上傳失敗的檔還在時不送", async () => {
    state.upload.mockReset().mockRejectedValue(new Error("太大"));
    state.todo.mockReset().mockResolvedValue({ id: "R-1" });
    render(<QuickInput variant="reminders" />);
    const box = screen.getByLabelText("新增待辦") as HTMLInputElement;
    fireEvent.change(box, { target: { value: "看牙醫" } });
    const shot = new File(["x"], "image.png", { type: "image/png" });
    fireEvent.paste(box, { clipboardData: { files: [shot], types: ["Files"] } });
    await waitFor(() => expect(screen.getByText("上傳失敗")).toBeTruthy());
    fireEvent.keyDown(box, { key: "Enter" });
    await new Promise((r) => setTimeout(r, 20));
    expect(state.todo).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /^拿掉 / }));
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(state.todo).toHaveBeenCalled());
  });

  // Guards: REQ-287
  test("TestQuickInputPasteText：沒有圖片的貼上不攔，也不上傳", () => {
    render(<QuickInput variant="today" />);
    const box = screen.getByLabelText("快速輸入");
    fireEvent.paste(box, { clipboardData: { files: [], types: ["text/plain"] } });
    expect(state.upload).not.toHaveBeenCalled();
  });
});

// 看板連多台（S3 收尾）：「@秘書」對得到別台的秘書，話送到她那台；同名時打名字對到主伺服器那位，建議清單點別台那位會填「@台名/職位 id」。
describe("QuickInput 跟別台的秘書說", () => {
  beforeEach(() => {
    state.agents = [state.roster(state.primary, "維納斯"), state.roster(state.office, "維納斯")];
    state.multi = true;
  });

  test("建議清單列出兩台的同名秘書並標台名；點別台那位填入台名/職位 id，話送到那台", async () => {
    render(<QuickInput variant="today" />);
    const box = screen.getByLabelText("快速輸入") as HTMLInputElement;
    fireEvent.change(box, { target: { value: "@維" } });
    const office = screen.getByRole("button", { name: "維納斯（amy-office）" });
    expect(screen.getByRole("button", { name: "維納斯（這台）" })).toBeTruthy();
    fireEvent.click(office);
    expect(box.value).toBe("@amy-office/life ");
    fireEvent.change(box, { target: { value: "@amy-office/life 早安" } });
    fireEvent.click(screen.getByRole("button", { name: "送出" }));
    await waitFor(() => expect(state.say).toHaveBeenCalled());
    const [, agent, body, , site] = state.say.mock.calls[0] ?? [];
    expect(agent).toEqual({ id: "life", name: "維納斯" });
    expect(body).toBe("早安");
    expect(site).toMatchObject({ key: "amy-office" });
  });

  test("同名時打名字對到主伺服器那位（主伺服器在第一個）", async () => {
    render(<QuickInput variant="today" />);
    const box = screen.getByLabelText("快速輸入");
    fireEvent.change(box, { target: { value: "@維納斯 晚安" } });
    fireEvent.click(screen.getByRole("button", { name: "送出" }));
    await waitFor(() => expect(state.say).toHaveBeenCalled());
    expect(state.say.mock.calls[0]?.[4]).toMatchObject({ key: "", primary: true });
  });

  test("送給別台的秘書時，貼的圖在那台再傳一次，話裡用那台的附件網址", async () => {
    state.upload.mockImplementation(async (_: File, base = "") => ({
      name: "x",
      url: base ? "/share/b.png" : "/share/a.png",
      size: 3,
    }));
    render(<QuickInput variant="today" />);
    const box = screen.getByLabelText("快速輸入");
    fireEvent.change(box, { target: { value: "@amy-office/life 看這張" } });
    const shot = new File(["x"], "image.png", { type: "image/png" });
    fireEvent.paste(box, { clipboardData: { files: [shot], types: ["Files"] } });
    await waitFor(() => expect(screen.getByRole("img")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "送出" }));
    await waitFor(() => expect(state.say).toHaveBeenCalled());
    expect(state.upload).toHaveBeenLastCalledWith(shot, "https://office.example");
    expect(state.say.mock.calls[0]?.[2]).toMatch(
      /^看這張\n!\[paste-\d+\.png\]\(\/share\/b\.png\)$/,
    );
  });

  test("那位的台現在不能用（不在能用的台裡）：找不到她，不送、也不改送主伺服器", async () => {
    state.agents = [state.roster(state.primary, "維納斯")];
    render(<QuickInput variant="today" />);
    const box = screen.getByLabelText("快速輸入");
    fireEvent.change(box, { target: { value: "@amy-office/life 早安" } });
    fireEvent.click(screen.getByRole("button", { name: "送出" }));
    expect(screen.getByRole("alert").textContent).toContain("amy-office/life");
    expect(state.say).not.toHaveBeenCalled();
  });
});
