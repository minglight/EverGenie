import type { PersonaCard, PersonasImportResponse } from "@evergenie/sdk-web";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { ExportDialog, ImportDialog } from "./PersonaTransfer";
import { exportable, hasEdits, importCounts } from "./transfer";

const state = vi.hoisted(() => ({
  exportFn: vi.fn(),
  importFn: vi.fn(),
  upload: vi.fn(),
  download: vi.fn(),
}));

vi.mock("@evergenie/sdk-web", () => {
  class ApiError extends Error {
    constructor(
      public code: string,
      message: string,
    ) {
      super(message);
    }
  }
  return {
    api: { personas: { export: state.exportFn, import: state.importFn } },
    uploadPersona: state.upload,
    MAX_PERSONA_UPLOAD_BYTES: 2147483648,
    ApiError,
  };
});
vi.mock("../../components/FormDialog", () => ({
  FormDialog: ({ open, children }: { open: boolean; children: React.ReactNode }) =>
    open ? <div role="dialog">{children}</div> : null,
}));
vi.mock("./transfer", async (orig) => ({
  ...(await orig<typeof import("./transfer")>()),
  startDownload: state.download,
}));

const card = (slug: string, source: string, extra: Partial<PersonaCard> = {}): PersonaCard => ({
  slug,
  name: slug.toUpperCase(),
  source,
  status: "live",
  shadowed: false,
  updated_at: "",
  ...extra,
});

const preview = (extra: Partial<PersonasImportResponse> = {}): PersonasImportResponse => ({
  dry_run: true,
  personas: ["mina"],
  added: ["personas/mina.md", "personas/avatars/mina.png"],
  replaced: ["personas/avatars/full/mina.png"],
  unchanged: 1,
  records_added: ["dress.outfit/mina-sea"],
  records_unchanged: 0,
  records_skipped: ["dress.variant/mina-x：這個家沒裝 dress"],
  ...extra,
});

function wrap(ui: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

async function pickFile() {
  const file = new File(["zip"], "mina.hgpersona");
  fireEvent.change(screen.getByTestId("persona-import-file"), { target: { files: [file] } });
  return file;
}

beforeEach(() => {
  for (const f of Object.values(state)) f.mockReset();
  state.upload.mockResolvedValue({ name: "20261104-120000-0a1b2c3d.hgpersona", size: 3 });
});
afterEach(cleanup);

describe("角色匯出（WEB-152）", () => {
  test("只列官方與我的（套件帶來的、被蓋住的不列）；勾了才能按，按下去用伺服器給的網址下載", async () => {
    const cards = [
      card("kaguya", "official"),
      card("mina", "mine"),
      card("garden1", "package", { package: "garden" }),
      card("kaguya", "mine", { shadowed: true }),
    ];
    expect(exportable(cards).map((c) => `${c.source}-${c.slug}`)).toEqual([
      "official-kaguya",
      "mine-mina",
    ]);
    state.exportFn.mockResolvedValue({
      files: 3,
      records: 1,
      bytes: 2048,
      filename: "MINA.hgpersona",
      download: "/persona-export/t/MINA.hgpersona",
    });
    wrap(<ExportDialog cards={cards} open onClose={() => {}} />);
    const picks = screen.getAllByTestId("persona-export-pick");
    expect(picks.map((p) => p.getAttribute("data-slug"))).toEqual(["kaguya", "mina"]);
    expect(screen.getByText("套件帶來的角色跟著套件走，不在這裡匯出。")).toBeTruthy();
    expect((screen.getByTestId("persona-export-go") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(picks[1] as HTMLElement);
    fireEvent.click(screen.getByTestId("persona-export-go"));
    await screen.findByTestId("persona-export-done");
    expect(state.exportFn).toHaveBeenCalledWith({ personas: ["mina"] });
    expect(state.download).toHaveBeenCalledWith(
      "/persona-export/t/MINA.hgpersona",
      "MINA.hgpersona",
    );
    expect(screen.getByTestId("persona-export-done").textContent).toContain("2.0 KB");
    // 瀏覽器擋掉自動下載時有備用的連結。
    const link = screen.getByTestId("persona-export-link");
    expect(link.getAttribute("href")).toBe("/persona-export/t/MINA.hgpersona");
    expect(link.getAttribute("download")).toBe("MINA.hgpersona");
  });

  test("做檔中關掉：做好了也不下載，再打開是勾選畫面", async () => {
    let finish: (v: unknown) => void = () => {};
    state.exportFn.mockReturnValue(new Promise((res) => (finish = res)));
    const cards = [card("mina", "mine")];
    const { rerender } = wrap(<ExportDialog cards={cards} open onClose={() => {}} />);
    fireEvent.click(screen.getAllByTestId("persona-export-pick")[0] as HTMLElement);
    fireEvent.click(screen.getByTestId("persona-export-go"));
    fireEvent.click(screen.getByTestId("persona-transfer-cancel"));
    finish({
      files: 1,
      records: 0,
      bytes: 1,
      filename: "MINA.hgpersona",
      download: "/persona-export/t/MINA.hgpersona",
    });
    await Promise.resolve();
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <ExportDialog cards={cards} open onClose={() => {}} />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(screen.getAllByTestId("persona-export-pick")).toHaveLength(1));
    expect(screen.queryByTestId("persona-export-done")).toBeNull();
    expect(state.download).not.toHaveBeenCalled();
  });
});

describe("角色匯入（WEB-153～155）", () => {
  test("選檔先上傳、dry_run 預覽（檔案與紀錄的新增／換掉／一樣、略過的原因），按確定匯入才真的寫", async () => {
    state.importFn
      .mockResolvedValueOnce(preview())
      .mockResolvedValueOnce(preview({ dry_run: false }));
    wrap(<ImportDialog open onClose={() => {}} />);
    const file = await pickFile();
    await screen.findByTestId("persona-import-preview");
    expect(state.upload).toHaveBeenCalledWith(file);
    expect(state.importFn).toHaveBeenNthCalledWith(1, {
      upload: "20261104-120000-0a1b2c3d.hgpersona",
      dry_run: true,
    });
    expect(screen.getByTestId("persona-import-files").textContent).toBe("新增 2、換掉 1、一樣 1");
    expect(screen.getByTestId("persona-import-records").textContent).toBe("新增 1、換掉 0、一樣 0");
    expect(screen.getByTestId("persona-import-skipped").textContent).toContain("這個家沒裝 dress");
    expect(screen.queryByTestId("persona-import-kept")).toBeNull();

    fireEvent.click(screen.getByTestId("persona-import-confirm"));
    await screen.findByTestId("persona-import-done");
    expect(state.importFn).toHaveBeenNthCalledWith(2, {
      upload: "20261104-120000-0a1b2c3d.hgpersona",
      force: false,
    });
    expect(screen.getByText("已匯入：mina")).toBeTruthy();
  });

  test("「蓋掉我改過的」只在有你改過的才出現；勾了確定時帶 force", async () => {
    // 沒有你改過的：沒有勾選
    expect(hasEdits(preview())).toBe(false);
    const edited = preview({ kept: ["personas/mina.md"], records_kept: ["dress.outfit/mina-sea"] });
    expect(hasEdits(edited)).toBe(true);
    expect(importCounts(edited).kept).toBe(2);

    state.importFn
      .mockResolvedValueOnce(edited)
      .mockResolvedValueOnce(
        preview({ dry_run: false, backup: "/home/runtime/backups/persona-import-x" }),
      );
    wrap(<ImportDialog open onClose={() => {}} />);
    await pickFile();
    await screen.findByTestId("persona-import-preview");
    expect(screen.getByTestId("persona-import-kept").textContent).toContain(
      "dress.outfit/mina-sea",
    );
    const force = screen.getByTestId("persona-import-force") as HTMLInputElement;
    expect(force.checked).toBe(false);
    fireEvent.click(force);
    fireEvent.click(screen.getByTestId("persona-import-confirm"));
    await screen.findByTestId("persona-import-done");
    expect(state.importFn).toHaveBeenLastCalledWith({
      upload: "20261104-120000-0a1b2c3d.hgpersona",
      force: true,
    });
    expect(screen.getByText(/備份在 \/home\/runtime\/backups/)).toBeTruthy();
  });

  test("檢查中關掉再開：回到選檔，晚到的預覽不顯示", async () => {
    let finish: (v: unknown) => void = () => {};
    state.importFn.mockReturnValueOnce(new Promise((res) => (finish = res)));
    wrap(<ImportDialog open onClose={() => {}} />);
    await pickFile();
    await waitFor(() => expect(state.importFn).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByTestId("persona-transfer-cancel"));
    finish(preview());
    await Promise.resolve();
    await Promise.resolve();
    expect(screen.queryByTestId("persona-import-preview")).toBeNull();
    expect(screen.getByTestId("persona-import-file")).toBeTruthy();
  });

  test("壞掉的檔、sha256 不符、同一個代號在別的位置：伺服器的白話直接顯示，留在選檔", async () => {
    const { ApiError } = await import("@evergenie/sdk-web");
    wrap(<ImportDialog open onClose={() => {}} />);
    for (const msg of [
      "這不是角色匯出檔（打不開），什麼都沒寫",
      "檔案內容跟清單的 sha256 不符：personas/mina.md，什麼都沒寫",
      "mina 在這個家是官方角色，跟檔裡的位置不一樣，什麼都沒寫",
    ]) {
      state.importFn.mockRejectedValueOnce(new ApiError("invalid", msg));
      await pickFile();
      await waitFor(() =>
        expect(screen.getByRole("alert").textContent).toBe(`這個檔不能匯入：${msg}`),
      );
      expect(screen.queryByTestId("persona-import-preview")).toBeNull();
    }
    // 上傳本身失敗（太大）也是白話。
    state.upload.mockRejectedValueOnce(new ApiError("invalid", "檔案太大（上限 2 GB）"));
    await pickFile();
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toBe("這個檔不能匯入：檔案太大（上限 2 GB）"),
    );
    // 確定時才失敗：留在預覽、顯示原因，可以再按。
    state.importFn
      .mockResolvedValueOnce(preview())
      .mockRejectedValueOnce(new ApiError("unavailable", "連不到看板服務，請再試一次"));
    await pickFile();
    await screen.findByTestId("persona-import-preview");
    fireEvent.click(screen.getByTestId("persona-import-confirm"));
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toBe("沒匯入成功：連不到看板服務，請再試一次"),
    );
    expect(screen.getByTestId("persona-import-preview")).toBeTruthy();
  });
});
