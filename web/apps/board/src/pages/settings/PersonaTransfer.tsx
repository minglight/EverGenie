import {
  api,
  MAX_PERSONA_UPLOAD_BYTES,
  type PersonaCard,
  type PersonasExportResponse,
  type PersonasImportResponse,
  uploadPersona,
} from "@evergenie/sdk-web";
import { cn } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { Download, FileUp, Upload } from "lucide-react";
import { type ReactNode, useRef, useState } from "react";
import { ActionButton, actionButtonClass } from "../../components/ActionButton";
import { FormDialog } from "../../components/FormDialog";
import { Pill } from "../../components/Pill";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { invalidatePersonas } from "./queries";
import {
  exportable,
  hasEdits,
  importCounts,
  keptItems,
  sizeText,
  startDownload,
  toggle,
} from "./transfer";

// 介紹分頁的匯出、匯入（DEV-116 S3、WEB-151～155）：檔案的上傳下載走看板（/api/web/persona-upload、/persona-export/），
// 匯出、匯入本身是 personas.export／personas.import，跟指令同一份程式；這裡只畫勾選、預覽、確定與結果。

function Alert({ children }: { children: ReactNode }) {
  return (
    <p
      role="alert"
      className="rounded-xl bg-danger-soft px-3 py-2.5 text-[13px] leading-snug break-words text-danger"
    >
      {children}
    </p>
  );
}

/** 一段可以捲的清單（手機上不會把確定鈕推出畫面）。 */
function ItemList({
  title,
  items,
  testId,
  tone = "muted",
}: {
  title: string;
  items: string[];
  testId: string;
  tone?: "muted" | "attention";
}) {
  if (items.length === 0) return null;
  return (
    <section data-testid={testId} className="flex flex-col gap-1">
      <h4
        className={cn(
          "text-[13px] font-semibold",
          tone === "attention" ? "text-attention-fg" : "text-fg",
        )}
      >
        {title}
      </h4>
      <ul className="m-0 max-h-40 list-none overflow-auto rounded-xl bg-surface-2 p-2 text-xs leading-relaxed break-all">
        {items.map((it) => (
          <li key={it}>{it}</li>
        ))}
      </ul>
    </section>
  );
}

/**
 * 對話框關掉（或重來）時作廢還在跑的請求：回來的結果不寫回畫面，免得下次打開看到上一個檔的預覽、
 * 或關掉之後還跳下載。`start()` 拿一個號碼，`stale(n)` 判斷這次的結果還要不要。
 */
function useGeneration() {
  const gen = useRef(0);
  return {
    start: () => gen.current,
    bump: () => {
      gen.current += 1;
    },
    stale: (n: number) => n !== gen.current,
  };
}

// ── 匯出 ─────────────────────────────────────────────────────────────────

export function ExportDialog({
  cards,
  open,
  onClose,
  preset = [],
}: {
  cards: readonly PersonaCard[];
  open: boolean;
  onClose: () => void;
  /** 一打開就勾好的代號（角色頁的「匯出這位」，DEV-143）。 */
  preset?: readonly string[];
}) {
  const list = exportable(cards);
  const hasPackage = cards.some((c) => c.source === "package");
  const [picked, setPicked] = useState<string[]>([...preset]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState<PersonasExportResponse | null>(null);
  const gen = useGeneration();

  const close = () => {
    gen.bump();
    setPicked([...preset]);
    setBusy(false);
    setError("");
    setDone(null);
    onClose();
  };
  const go = async () => {
    const n = gen.start();
    setBusy(true);
    setError("");
    try {
      const res = await api.personas.export({ personas: picked });
      // 關掉了就不下載（檔留在家的暫存，一小時後清）。
      if (gen.stale(n)) return;
      if (res.download) startDownload(res.download, res.filename ?? "persona.hgpersona");
      setDone(res);
    } catch (err) {
      if (!gen.stale(n)) setError(t("settings.personas.export.failed", { reason: reasonOf(err) }));
    } finally {
      if (!gen.stale(n)) setBusy(false);
    }
  };
  const allPicked = list.length > 0 && picked.length === list.length;

  return (
    <FormDialog
      open={open}
      title={t("settings.personas.export.title")}
      body={t("settings.personas.export.body")}
      onClose={close}
    >
      <div className="flex flex-col gap-3 px-5 pb-4" data-testid="persona-export">
        {done ? (
          <>
            <p role="status" className="text-[14px] leading-snug" data-testid="persona-export-done">
              {t("settings.personas.export.done", {
                file: done.filename ?? "",
                files: String(done.files),
                records: String(done.records),
                size: sizeText(done.bytes),
              })}
            </p>
            <p className="text-[13px] leading-snug text-muted">
              {t("settings.personas.export.done.hint")}
            </p>
            <ItemList
              title={t("settings.personas.export.skipped")}
              items={done.skipped ?? []}
              testId="persona-export-skipped"
            />
            <div className="flex flex-wrap justify-end gap-2 pt-1">
              {done.download ? (
                // 瀏覽器擋掉自動下載時的備用（伺服器整個送完一次才刪，所以這顆在第一次沒成功時有用）。
                <a
                  href={done.download}
                  download={done.filename}
                  data-testid="persona-export-link"
                  className={actionButtonClass("outline", "page")}
                >
                  <Download aria-hidden="true" className="size-4" />
                  {t("settings.personas.export.again")}
                </a>
              ) : null}
              <ActionButton tone="primary" onClick={close}>
                {t("settings.personas.transfer.close")}
              </ActionButton>
            </div>
          </>
        ) : list.length === 0 ? (
          <p className="text-[13px] leading-snug text-muted">
            {t("settings.personas.export.empty")}
          </p>
        ) : (
          <>
            <div className="flex items-center justify-between gap-3">
              <span className="text-[13px] font-semibold">
                {t("settings.personas.export.pick", { n: String(picked.length) })}
              </span>
              <button
                type="button"
                onClick={() => setPicked(allPicked ? [] : list.map((c) => c.slug ?? ""))}
                className="min-h-11 px-2 text-[13px] font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
              >
                {allPicked ? t("settings.personas.export.none") : t("settings.personas.export.all")}
              </button>
            </div>
            <ul
              aria-label={t("settings.personas.export.pick.aria")}
              className="m-0 flex max-h-[45vh] list-none flex-col divide-y divide-border overflow-auto rounded-xl border border-border p-0"
            >
              {list.map((card) => {
                const slug = card.slug ?? "";
                return (
                  <li key={`${card.source}-${slug}`}>
                    <label className="flex min-h-12 cursor-pointer items-center gap-3 px-3 py-2 hover:bg-hover">
                      <input
                        type="checkbox"
                        checked={picked.includes(slug)}
                        onChange={() => setPicked((p) => toggle(p, slug))}
                        data-testid="persona-export-pick"
                        data-slug={slug}
                        className="size-5 shrink-0 accent-[var(--color-accent)]"
                      />
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="text-[15px] leading-snug font-semibold break-words">
                          {card.name || slug}
                        </span>
                        <span className="text-xs text-muted">{slug}</span>
                      </span>
                      <Pill tone={card.source === "official" ? "muted" : "accent"}>
                        {card.source === "official"
                          ? t("settings.personas.tag.official")
                          : t("settings.personas.tag.mine")}
                      </Pill>
                    </label>
                  </li>
                );
              })}
            </ul>
            {hasPackage ? (
              <p className="text-xs leading-snug text-muted">
                {t("settings.personas.export.packageNote")}
              </p>
            ) : null}
            {error ? <Alert>{error}</Alert> : null}
            <div className="flex flex-wrap justify-end gap-2 pt-1">
              <ActionButton tone="outline" onClick={close} data-testid="persona-transfer-cancel">
                {t("settings.personas.form.cancel")}
              </ActionButton>
              <ActionButton
                tone="primary"
                busy={busy}
                disabled={picked.length === 0}
                onClick={() => void go()}
                data-testid="persona-export-go"
              >
                <Download aria-hidden="true" className="size-4" />
                {t("settings.personas.export.go")}
              </ActionButton>
            </div>
          </>
        )}
      </div>
    </FormDialog>
  );
}

// ── 匯入 ─────────────────────────────────────────────────────────────────

type ImportState =
  | { step: "pick" }
  | { step: "checking"; file: File }
  | { step: "preview"; file: File; upload: string; res: PersonasImportResponse }
  | { step: "importing"; file: File; upload: string; res: PersonasImportResponse }
  | { step: "done"; res: PersonasImportResponse };

function Counts({ res }: { res: PersonasImportResponse }) {
  const c = importCounts(res);
  return (
    <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[13px] leading-snug">
      <dt className="font-semibold">{t("settings.personas.import.files")}</dt>
      <dd className="m-0" data-testid="persona-import-files">
        {t("settings.personas.import.counts", {
          added: String(c.added),
          replaced: String(c.replaced),
          same: String(c.same),
        })}
      </dd>
      <dt className="font-semibold">{t("settings.personas.import.records")}</dt>
      <dd className="m-0" data-testid="persona-import-records">
        {t("settings.personas.import.counts", {
          added: String(c.recordsAdded),
          replaced: String(c.recordsReplaced),
          same: String(c.recordsSame),
        })}
      </dd>
    </dl>
  );
}

function Details({ res }: { res: PersonasImportResponse }) {
  const added = [...(res.added ?? []), ...(res.records_added ?? [])];
  const replaced = [...(res.replaced ?? []), ...(res.records_replaced ?? [])];
  if (added.length + replaced.length === 0) return null;
  return (
    <details className="text-[13px]">
      <summary className="flex min-h-11 cursor-pointer items-center font-semibold text-accent">
        {t("settings.personas.import.details")}
      </summary>
      <div className="flex flex-col gap-2 pt-1">
        <ItemList
          title={t("settings.personas.import.added")}
          items={added}
          testId="persona-import-added"
        />
        <ItemList
          title={t("settings.personas.import.replaced")}
          items={replaced}
          testId="persona-import-replaced"
        />
      </div>
    </details>
  );
}

export function ImportDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const client = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const [state, setState] = useState<ImportState>({ step: "pick" });
  const [force, setForce] = useState(false);
  const [error, setError] = useState("");
  const gen = useGeneration();

  const reset = () => {
    gen.bump();
    setState({ step: "pick" });
    setForce(false);
    setError("");
    if (input.current) input.current.value = "";
  };
  const close = () => {
    // 真的在寫的時候不給關（伺服器那邊停不下來，關了也照樣寫進去）；檢查中可以關，結果作廢。
    if (state.step === "importing") return;
    reset();
    onClose();
  };
  const pick = async (file: File | undefined) => {
    if (!file) return;
    gen.bump();
    const n = gen.start();
    setError("");
    setForce(false);
    setState({ step: "checking", file });
    try {
      const up = await uploadPersona(file);
      if (gen.stale(n)) return;
      const res = await api.personas.import({ upload: up.name, dry_run: true });
      if (gen.stale(n)) return;
      setState({ step: "preview", file, upload: up.name, res });
    } catch (err) {
      if (gen.stale(n)) return;
      setState({ step: "pick" });
      setError(t("settings.personas.import.checkFailed", { reason: reasonOf(err) }));
    } finally {
      if (input.current) input.current.value = "";
    }
  };
  const confirm = async () => {
    if (state.step !== "preview") return;
    const n = gen.start();
    setError("");
    setState({ ...state, step: "importing" });
    try {
      const res = await api.personas.import({ upload: state.upload, force });
      await invalidatePersonas(client);
      if (gen.stale(n)) return;
      setState({ step: "done", res });
    } catch (err) {
      if (gen.stale(n)) return;
      setState({ ...state, step: "preview" });
      setError(t("settings.personas.import.failed", { reason: reasonOf(err) }));
    }
  };

  const chooser = (
    <>
      <input
        ref={input}
        type="file"
        className="sr-only"
        tabIndex={-1}
        aria-hidden="true"
        data-testid="persona-import-file"
        onChange={(e) => void pick(e.target.files?.[0])}
      />
      <ActionButton
        tone={state.step === "pick" ? "primary" : "outline"}
        busy={state.step === "checking"}
        disabled={state.step === "importing"}
        onClick={() => input.current?.click()}
        data-testid="persona-import-choose"
      >
        <FileUp aria-hidden="true" className="size-4" />
        {state.step === "pick" || state.step === "checking"
          ? t("settings.personas.import.choose")
          : t("settings.personas.import.another")}
      </ActionButton>
    </>
  );

  let content: ReactNode;
  switch (state.step) {
    case "pick":
    case "checking":
      content = (
        <>
          <p className="text-[13px] leading-snug text-muted">
            {state.step === "checking"
              ? t("settings.personas.import.checking", { file: state.file.name })
              : t("settings.personas.import.hint", { limit: sizeText(MAX_PERSONA_UPLOAD_BYTES) })}
          </p>
          {error ? <Alert>{error}</Alert> : null}
          <div className="flex flex-wrap justify-end gap-2 pt-1">
            <ActionButton tone="outline" onClick={close} data-testid="persona-transfer-cancel">
              {t("settings.personas.form.cancel")}
            </ActionButton>
            {chooser}
          </div>
        </>
      );
      break;
    case "preview":
    case "importing": {
      const { res, file } = state;
      const kept = keptItems(res);
      content = (
        <div className="flex flex-col gap-3" data-testid="persona-import-preview">
          <div className="flex flex-col gap-1">
            <p className="text-[14px] leading-snug font-semibold">
              {t("settings.personas.import.preview.title", { names: res.personas.join("、") })}
            </p>
            <p className="text-xs text-muted break-all">
              {t("settings.personas.import.preview.file", {
                file: file.name,
                size: sizeText(file.size),
              })}
            </p>
          </div>
          <Counts res={res} />
          <Details res={res} />
          {kept.length > 0 ? (
            <>
              <ItemList
                title={t("settings.personas.import.kept", { n: String(kept.length) })}
                items={kept}
                testId="persona-import-kept"
                tone="attention"
              />
              <p className="text-xs leading-snug text-muted">
                {force
                  ? t("settings.personas.import.kept.forced")
                  : t("settings.personas.import.kept.hint")}
              </p>
            </>
          ) : null}
          {hasEdits(res) ? (
            <label className="flex min-h-11 items-center gap-2 text-[14px]">
              <input
                type="checkbox"
                checked={force}
                onChange={(e) => setForce(e.target.checked)}
                disabled={state.step === "importing"}
                data-testid="persona-import-force"
                className="size-5 shrink-0 accent-[var(--color-accent)]"
              />
              {t("settings.personas.import.force")}
            </label>
          ) : null}
          <ItemList
            title={t("settings.personas.import.skipped", {
              n: String(res.records_skipped?.length ?? 0),
            })}
            items={res.records_skipped ?? []}
            testId="persona-import-skipped"
          />
          {error ? <Alert>{error}</Alert> : null}
          <div className="flex flex-wrap justify-end gap-2 pt-1">
            {chooser}
            <ActionButton
              tone="primary"
              busy={state.step === "importing"}
              onClick={() => void confirm()}
              data-testid="persona-import-confirm"
            >
              <Upload aria-hidden="true" className="size-4" />
              {t("settings.personas.import.confirm")}
            </ActionButton>
          </div>
        </div>
      );
      break;
    }
    case "done":
      content = (
        <div className="flex flex-col gap-3" data-testid="persona-import-done">
          <p role="status" className="text-[14px] leading-snug font-semibold">
            {t("settings.personas.import.done", { names: state.res.personas.join("、") })}
          </p>
          <Counts res={state.res} />
          <ItemList
            title={t("settings.personas.import.kept", {
              n: String(keptItems(state.res).length),
            })}
            items={keptItems(state.res)}
            testId="persona-import-kept"
            tone="attention"
          />
          <ItemList
            title={t("settings.personas.import.skipped", {
              n: String(state.res.records_skipped?.length ?? 0),
            })}
            items={state.res.records_skipped ?? []}
            testId="persona-import-skipped"
          />
          {state.res.backup ? (
            <p className="text-[13px] leading-snug break-all text-muted">
              {t("settings.personas.import.backup", { path: state.res.backup })}
            </p>
          ) : null}
          <p className="text-[13px] leading-snug text-muted">
            {t("settings.personas.import.done.hint")}
          </p>
          <div className="flex justify-end pt-1">
            <ActionButton tone="primary" onClick={close}>
              {t("settings.personas.transfer.close")}
            </ActionButton>
          </div>
        </div>
      );
      break;
  }

  return (
    <FormDialog
      open={open}
      title={t("settings.personas.import.title")}
      body={state.step === "done" ? undefined : t("settings.personas.import.body")}
      onClose={close}
    >
      <div className="flex flex-col gap-3 px-5 pb-4" data-testid="persona-import">
        {content}
      </div>
    </FormDialog>
  );
}

/** 角色清單的「匯入」「匯出」對話框（入口在清單右上的「⋯」，DEV-156 U-15）。 */
export function TransferDialogs({
  cards,
  open,
  onClose,
}: {
  cards: readonly PersonaCard[];
  open: "" | "export" | "import";
  onClose: () => void;
}) {
  return (
    <>
      <ExportDialog cards={cards} open={open === "export"} onClose={onClose} />
      <ImportDialog open={open === "import"} onClose={onClose} />
    </>
  );
}
