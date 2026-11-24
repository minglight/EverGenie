import { Spinner } from "@evergenie/ui";
import { useId, useState } from "react";
import { Card, LoadError, RefreshButton, Row, TaskButton, useLoad } from "./lib/parts";
import {
  deployBody,
  fill,
  KIND_PREFIX,
  type Next,
  nightlyBody,
  noSafeHint,
  pendingNote,
  type Release,
  safeLabel,
  stableBody,
  T,
  versionProblem,
  whenText,
} from "./lib/release";
import { type Desk, loadDesk, loadRelease } from "./lib/store";

const POLL_MS = 15_000;

const fieldClass =
  "w-full rounded-xl border border-border-strong bg-bg px-3 py-2.5 text-base leading-snug text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";

// 發版：這台的版本、main 最新與還沒部署的改動（部署鈕）、正式版（發版測試鈕、發正式版表單）。
// 頁面不跑 git、不讀 repo：只讀 admin 程式算好的 admin.release；按鈕開任務單給 admin 秘書（計畫 2026-10-31-dev-workshop-split）。
export default function ReleasePage() {
  const release = useLoad(loadRelease, POLL_MS);
  const desk = useLoad(loadDesk, POLL_MS);
  const r = release.data;
  return (
    <div data-testid="admin-release" className="flex flex-col gap-4">
      {/* 頁名由外殼的頁首寫（套件選單的 title），這裡不再放一個同名的 h1。 */}
      <header className="flex flex-col gap-1">
        <p className="text-sm text-muted">{T.releaseSub}</p>
      </header>
      <LoadError error={release.error || desk.error} />
      {r === undefined && !release.error ? (
        <div className="flex justify-center py-8">
          <Spinner label={T.loading} />
        </div>
      ) : r === null ? (
        <p className="text-sm text-muted">{T.notYet}</p>
      ) : r ? (
        <>
          <Versions r={r} desk={desk.data} onOpened={desk.reload} />
          <Stable r={r} desk={desk.data} onOpened={desk.reload} />
          {r.computedAt ? (
            <p className="text-xs text-muted">
              {fill(T.computedAt, { at: whenText(r.computedAt) })}
            </p>
          ) : null}
        </>
      ) : null}
      <RefreshButton
        target="release"
        onDone={() => {
          release.reload();
          desk.reload();
        }}
      />
    </div>
  );
}

function Versions({
  r,
  desk,
  onOpened,
}: {
  r: Release;
  desk: Desk | undefined;
  onOpened: () => void;
}) {
  const main = r.main;
  return (
    <Card title={T.versions} testId="admin-versions">
      <Row label={T.hereVersion}>{r.version || T.versionUnknown}</Row>
      {r.detail ? <p className="text-[13px] text-attention-fg">{r.detail}</p> : null}
      <Row label={T.main}>{main ? `${main.short} ${main.subject}` : T.mainNone}</Row>
      <div className="flex flex-col gap-1">
        <h3 className="text-sm font-medium">{T.pending}</h3>
        {pendingNote(r) ? (
          <p className="text-[13px] text-muted">{pendingNote(r)}</p>
        ) : (
          <ul data-testid="admin-pending" className="flex flex-col gap-0.5 text-[13px]">
            {r.pending.map((c) => (
              <li key={c.commit}>
                <code className="text-muted">{c.short}</code> {c.subject}
              </li>
            ))}
            {r.pendingMore ? <li className="text-muted">{T.pendingMore}</li> : null}
          </ul>
        )}
      </div>
      {main && r.pending.length > 0 ? (
        <TaskButton
          desk={desk}
          prefix={KIND_PREFIX.deploy}
          label={T.deploy}
          confirm={T.deployConfirm}
          yes={T.deployYes}
          title={fill(T.deployTitle, { short: main.short })}
          body={() => deployBody(main, r.pending)}
          onOpened={onOpened}
          testId="admin-deploy"
        />
      ) : null}
    </Card>
  );
}

function Stable({
  r,
  desk,
  onOpened,
}: {
  r: Release;
  desk: Desk | undefined;
  onOpened: () => void;
}) {
  const n = r.nightly;
  const safeIsLatest = !!r.safe && !!r.latest && r.safe.commit === r.latest.commit;
  return (
    <Card title={T.stable} testId="admin-stable">
      <p className="text-[13px] text-muted">{T.stableSub}</p>
      <Row label={T.latest}>
        {r.latest
          ? [r.latest.version || r.latest.name, whenText(r.latest.at)].filter(Boolean).join(" · ")
          : T.latestNone}
      </Row>
      <Row label={T.safe}>
        {r.safe
          ? `${safeLabel(r.safe.name)} · ${r.safe.short} · ${safeIsLatest ? T.safeSame : r.latest ? T.safeNewer : whenText(r.safe.at)}`
          : T.safeNone}
      </Row>
      <Row label={T.nightly}>
        {n
          ? [n.short, whenText(n.at), n.ok ? T.nightlyOk : T.nightlyBad].filter(Boolean).join(" · ")
          : T.nightlyNone}
      </Row>
      {n && n.steps.length > 0 ? (
        <ul className="flex flex-col gap-0.5 text-[13px]">
          {n.steps.map((s) => (
            <li key={s.name} className={s.ok ? "text-muted" : "text-danger"}>
              {fill(s.ok ? T.nightlyStep : T.nightlyStepBad, { name: s.name, seconds: s.seconds })}
            </li>
          ))}
        </ul>
      ) : null}
      <p className="text-[13px] text-muted">{T.nightlyHint}</p>
      <TaskButton
        desk={desk}
        prefix={KIND_PREFIX.nightly}
        label={T.nightlyRun}
        confirm={T.nightlyConfirm}
        yes={T.nightlyYes}
        title={T.nightlyTitle}
        body={nightlyBody}
        onOpened={onOpened}
        testId="admin-nightly"
      />
      {!r.safe || safeIsLatest ? (
        noSafeHint(desk) ? (
          <p className="text-[13px] text-muted">{noSafeHint(desk)}</p>
        ) : null
      ) : !r.next ? (
        <p className="text-[13px] text-muted">{T.noNext}</p>
      ) : (
        // 草稿換了（admin 秘書把說明改寫成白話、或新的測試過版本）就重新掛表單，不留著舊的初值（DEV-113）
        <StableForm
          key={`${r.next.safeTag}|${r.next.version}|${r.next.notes}`}
          r={r}
          next={r.next}
          desk={desk}
          onOpened={onOpened}
        />
      )}
    </Card>
  );
}

// 下一版的草稿：版號與發版說明都可以改；確定了才開一張發正式版的單。
function StableForm({
  r,
  next,
  desk,
  onOpened,
}: {
  r: Release;
  next: Next;
  desk: Desk | undefined;
  onOpened: () => void;
}) {
  const id = useId();
  const [version, setVersion] = useState(next.version);
  const [notes, setNotes] = useState(next.notes);
  const v = version.trim();
  const problem = versionProblem(v, r.latest?.version ?? "");
  const safe = r.safe;
  const bad = v === "" || problem !== "" || notes.trim() === "" || !safe;
  return (
    <div className="flex flex-col gap-3" data-testid="admin-stable-form">
      <label htmlFor={`${id}-v`} className="flex flex-col gap-1 text-sm">
        <span className="font-medium">{T.version}</span>
        <input
          id={`${id}-v`}
          className={fieldClass}
          value={version}
          onChange={(e) => setVersion(e.target.value)}
        />
        <span className={problem ? "text-[13px] text-danger" : "text-[13px] text-muted"}>
          {problem || T.versionHint}
        </span>
      </label>
      <label htmlFor={`${id}-n`} className="flex flex-col gap-1 text-sm">
        <span className="font-medium">{T.notes}</span>
        <textarea
          id={`${id}-n`}
          rows={6}
          className={fieldClass}
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
        />
        <span
          className={notes.trim() === "" ? "text-[13px] text-danger" : "text-[13px] text-muted"}
        >
          {notes.trim() === ""
            ? T.notesEmpty
            : next.draft && notes === next.notes
              ? T.notesDraft
              : T.notesHint}
        </span>
      </label>
      <TaskButton
        desk={desk}
        prefix={KIND_PREFIX.stable}
        label={fill(T.stableGo, { version: v })}
        confirm={fill(T.stableConfirm, {
          safe: safe ? safeLabel(safe.name) : "",
          version: v,
          name: "{name}",
        })}
        yes={T.stableYes}
        title={fill(T.stableTitle, { version: v })}
        body={() => stableBody(v, safe?.name ?? "", safe?.commit ?? "", notes)}
        disabled={bad}
        onOpened={onOpened}
        testId="admin-stable-go"
      />
    </div>
  );
}
