import {
  api,
  type BackupCreateResponse,
  type BackupGetStatusResponse,
  type BackupPreviewResponse,
  type HomeBackupFile,
  type HomeRestoreResult,
  MAX_HOME_BACKUP_UPLOAD_BYTES,
  uploadHomeBackup,
} from "@evergenie/sdk-web";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, FileUp, HardDriveDownload, History, MonitorSmartphone } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { ActionButton, actionButtonClass } from "../../components/ActionButton";
import { FormDialog } from "../../components/FormDialog";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { t } from "../../i18n/zh-TW";
import { bytesText } from "../../lib/format";
import { reasonOf } from "../../lib/reason";
import { backupStatusQuery } from "./backupQueries";
import {
  dayTimeText,
  hasTime,
  kindKey,
  lostLinksShown,
  restorable,
  restoreFinished,
  skippedText,
  totalOf,
  usageIsBig,
} from "./backupView";
import { startDownload } from "./transfer";

// 設定 → 這台電腦 → 備份（DEV-153、WEB-190）：上次備份的時間，加三顆按鈕
// 「立刻備份」「從備份還原」「搬到新電腦」。做備份、預覽、還原都是 sdk 的 backup 服務；這裡只畫步驟與結果。

type Target = { upload: string } | { kind: string; name: string };

function Alert({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <p
      role="alert"
      data-testid={testId}
      className="rounded-xl bg-danger-soft px-3 py-2.5 text-[13px] leading-snug break-words text-danger"
    >
      {children}
    </p>
  );
}

function Note({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <div
      data-testid={testId}
      className="rounded-xl bg-attention-soft px-3 py-2.5 text-[13px] leading-snug break-words text-fg"
    >
      {children}
    </div>
  );
}

/** 備份裡有什麼：一類一行（名稱、幾個檔、多大）。 */
function Sections({ sections }: { sections: BackupPreviewResponse["sections"] }) {
  if (!sections || sections.length === 0) return null;
  return (
    <ul
      data-testid="backup-sections"
      className="m-0 flex list-none flex-col divide-y divide-border rounded-xl border border-border p-0 text-[14px]"
    >
      {sections.map((s) => (
        <li key={s.id} className="flex items-center justify-between gap-3 px-3 py-2.5">
          <span className="min-w-0 font-medium break-words">{s.label}</span>
          <span className="shrink-0 text-[13px] text-muted">
            {t("settings.backup.section.amount", { n: s.files, size: bytesText(s.bytes) })}
          </span>
        </li>
      ))}
    </ul>
  );
}

/** 做備份時沒收進去的東西（捷徑類的檔）：要讓人知道這份備份少了什麼。 */
function SkippedNote({ paths }: { paths: string[] | undefined }) {
  if (!paths || paths.length === 0) return null;
  return (
    <Note testId="backup-skipped">
      {t("settings.backup.skipped", { n: paths.length, names: skippedText(paths) })}
    </Note>
  );
}

/** 還原結果裡「還要你做的事」。 */
/** 還原後會不見的捷徑（家裡現有、備份不收的）：列出來，讓主人換之前就知道要自己重建。 */
function LostLinks({ paths }: { paths: string[] | undefined }) {
  if (!paths || paths.length === 0) return null;
  const { shown, more } = lostLinksShown(paths);
  return (
    <Note testId="backup-preview-lost">
      <span className="font-semibold">
        {t("settings.backup.preview.lost", { n: paths.length })}
      </span>
      <ul className="mt-1 list-disc pl-5">
        {shown.map((p) => (
          <li key={p} className="break-all">
            {p}
          </li>
        ))}
        {more > 0 ? <li>{t("settings.backup.preview.lostMore", { n: more })}</li> : null}
      </ul>
    </Note>
  );
}

function Todo({ items }: { items: string[] | undefined }) {
  if (!items || items.length === 0) return null;
  return (
    <div data-testid="backup-todo" className="flex flex-col gap-1">
      <p className="text-[13px] font-semibold">{t("settings.backup.todo.title")}</p>
      <ul className="m-0 flex list-disc flex-col gap-1 pl-5 text-[13px] leading-snug">
        {items.map((it) => (
          <li key={it}>{it}</li>
        ))}
      </ul>
    </div>
  );
}

// ── 立刻備份 ─────────────────────────────────────────────────────────────

/** 做一份備份。download＝做完直接開始下載（搬到新電腦用；平常的「立刻備份」只給連結，手機上不會一按就抓好幾 GB）。 */
function useCreateBackup(download: boolean) {
  const client = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState<BackupCreateResponse | null>(null);
  const run = async () => {
    setBusy(true);
    setError("");
    try {
      const res = await api.backup.create({});
      setDone(res);
      await client.invalidateQueries({ queryKey: backupStatusQuery.queryKey });
      if (download && res.file.download)
        startDownload(res.file.download, res.file.name ?? "home.egbackup");
    } catch (err) {
      setError(t("settings.backup.create.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy(false);
    }
  };
  const reset = () => {
    setDone(null);
    setError("");
  };
  return { busy, error, done, run, reset };
}

function CreatedResult({ done }: { done: BackupCreateResponse }) {
  return (
    <div className="flex flex-col gap-3" data-testid="backup-created">
      <p role="status" className="text-[14px] leading-snug font-semibold">
        {t("settings.backup.create.done", { size: bytesText(done.file.size) })}
      </p>
      <p className="text-[13px] leading-snug text-muted">{t("settings.backup.create.doneHint")}</p>
      <Sections sections={done.sections} />
      <SkippedNote paths={done.skipped} />
    </div>
  );
}

function DownloadLink({ file }: { file: HomeBackupFile }) {
  if (!file.download) return null;
  return (
    <a
      href={file.download}
      download={file.name}
      data-testid="backup-download"
      className={actionButtonClass("outline", "page")}
    >
      <Download aria-hidden="true" className="size-4" />
      {t("settings.backup.download")}
    </a>
  );
}

function CreateDialog({
  open,
  onClose,
  c,
}: {
  open: boolean;
  onClose: () => void;
  /** 做備份的狀態由區塊拿著：按下「立刻備份」那一下就開始做，這裡只畫進度與結果。 */
  c: ReturnType<typeof useCreateBackup>;
}) {
  const close = () => {
    if (c.busy) return; // 伺服器那邊停不下來；做完才關
    c.reset();
    onClose();
  };
  return (
    <FormDialog open={open} title={t("settings.backup.create.title")} onClose={close}>
      <div className="flex flex-col gap-3 px-5 pb-4" data-testid="backup-create">
        {c.done ? (
          <CreatedResult done={c.done} />
        ) : c.error ? (
          <Alert>{c.error}</Alert>
        ) : (
          <div role="status" className="flex flex-col items-center gap-2 py-4">
            <LoadingBlock />
            <p className="text-[13px] text-muted">{t("settings.backup.create.busy")}</p>
          </div>
        )}
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          {c.done ? <DownloadLink file={c.done.file} /> : null}
          {c.error ? (
            <ActionButton tone="outline" onClick={() => void c.run()}>
              {t("settings.backup.retry")}
            </ActionButton>
          ) : null}
          <ActionButton tone="primary" disabled={c.busy} onClick={close} data-testid="backup-close">
            {t("settings.backup.close")}
          </ActionButton>
        </div>
      </div>
    </FormDialog>
  );
}

// ── 從備份還原 ───────────────────────────────────────────────────────────

type RestoreState =
  | { step: "pick" }
  | { step: "checking"; label: string }
  | { step: "preview"; target: Target; res: BackupPreviewResponse }
  | { step: "sending"; target: Target; res: BackupPreviewResponse }
  | { step: "waiting"; before: string; slow: boolean }
  | { step: "done"; result: HomeRestoreResult };

/** 還原送出後最多等多久才提示「等比較久」（服務重開加上換檔，通常一分鐘內）。 */
const SLOW_AFTER_MS = 120_000;
const POLL_MS = 2_000;

function RestoreDialog({
  open,
  onClose,
  status,
}: {
  open: boolean;
  onClose: () => void;
  status: BackupGetStatusResponse;
}) {
  const client = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const [state, setState] = useState<RestoreState>({ step: "pick" });
  const [error, setError] = useState("");
  const gen = useRef(0);
  const files = restorable(status.files);

  const locked = state.step === "sending" || state.step === "waiting";
  const close = () => {
    if (locked) return; // 正在換，不能中途關掉當作沒事
    gen.current += 1;
    setState({ step: "pick" });
    setError("");
    if (input.current) input.current.value = "";
    onClose();
  };

  const check = async (label: string, get: () => Promise<Target>) => {
    gen.current += 1;
    const n = gen.current;
    setError("");
    setState({ step: "checking", label });
    try {
      const target = await get();
      if (n !== gen.current) return;
      const res = await api.backup.preview(target);
      if (n !== gen.current) return;
      setState({ step: "preview", target, res });
    } catch (err) {
      if (n !== gen.current) return;
      setState({ step: "pick" });
      setError(t("settings.backup.restore.checkFailed", { reason: reasonOf(err) }));
    } finally {
      if (input.current) input.current.value = "";
    }
  };
  const pickFile = (file: File | undefined) => {
    if (!file) return;
    void check(file.name, async () => ({ upload: (await uploadHomeBackup(file)).name }));
  };
  const pickExisting = (f: HomeBackupFile) => {
    void check(dayTimeText(f.created_at), async () => ({ kind: f.kind ?? "", name: f.name ?? "" }));
  };

  const confirm = async () => {
    if (state.step !== "preview") return;
    const before = status.last_restore?.at ?? "";
    setError("");
    setState({ ...state, step: "sending" });
    try {
      await api.backup.restore(state.target);
      setState({ step: "waiting", before, slow: false });
    } catch (err) {
      setState({ ...state, step: "preview" });
      setError(t("settings.backup.restore.failed", { reason: reasonOf(err) }));
    }
  };

  // 等服務重開：每兩秒問一次現況（重開的那幾秒連不上是正常的），拿到新的一筆結果才算做完。
  const waiting = state.step === "waiting" ? state.before : null;
  useEffect(() => {
    if (waiting === null) return;
    const started = Date.now();
    let stopped = false;
    const timer = window.setInterval(() => {
      void api.backup
        .getStatus({})
        .then((now) => {
          if (stopped) return;
          const result = restoreFinished(waiting, now);
          if (result) {
            setState({ step: "done", result });
            void client.invalidateQueries();
          }
        })
        .catch(() => {
          // 服務正在重開，等下一輪
        });
      if (!stopped && Date.now() - started > SLOW_AFTER_MS) {
        setState((s) => (s.step === "waiting" && !s.slow ? { ...s, slow: true } : s));
      }
    }, POLL_MS);
    return () => {
      stopped = true;
      window.clearInterval(timer);
    };
  }, [waiting, client]);

  let content: ReactNode;
  switch (state.step) {
    case "pick":
    case "checking":
      content = (
        <>
          <input
            ref={input}
            type="file"
            // 改名前的舊版備份（.hgbackup）也選得到：這一版不還原，選了會白話說原因，不是默默看不到檔。
            accept=".egbackup,.hgbackup"
            className="sr-only"
            tabIndex={-1}
            aria-hidden="true"
            data-testid="backup-restore-file"
            onChange={(e) => pickFile(e.target.files?.[0])}
          />
          {state.step === "checking" ? (
            <p role="status" className="text-[13px] leading-snug text-muted">
              {t("settings.backup.restore.checking", { file: state.label })}
            </p>
          ) : null}
          {error ? <Alert testId="backup-restore-error">{error}</Alert> : null}
          {status.local ? (
            <>
              <ActionButton
                tone="outline"
                busy={state.step === "checking"}
                onClick={() => input.current?.click()}
                data-testid="backup-restore-choose"
                className="w-full"
              >
                <FileUp aria-hidden="true" className="size-4" />
                {t("settings.backup.restore.choose")}
              </ActionButton>
              <p className="text-xs leading-snug text-muted">
                {t("settings.backup.restore.chooseHint", {
                  limit: bytesText(MAX_HOME_BACKUP_UPLOAD_BYTES),
                })}
              </p>
            </>
          ) : (
            <Note testId="backup-restore-thismac">{t("settings.backup.restore.thisMac")}</Note>
          )}
          {files.length > 0 ? (
            <section className="flex flex-col gap-1.5">
              <h4 className="text-[13px] font-semibold">{t("settings.backup.restore.existing")}</h4>
              <ul
                data-testid="backup-restore-list"
                className="m-0 flex max-h-[40vh] list-none flex-col divide-y divide-border overflow-auto rounded-xl border border-border p-0"
              >
                {files.map((f) => (
                  <li key={`${f.kind}/${f.name}`}>
                    <button
                      type="button"
                      disabled={state.step === "checking"}
                      onClick={() => pickExisting(f)}
                      data-testid="backup-restore-pick"
                      data-kind={f.kind}
                      className="flex min-h-12 w-full items-center justify-between gap-3 px-3 py-2 text-left hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50"
                    >
                      <span className="flex min-w-0 flex-col">
                        <span className="text-[14px] leading-snug font-semibold">
                          {dayTimeText(f.created_at)}
                        </span>
                        <span className="text-xs text-muted">{t(kindKey(f.kind))}</span>
                      </span>
                      <span className="shrink-0 text-xs text-muted">{bytesText(f.size)}</span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          ) : null}
          <div className="flex justify-end pt-1">
            <ActionButton tone="outline" onClick={close}>
              {t("settings.backup.cancel")}
            </ActionButton>
          </div>
        </>
      );
      break;
    case "preview":
    case "sending": {
      const { res } = state;
      content = (
        <div className="flex flex-col gap-3" data-testid="backup-restore-preview">
          <div className="flex flex-col gap-0.5">
            <p className="text-xs text-muted">{t("settings.backup.preview.when")}</p>
            <p className="text-[17px] leading-snug font-semibold" data-testid="backup-preview-when">
              {dayTimeText(res.created_at)}
            </p>
            <p className="text-[13px] leading-snug break-words text-muted">
              {t("settings.backup.preview.from", {
                home: res.home_name ?? "",
                owner: res.owner ?? "",
                size: bytesText(res.size),
              })}
            </p>
          </div>
          <Sections sections={res.sections} />
          <SkippedNote paths={res.skipped} />
          {res.other_machine ? (
            <Note testId="backup-preview-other">{t("settings.backup.preview.other")}</Note>
          ) : null}
          {res.other_machine && res.installed && res.installed.length > 0 ? (
            <Note>
              {t("settings.backup.preview.installed", { names: res.installed.join("、") })}
            </Note>
          ) : null}
          <LostLinks paths={res.lost_links} />
          {res.problem ? (
            <Alert testId="backup-preview-problem">{res.problem}</Alert>
          ) : (
            <Note testId="backup-preview-warning">{t("settings.backup.preview.warning")}</Note>
          )}
          {error ? <Alert testId="backup-restore-error">{error}</Alert> : null}
          <div className="flex flex-wrap justify-end gap-2 pt-1">
            <ActionButton
              tone="outline"
              disabled={state.step === "sending"}
              onClick={() => {
                setError("");
                setState({ step: "pick" });
              }}
            >
              {t("settings.backup.restore.another")}
            </ActionButton>
            {res.problem ? null : (
              <ActionButton
                tone="danger"
                busy={state.step === "sending"}
                onClick={() => void confirm()}
                data-testid="backup-restore-confirm"
              >
                {t("settings.backup.restore.confirm")}
              </ActionButton>
            )}
          </div>
        </div>
      );
      break;
    }
    case "waiting":
      content = (
        <div
          role="status"
          className="flex flex-col items-center gap-2 py-4"
          data-testid="backup-restore-waiting"
        >
          <LoadingBlock />
          <p className="text-center text-[14px] leading-snug font-semibold">
            {t("settings.backup.restore.waiting")}
          </p>
          <p className="text-center text-[13px] leading-snug text-muted">
            {state.slow
              ? t("settings.backup.restore.waitingSlow")
              : t("settings.backup.restore.waitingHint")}
          </p>
          {state.slow ? (
            <ActionButton tone="outline" onClick={() => window.location.reload()}>
              {t("settings.backup.reload")}
            </ActionButton>
          ) : null}
        </div>
      );
      break;
    case "done":
      content = (
        <div className="flex flex-col gap-3" data-testid="backup-restore-done">
          {state.result.ok ? (
            <p role="status" className="text-[15px] leading-snug font-semibold">
              {t("settings.backup.restore.done", {
                when: dayTimeText(state.result.backup_created_at),
              })}
            </p>
          ) : (
            <Alert testId="backup-restore-notdone">{state.result.message}</Alert>
          )}
          {state.result.ok && state.result.safety ? (
            <p className="text-[13px] leading-snug text-muted">
              {t("settings.backup.restore.safety")}
            </p>
          ) : null}
          <Todo items={state.result.todo} />
          <div className="flex justify-end pt-1">
            <ActionButton
              tone="primary"
              onClick={() => window.location.reload()}
              data-testid="backup-restore-reload"
            >
              {t("settings.backup.reload")}
            </ActionButton>
          </div>
        </div>
      );
      break;
  }

  return (
    <FormDialog
      open={open}
      title={t("settings.backup.restore.title")}
      body={state.step === "pick" ? t("settings.backup.restore.body") : undefined}
      onClose={close}
    >
      <div className="flex flex-col gap-3 px-5 pb-4" data-testid="backup-restore">
        {content}
      </div>
    </FormDialog>
  );
}

// ── 搬到新電腦 ───────────────────────────────────────────────────────────

function MoveDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const c = useCreateBackup(true);
  const close = () => {
    if (c.busy) return;
    c.reset();
    onClose();
  };
  const steps = [
    t("settings.backup.move.step1"),
    t("settings.backup.move.step2"),
    t("settings.backup.move.step3"),
    t("settings.backup.move.step4"),
  ];
  return (
    <FormDialog
      open={open}
      title={t("settings.backup.move.title")}
      body={t("settings.backup.move.body")}
      onClose={close}
    >
      <div className="flex flex-col gap-3 px-5 pb-4" data-testid="backup-move">
        <ol className="m-0 flex list-decimal flex-col gap-2 pl-5 text-[14px] leading-snug">
          {steps.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ol>
        {c.done ? <CreatedResult done={c.done} /> : null}
        {c.error ? <Alert>{c.error}</Alert> : null}
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <ActionButton tone="outline" disabled={c.busy} onClick={close}>
            {t("settings.backup.close")}
          </ActionButton>
          {c.done ? (
            <DownloadLink file={c.done.file} />
          ) : (
            <ActionButton
              tone="primary"
              busy={c.busy}
              onClick={() => void c.run()}
              data-testid="backup-move-go"
            >
              <Download aria-hidden="true" className="size-4" />
              {t("settings.backup.move.go")}
            </ActionButton>
          )}
        </div>
      </div>
    </FormDialog>
  );
}

// ── 區塊本身 ─────────────────────────────────────────────────────────────

/** 「備份」區塊最上面那幾列：現況、三顆按鈕。備份只放在這台電腦（法律第 7 條），要另外存一份主人自己下載。 */
export function HomeBackup() {
  const q = useQuery(backupStatusQuery);
  const [open, setOpen] = useState<"" | "create" | "restore" | "move">("");
  const creating = useCreateBackup(false);
  if (q.data === undefined) {
    return q.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void q.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const s = q.data;
  const close = () => setOpen("");
  const last = s.last_restore;
  const total = totalOf(s.files);
  return (
    <>
      <div className="flex flex-col gap-3 px-4 py-3.5" data-testid="home-backup">
        <div className="flex flex-col gap-0.5">
          <p className="text-[15px] leading-snug font-semibold" data-testid="backup-last">
            {hasTime(s.last_at)
              ? t("settings.backup.last", {
                  when: dayTimeText(s.last_at),
                  kind: t(kindKey(s.last_kind)),
                })
              : t("settings.backup.last.none")}
          </p>
          <p className="text-[13px] leading-snug text-muted">
            {t("settings.backup.rule", {
              keep: s.keep_count,
              days: s.keep_days,
            })}
          </p>
          <p className="text-[13px] leading-snug text-muted" data-testid="backup-only-here">
            {t("settings.backup.onlyHere")}
          </p>
          {total.count > 0 ? (
            <p className="text-[13px] leading-snug text-muted" data-testid="backup-total">
              {t("settings.backup.total", { n: total.count, size: bytesText(total.bytes) })}
            </p>
          ) : null}
          {usageIsBig(total.bytes, s.free_bytes) ? (
            <p
              className="text-[13px] leading-snug font-medium text-danger"
              data-testid="backup-total-big"
            >
              {s.free_bytes > 0
                ? t("settings.backup.total.big", { free: bytesText(s.free_bytes) })
                : t("settings.backup.total.bigNoFree")}
            </p>
          ) : null}
        </div>
        {s.restoring ? <Note>{t("settings.backup.restoringNow")}</Note> : null}
        {last ? (
          <div data-testid="backup-last-restore" className="flex flex-col gap-2">
            {last.ok ? (
              <Note>
                {t("settings.backup.lastRestore.ok", {
                  at: dayTimeText(last.at),
                  when: dayTimeText(last.backup_created_at),
                })}
              </Note>
            ) : (
              <Alert>
                {t("settings.backup.lastRestore.failed", { reason: last.message ?? "" })}
              </Alert>
            )}
            <Todo items={last.todo} />
          </div>
        ) : null}
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
          <ActionButton
            tone="primary"
            onClick={() => {
              setOpen("create");
              void creating.run();
            }}
            data-testid="backup-now"
          >
            <HardDriveDownload aria-hidden="true" className="size-4" />
            {t("settings.backup.now")}
          </ActionButton>
          <ActionButton
            tone="outline"
            disabled={s.restoring}
            onClick={() => setOpen("restore")}
            data-testid="backup-restore-open"
          >
            <History aria-hidden="true" className="size-4" />
            {t("settings.backup.restore")}
          </ActionButton>
          <ActionButton
            tone="outline"
            onClick={() => setOpen("move")}
            data-testid="backup-move-open"
          >
            <MonitorSmartphone aria-hidden="true" className="size-4" />
            {t("settings.backup.move")}
          </ActionButton>
        </div>
      </div>
      <CreateDialog open={open === "create"} onClose={close} c={creating} />
      <RestoreDialog open={open === "restore"} onClose={close} status={s} />
      <MoveDialog open={open === "move"} onClose={close} />
    </>
  );
}
