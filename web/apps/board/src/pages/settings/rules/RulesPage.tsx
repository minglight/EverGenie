import {
  ApiError,
  api,
  type RulesDiffLine,
  type RulesDoc,
  type RulesProposal,
  type RulesVersion,
} from "@evergenie/sdk-web";
import { Card, cn, EmptyState, Markdown, Segmented } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronDown, FileText, History, Pencil } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ActionButton } from "../../../components/ActionButton";
import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { LoadFailed, LoadingBlock } from "../../../components/StateViews";
import { useAgents } from "../../../data/agents";
import { t } from "../../../i18n/zh-TW";
import { useDraft } from "../../../lib/draft";
import { clockText, parseIso } from "../../../lib/format";
import {
  rulesDocQuery,
  rulesHistoryQuery,
  rulesListQuery,
  rulesPendingQuery,
  rulesVersionQuery,
} from "./queries";
import { collapseDiff, diffCounts, sourceKey, splitItems } from "./rulesText";

// 規矩頁（S7、D-44）：家裡的憲法、宗旨、法律。看＝一條一條白話呈現；改＝只有主人（看板一律是主人）；
// 每次改都留版本，歷史可以比較、還原；秘書的修改提案在最上面，採用或退回。

const inputClass =
  "w-full rounded-xl border border-border-strong bg-bg px-3 py-2.5 text-base leading-snug text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";

function dateText(iso: string | undefined): string {
  const d = parseIso(iso);
  if (!d) return "";
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")} ${clockText(iso)}`;
}

function failText(err: unknown): string {
  return t("rules.failed", { reason: err instanceof ApiError ? err.message : String(err) });
}

function whoText(by: string | undefined, name: (id: string) => string): string {
  if (!by) return "";
  if (by.startsWith("owner(")) return t("rules.by.owner");
  if (by === "system") return t("rules.by.system");
  return name(by);
}

/** 差異：加上的綠底「＋」、拿掉的紅底「－」，沒變的收起來。 */
function DiffView({ diff, label }: { diff: RulesDiffLine[]; label: string }) {
  const { add, del } = diffCounts(diff);
  if (add === 0 && del === 0) {
    return <p className="text-[13px] text-muted">{t("rules.diff.none")}</p>;
  }
  return (
    <figure className="m-0 flex flex-col gap-1">
      <figcaption className="text-xs font-semibold text-muted">
        {label}　{t("rules.diff.counts", { add, del })}
      </figcaption>
      <section
        aria-label={label}
        className="overflow-x-auto rounded-xl border border-border bg-bg text-[13px] leading-relaxed"
      >
        {collapseDiff(diff).map((seg, i) =>
          seg.kind === "skip" ? (
            <p key={`s${seg.key}`} className="m-0 px-3 py-1 text-xs text-muted">
              {t("rules.diff.skip", { n: seg.count })}
            </p>
          ) : (
            <p
              // biome-ignore lint/suspicious/noArrayIndexKey: 差異的行沒有自己的 id，順序固定
              key={i}
              data-op={seg.line.op}
              className={cn(
                "m-0 px-3 py-0.5 break-words whitespace-pre-wrap",
                seg.line.op === "add" && "bg-ok-soft",
                seg.line.op === "del" && "bg-danger-soft text-muted line-through decoration-1",
              )}
            >
              <span aria-hidden="true" className="mr-2 inline-block w-3 font-mono">
                {seg.line.op === "add" ? "+" : seg.line.op === "del" ? "−" : " "}
              </span>
              <span className="sr-only">
                {seg.line.op === "add"
                  ? t("rules.diff.add")
                  : seg.line.op === "del"
                    ? t("rules.diff.del")
                    : ""}
              </span>
              {seg.line.text || " "}
            </p>
          ),
        )}
      </section>
    </figure>
  );
}

/** 讀的樣子：一條一條，可以收合；開頭沒有標題的那段直接顯示。 */
function ReadView({ text }: { text: string }) {
  const items = splitItems(text);
  if (items.length === 0) {
    return <EmptyState title={t("rules.doc.blank")} hint={t("rules.doc.blank.hint")} />;
  }
  return (
    <div className="flex flex-col gap-2">
      {items.map((item, i) =>
        item.heading === "" ? (
          // biome-ignore lint/suspicious/noArrayIndexKey: 條文沒有 id，順序就是位置
          <Markdown key={i} className="px-1 text-muted">
            {item.body}
          </Markdown>
        ) : (
          // biome-ignore lint/suspicious/noArrayIndexKey: 條文沒有 id，順序就是位置
          <details key={i} open className="group rounded-xl border border-border bg-surface">
            <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-2 px-3 py-2 text-[15px] font-semibold focus-visible:outline-2 focus-visible:outline-focus">
              <span className="min-w-0 break-words">{item.heading}</span>
              <ChevronDown
                aria-hidden="true"
                className="size-4 flex-none text-muted transition-transform group-open:rotate-180"
              />
            </summary>
            {item.body ? (
              <Markdown className="border-t border-border px-3 py-2">{item.body}</Markdown>
            ) : null}
          </details>
        ),
      )}
    </div>
  );
}

const draftKey = (doc: string | undefined) => `eg.draft.rules.${doc ?? ""}`;

/**
 * 編輯：草稿與「開始改的時候是哪一版」（revision）一起存 localStorage（FEN-23）：離開再回來接著改；
 * 存檔一律帶開始改時的 revision，中途文件被別處改過（別的分頁、採用提案、在電腦上直接改）就會回衝突，不會蓋掉。
 */
function EditForm({ doc, text, onDone }: { doc: RulesDoc; text: string; onDone: () => void }) {
  const client = useQueryClient();
  const key = draftKey(doc.id);
  const [savedDraft, keepDraft, clearDraft] = useDraft(key);
  const [savedBase, keepBase, clearBase] = useDraft(`${key}.base`);
  const [resumed] = useState(savedDraft !== "");
  const [original] = useState(text);
  const [base] = useState(resumed ? savedBase : (doc.revision ?? ""));
  const [draft, setDraft] = useState(resumed ? savedDraft : text);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const [discard, setDiscard] = useState(false);
  const dirty = draft !== original || resumed;
  const forget = () => {
    clearDraft();
    clearBase();
  };
  const leave = () => {
    forget();
    // 衝突過的文件在別處改了（看板外改檔不會有即時更新），重新讀最新的內容。
    void client.invalidateQueries({ queryKey: ["rules"] });
    onDone();
  };
  const save = async () => {
    setBusy(true);
    setError("");
    setConflict(false);
    try {
      await api.rules.save({ doc: doc.id, text: draft, revision: base, note });
      forget();
      await client.invalidateQueries({ queryKey: ["rules"] });
      onDone();
    } catch (err) {
      setError(failText(err));
      setConflict(err instanceof ApiError && err.code === "conflict");
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      {resumed ? (
        <p role="note" className="m-0 rounded-xl bg-accent-soft px-3 py-2 text-[14px]">
          {t("rules.edit.resumed")}
        </p>
      ) : null}
      <label className="flex flex-col gap-1">
        <span className="text-xs font-semibold text-muted">{t("rules.edit.text")}</span>
        <textarea
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value);
            keepDraft(e.target.value);
            keepBase(base);
          }}
          rows={18}
          spellCheck={false}
          className={cn(inputClass, "min-h-[50vh] font-mono text-[14px] leading-relaxed")}
        />
      </label>
      <label className="flex flex-col gap-1">
        <span className="text-xs font-semibold text-muted">{t("rules.edit.note")}</span>
        <input
          value={note}
          onChange={(e) => setNote(e.target.value)}
          maxLength={500}
          placeholder={t("rules.edit.note.placeholder")}
          className={inputClass}
        />
      </label>
      <p className="m-0 text-[13px] text-muted">{t("rules.edit.hint")}</p>
      {error ? (
        <div role="alert" className="flex flex-col gap-2 text-[14px] text-danger">
          <p className="m-0">{error}</p>
          {conflict ? <p className="m-0 text-muted">{t("rules.edit.conflict")}</p> : null}
        </div>
      ) : null}
      <div className="flex flex-wrap justify-end gap-2">
        <ActionButton
          tone="outline"
          size="page"
          onClick={() => (dirty ? setDiscard(true) : leave())}
          disabled={busy}
        >
          {conflict ? t("rules.edit.restart") : t("rules.edit.cancel")}
        </ActionButton>
        <ActionButton type="submit" size="page" busy={busy} disabled={busy || draft === original}>
          {t("rules.edit.save")}
        </ActionButton>
      </div>
      <ConfirmDialog
        open={discard}
        title={t("rules.edit.discard.title")}
        body={t("rules.edit.discard.body")}
        confirmLabel={t("rules.edit.discard.yes")}
        cancelLabel={t("rules.edit.discard.no")}
        onConfirm={() => {
          setDiscard(false);
          leave();
        }}
        onCancel={() => setDiscard(false)}
      />
    </form>
  );
}

/** 一版的詳情：跟前一版的差異（或跟現在比）、還原。 */
function VersionDetail({ version, doc }: { version: RulesVersion; doc: RulesDoc }) {
  const client = useQueryClient();
  // 打開這一版時的文件是哪一版：還原時帶著，中途文件被別處改過就回衝突。
  const [base] = useState(doc.revision ?? "");
  const [against, setAgainst] = useState<"" | "current">("");
  // 跟現在一樣就不用還原（看「跟現在比」有沒有差，不是看是不是最新的一版：在電腦上直接改過時兩者不同）。
  const vsCurrent = useQuery(rulesVersionQuery(version.id ?? "", "current"));
  const same =
    vsCurrent.data !== undefined &&
    diffCounts(vsCurrent.data.diff ?? []).add + diffCounts(vsCurrent.data.diff ?? []).del === 0;
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const q = useQuery(rulesVersionQuery(version.id ?? "", against));
  const restore = async () => {
    setConfirm(false);
    setBusy(true);
    setError("");
    try {
      await api.rules.restore({ id: version.id, revision: base });
      await client.invalidateQueries({ queryKey: ["rules"] });
    } catch (err) {
      setError(failText(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col gap-3 border-t border-border px-3 py-3">
      <Segmented
        ariaLabel={t("rules.version.compare")}
        value={against}
        onChange={setAgainst}
        options={[
          { value: "", label: t("rules.version.vsPrev") },
          { value: "current", label: t("rules.version.vsCurrent") },
        ]}
      />
      {q.isPending ? (
        <LoadingBlock />
      ) : q.isError ? (
        <p role="alert" className="text-[14px] text-danger">
          {failText(q.error)}
        </p>
      ) : (
        <DiffView
          diff={q.data.diff ?? []}
          label={
            against === "current"
              ? t("rules.version.diffCurrent", { id: version.id ?? "" })
              : t("rules.version.diffPrev", { id: version.id ?? "" })
          }
        />
      )}
      {error ? (
        <p role="alert" className="text-[14px] text-danger">
          {error}
        </p>
      ) : null}
      {same ? (
        <p className="text-[13px] text-muted">{t("rules.version.isCurrent")}</p>
      ) : (
        <div className="flex justify-end">
          <ActionButton tone="outline" size="page" busy={busy} onClick={() => setConfirm(true)}>
            {t("rules.version.restore")}
          </ActionButton>
        </div>
      )}
      <ConfirmDialog
        open={confirm}
        tone="primary"
        title={t("rules.version.restore.title", { id: version.id ?? "" })}
        body={t("rules.version.restore.body")}
        confirmLabel={t("rules.version.restore.yes")}
        cancelLabel={t("rules.version.restore.no")}
        onConfirm={() => void restore()}
        onCancel={() => setConfirm(false)}
      />
    </div>
  );
}

function HistoryView({ doc }: { doc: RulesDoc }) {
  const { view } = useAgents();
  const q = useQuery(rulesHistoryQuery(doc.id ?? ""));
  const [open, setOpen] = useState("");
  if (q.isPending) return <LoadingBlock />;
  if (q.isError) return <LoadFailed what={t("rules.history")} onRetry={() => void q.refetch()} />;
  const versions = q.data;
  if (versions.length === 0) {
    return <EmptyState title={t("rules.history.empty")} hint={t("rules.history.empty.hint")} />;
  }
  return (
    <ol aria-label={t("rules.history")} className="m-0 flex list-none flex-col gap-2 p-0">
      {versions.map((v) => {
        const on = open === v.id;
        return (
          <li key={v.id} className="rounded-xl border border-border bg-surface">
            <button
              type="button"
              aria-expanded={on}
              onClick={() => setOpen(on ? "" : (v.id ?? ""))}
              className="flex min-h-11 w-full flex-col items-start gap-0.5 px-3 py-2 text-left focus-visible:outline-2 focus-visible:outline-focus"
            >
              <span className="flex w-full flex-wrap items-center gap-x-2 text-[14px] font-semibold">
                <span className="font-mono text-[13px]">{v.id}</span>
                <span>{t(sourceKey[v.source ?? "save"])}</span>
                <span className="ml-auto text-xs font-normal text-muted">
                  {dateText(v.created_at)}
                </span>
              </span>
              <span className="text-[13px] break-words text-muted">
                {[
                  whoText(v.by, (id) => view(id).name),
                  v.note === t(sourceKey[v.source ?? "save"]) ? "" : v.note,
                ]
                  .filter(Boolean)
                  .join("　")}
              </span>
            </button>
            {on ? <VersionDetail version={v} doc={doc} /> : null}
          </li>
        );
      })}
    </ol>
  );
}

type Mode = "read" | "edit" | "history";

function DocView({ doc }: { doc: RulesDoc }) {
  const [hasDraft] = useDraft(draftKey(doc.id));
  const [mode, setMode] = useState<Mode>(() => (hasDraft ? "edit" : "read"));
  const q = useQuery(rulesDocQuery(doc.id ?? ""));
  const where = doc.section ? `${doc.path}#${doc.section}` : doc.path;
  return (
    <Card className="flex flex-col gap-3 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="m-0 text-[17px] font-semibold">{doc.title}</h2>
          <p className="m-0 font-mono text-xs break-all text-muted">{where}</p>
        </div>
        {mode === "read" ? (
          <div className="flex gap-2">
            <ActionButton tone="outline" size="page" onClick={() => setMode("history")}>
              <History aria-hidden="true" className="size-4" />
              {t("rules.history")}
            </ActionButton>
            <ActionButton
              tone="secondary"
              size="page"
              onClick={() => setMode("edit")}
              disabled={!q.data}
            >
              <Pencil aria-hidden="true" className="size-4" />
              {t("rules.edit")}
            </ActionButton>
          </div>
        ) : mode === "history" ? (
          <ActionButton tone="outline" size="page" onClick={() => setMode("read")}>
            {t("rules.back")}
          </ActionButton>
        ) : null}
      </div>
      {doc.problem ? (
        <p
          role="alert"
          className="m-0 rounded-xl bg-attention-soft px-3 py-2 text-[14px] break-words"
        >
          {doc.problem}
        </p>
      ) : null}
      {q.isPending ? (
        <LoadingBlock />
      ) : q.isError ? (
        <LoadFailed what={doc.title ?? ""} onRetry={() => void q.refetch()} />
      ) : mode === "edit" ? (
        <EditForm doc={q.data.doc ?? doc} text={q.data.text ?? ""} onDone={() => setMode("read")} />
      ) : mode === "history" ? (
        <HistoryView doc={q.data.doc ?? doc} />
      ) : !q.data.doc?.exists ? (
        <EmptyState
          tone="missing"
          title={t("rules.doc.missing")}
          hint={t("rules.doc.missing.hint", { where: where ?? "" })}
        />
      ) : (
        <ReadView text={q.data.text ?? ""} />
      )}
    </Card>
  );
}

function ProposalCard({
  p,
  title,
  highlight,
}: {
  p: RulesProposal;
  title: string;
  highlight: boolean;
}) {
  const client = useQueryClient();
  const { view } = useAgents();
  const ref = useRef<HTMLDivElement>(null);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState<"" | "accept" | "reject">("");
  const [error, setError] = useState("");
  useEffect(() => {
    if (highlight) ref.current?.scrollIntoView({ block: "center" });
  }, [highlight]);
  const decide = async (accept: boolean) => {
    setBusy(accept ? "accept" : "reject");
    setError("");
    try {
      await api.rules.decide({ id: p.id, accept, note });
      await client.invalidateQueries({ queryKey: ["rules"] });
    } catch (err) {
      setError(failText(err));
    } finally {
      setBusy("");
    }
  };
  const name = view(p.from).name;
  return (
    <div ref={ref}>
      <Card
        role="group"
        aria-label={t("rules.proposal.label", { id: p.id ?? "" })}
        className={cn("flex flex-col gap-3 p-3", highlight && "outline-2 outline-accent")}
      >
        <div className="flex flex-col gap-0.5">
          <p className="m-0 text-[15px] font-semibold break-words">
            {p.section
              ? t("rules.proposal.titleSection", { name, title, section: p.section })
              : t("rules.proposal.title", { name, title })}
          </p>
          <p className="m-0 text-xs text-muted">
            {p.id}　{dateText(p.created_at)}
          </p>
        </div>
        <div className="flex flex-col gap-0.5">
          <p className="m-0 text-xs font-semibold text-muted">{t("rules.proposal.why")}</p>
          <p className="m-0 text-[14px] leading-relaxed break-words whitespace-pre-wrap">{p.why}</p>
        </div>
        <DiffView diff={p.diff ?? []} label={t("rules.proposal.diff")} />
        {p.stale ? (
          <p role="note" className="m-0 rounded-xl bg-attention-soft px-3 py-2 text-[14px]">
            {t("rules.proposal.stale")}
          </p>
        ) : null}
        <label className="flex flex-col gap-1">
          <span className="text-xs font-semibold text-muted">{t("rules.proposal.note")}</span>
          <input value={note} onChange={(e) => setNote(e.target.value)} className={inputClass} />
        </label>
        {error ? (
          <p role="alert" className="m-0 text-[14px] text-danger">
            {error}
          </p>
        ) : null}
        <div className="flex flex-wrap justify-end gap-2">
          <ActionButton
            tone="outline"
            size="page"
            busy={busy === "reject"}
            disabled={busy !== ""}
            onClick={() => void decide(false)}
          >
            {t("rules.proposal.reject")}
          </ActionButton>
          <ActionButton
            size="page"
            busy={busy === "accept"}
            disabled={busy !== "" || p.stale}
            onClick={() => void decide(true)}
          >
            {t("rules.proposal.accept")}
          </ActionButton>
        </div>
      </Card>
    </div>
  );
}

export function RulesPage() {
  const search = useSearch({ from: "/settings/rules" });
  const navigate = useNavigate({ from: "/settings/rules" });
  const list = useQuery(rulesListQuery());
  const pending = useQuery(rulesPendingQuery());
  if (list.isPending) return <LoadingBlock />;
  if (list.isError)
    return <LoadFailed what={t("rules.what")} onRetry={() => void list.refetch()} />;
  const docs = list.data.docs ?? [];
  const titleOf = (id: string | undefined) => docs.find((d) => d.id === id)?.title ?? id ?? "";
  const proposals = pending.data ?? [];
  const current = docs.find((d) => d.id === search.doc) ?? docs[0];
  return (
    <div className="flex flex-col gap-4">
      {list.data.problem ? (
        <p
          role="alert"
          className="m-0 rounded-xl bg-attention-soft px-3 py-2 text-[14px] break-words"
        >
          {t("rules.problem", { problem: list.data.problem })}
        </p>
      ) : null}
      {proposals.length > 0 ? (
        <section aria-label={t("rules.proposals")} className="flex flex-col gap-2">
          <h2 className="m-0 px-1 text-[15px] font-semibold">
            {t("rules.proposals.count", { n: proposals.length })}
          </h2>
          {proposals.map((p) => (
            <ProposalCard
              key={p.id}
              p={p}
              title={titleOf(p.doc)}
              highlight={p.id === search.proposal}
            />
          ))}
        </section>
      ) : null}
      {docs.length === 0 ? (
        <EmptyState
          title={t("rules.empty")}
          hint={
            <span className="inline-flex items-center gap-1">
              <FileText aria-hidden="true" className="size-4" />
              {t("rules.empty.hint")}
            </span>
          }
        />
      ) : (
        <>
          {docs.length > 1 ? (
            <Segmented
              scroll
              ariaLabel={t("rules.docs")}
              value={current?.id ?? ""}
              onChange={(doc) => void navigate({ search: (s) => ({ ...s, doc }), replace: true })}
              options={docs.map((d) => ({
                value: d.id ?? "",
                label: d.title ?? d.id ?? "",
                count: d.pending || undefined,
              }))}
            />
          ) : null}
          <p className="m-0 px-1 text-[13px] text-muted">{t("rules.rank")}</p>
          {current ? <DocView key={current.id} doc={current} /> : null}
        </>
      )}
    </div>
  );
}
