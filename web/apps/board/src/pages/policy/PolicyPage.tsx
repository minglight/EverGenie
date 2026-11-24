import type { PolicyRequest } from "@evergenie/sdk-web";
import { Card, cn, EmptyState, Field, Segmented, showToast } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { ShieldAlert } from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { Pill } from "../../components/Pill";
import { SiteProblems } from "../../components/SiteNotice";
import { ItemSiteTag } from "../../components/SiteTag";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import {
  failedSites,
  flattenParts,
  itemKey,
  type OnSite,
  sumParts,
  useAcrossSites,
} from "../../data/acrossSites";
import { useAgents } from "../../data/agents";
import { apiFor, type Site, siteKey } from "../../data/sites";
import { type TextKey, t } from "../../i18n/zh-TW";
import { clockText, parseIso, relativeAgo } from "../../lib/format";
import { knownText } from "../../lib/knownText";
import { useIsDesktop } from "../../lib/media";
import { useNow } from "../../lib/now";
import {
  actionText,
  decidedToastKey,
  failedToast,
  isNarrower,
  statusKey,
  statusTone,
} from "./policyText";
import { type PolicyView, policyQuery, policyViews } from "./queries";

const viewKeys = {
  pending: "policy.view.pending",
  applied: "policy.view.applied",
  history: "policy.view.history",
} as const satisfies Record<PolicyView, TextKey>;

const emptyKeys = {
  pending: "policy.empty.pending",
  applied: "policy.empty.applied",
  history: "policy.empty.history",
} as const satisfies Record<PolicyView, TextKey>;

const inputClass =
  "w-full rounded-xl border border-border-strong bg-bg px-3 py-2.5 text-base leading-snug text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";

function dateText(iso: string | undefined): string {
  const d = parseIso(iso);
  if (!d) return "";
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")} ${clockText(iso)}`;
}

// 送出失敗用 toast 說原因（POL-15，failedToast）。
function toastFailed(err: unknown) {
  const { text, durationMs } = failedToast(err);
  showToast(text, { durationMs });
}

/** 申請的主旨：rule＝誰、做什麼、哪裡；guidance＝那句話。 */
function Subject({ q, site }: { q: PolicyRequest; site: Site }) {
  const { view } = useAgents(site);
  if (q.kind === "guidance") {
    return (
      <p className="text-[15px] leading-[1.45] font-semibold break-words">
        {t("policy.kind.guidance")}：「{q.text}」
      </p>
    );
  }
  return (
    <p className="text-[15px] leading-[1.45] font-semibold break-words">
      {t("policy.rule.subject", {
        name: view(q.agent).name,
        action: t(actionText(q.action)),
      })}
      <span className="font-mono text-[13px] font-medium break-all">
        {q.scope || t("policy.scope.anywhere")}
      </span>
    </p>
  );
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs font-semibold text-muted">{label}</dt>
      <dd className="m-0 text-[14px] leading-relaxed break-words">{children}</dd>
    </div>
  );
}

// 按完那張卡會從目前的檢視消失：一句話說結果（POL-14）；套用失敗留久一點。
function toastDecided(r: PolicyRequest | undefined, id: string | undefined) {
  const key = decidedToastKey(r);
  if (!key) return;
  showToast(t(key, { id: r?.id ?? id ?? "" }), {
    durationMs: r?.status === "failed" ? 10000 : undefined,
  });
}

/** 待審：核准（可改窄範圍或改一句話、備註；核准後一直有效）／拒絕。 */
function DecideForm({ q, site, onDone }: { q: PolicyRequest; site: Site; onDone: () => void }) {
  const [edit, setEdit] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState<"" | "approve" | "reject">("");
  const isRule = q.kind !== "guidance";
  const editBad = isRule && edit.trim() !== "" && !isNarrower(edit, q.scope);
  const send = async (approve: boolean) => {
    setBusy(approve ? "approve" : "reject");
    try {
      // 送到申請所在的台（S3-7b）：核准的是那台秘書的權限。
      const res = await apiFor(site).policy.decide({
        id: q.id,
        approve,
        // 生成的型別要求這個欄位；Q7b 起核准一律一直有效，拒絕時不能帶。
        permanent: approve,
        note: note.trim() || undefined,
        scope: approve && isRule ? edit.trim() || undefined : undefined,
        text: approve && !isRule ? edit.trim() || undefined : undefined,
        fingerprint: q.fingerprint,
      });
      toastDecided(res.request, q.id);
      onDone();
    } catch (err) {
      toastFailed(err);
      // 指紋過期（內容或有效規則變了）、已在別處決定：重新抓一次給主人重看。
      onDone();
    } finally {
      setBusy("");
    }
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    void send(true);
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-3 border-t border-border pt-3">
      <Field
        label={isRule ? t("policy.edit.scope") : t("policy.edit.text")}
        hint={isRule ? t("policy.edit.scope.hint") : t("policy.edit.text.hint")}
        error={editBad ? t("policy.edit.scope.bad") : undefined}
      >
        {(control) => (
          <input
            {...control}
            value={edit}
            onChange={(e) => setEdit(e.target.value)}
            placeholder={isRule ? q.scope || "~/Workspace/…" : q.text}
            className={inputClass}
          />
        )}
      </Field>
      <Field label={t("policy.note")}>
        {(control) => (
          <input
            {...control}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            className={inputClass}
          />
        )}
      </Field>
      <div className="flex flex-wrap gap-2">
        <ActionButton
          type="submit"
          tone="primary"
          size="card"
          busy={busy === "approve"}
          disabled={busy !== "" || editBad}
          className="flex-1"
        >
          {t("policy.approve")}
        </ActionButton>
        <ActionButton
          tone="outline"
          size="card"
          busy={busy === "reject"}
          disabled={busy !== ""}
          onClick={() => void send(false)}
        >
          {t("policy.reject")}
        </ActionButton>
      </div>
    </form>
  );
}

/** 已授權：撤銷（按一次展開確認）。 */
function RevokeButton({ q, site, onDone }: { q: PolicyRequest; site: Site; onDone: () => void }) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const revoke = async () => {
    setBusy(true);
    try {
      const res = await apiFor(site).policy.revoke({ id: q.id });
      toastDecided(res.request, q.id);
      onDone();
    } catch (err) {
      toastFailed(err);
      // 可能已在別處撤銷：重抓，卡片照現況。
      onDone();
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col gap-2 border-t border-border pt-3">
      {confirming ? (
        <div className="flex flex-wrap items-center gap-2">
          <p className="w-full text-[13px] text-muted">{t("policy.revoke.confirm")}</p>
          <ActionButton tone="danger" size="card" busy={busy} onClick={() => void revoke()}>
            {t("policy.revoke.yes")}
          </ActionButton>
          <ActionButton tone="quiet" size="card" onClick={() => setConfirming(false)}>
            {t("ui.cancel")}
          </ActionButton>
        </div>
      ) : (
        <ActionButton tone="outline" size="card" onClick={() => setConfirming(true)}>
          {t("policy.revoke")}
        </ActionButton>
      )}
    </div>
  );
}

function RequestCard({ q, site, onDone }: { q: PolicyRequest; site: Site; onDone: () => void }) {
  const { view } = useAgents(site);
  const now = useNow();
  const status = q.status ?? "pending";
  const expiry =
    status === "applied"
      ? parseIso(q.expires_at)
        ? t("policy.expires.at", { at: dateText(q.expires_at) })
        : t("policy.expires.permanent")
      : "";
  return (
    <Card className="flex flex-col gap-3 p-4" data-testid="policy-request">
      <div className="flex flex-wrap items-center gap-1.5">
        <Pill tone={statusTone[status]}>{knownText(statusKey, status)}</Pill>
        <ItemSiteTag site={site} />
        <span className="font-mono text-xs text-muted">{q.id}</span>
        <span className="truncate text-[13px] text-muted">
          {t("policy.from", { name: view(q.from).name })}
        </span>
        <span className="ml-auto shrink-0 text-xs text-muted">
          {relativeAgo(q.created_at, now)}
        </span>
      </div>
      <Subject q={q} site={site} />
      {q.broad ? (
        <p
          role="note"
          className="flex items-start gap-2 rounded-xl bg-danger-soft px-3 py-2 text-[14px] font-semibold text-danger"
        >
          <ShieldAlert aria-hidden className="mt-0.5 size-4 shrink-0" />
          {t("policy.broad")}
        </p>
      ) : null}
      <dl className="m-0 flex flex-col gap-2.5">
        <Detail label={t("policy.impact")}>
          <span className={cn(q.broad && "text-danger")}>{q.impact}</span>
        </Detail>
        <Detail label={t("policy.why")}>{q.why}</Detail>
        {q.risk ? <Detail label={t("policy.risk")}>{q.risk}</Detail> : null}
        {expiry ? <Detail label={t("policy.expires")}>{expiry}</Detail> : null}
        {q.note ? <Detail label={t("policy.note")}>{q.note}</Detail> : null}
      </dl>
      {status === "pending" ? <DecideForm q={q} site={site} onDone={onDone} /> : null}
      {status === "applied" ? <RevokeButton q={q} site={site} onDone={onDone} /> : null}
    </Card>
  );
}

// 好幾台的申請合起來，新的在前（跟 newestFirst 同一個比法；sort 是穩定的，同時間照台的順序）。
function byNewest(rows: OnSite<PolicyRequest>[]): OnSite<PolicyRequest>[] {
  return [...rows].sort((a, b) => (b.item.created_at ?? "").localeCompare(a.item.created_at ?? ""));
}

// 權限申請（REQ-169、D-26）：?view= 待審／已授權／紀錄；核准、拒絕、撤銷都由主人在這裡按。
export function PolicyPage() {
  const desktop = useIsDesktop();
  const navigate = useNavigate();
  const client = useQueryClient();
  const { view: viewParam } = useSearch({ strict: false }) as { view?: PolicyView };
  const view: PolicyView = viewParam ?? "pending";
  // 每台各抓一份、合成一張清單，每張標台名（S3-7b）；只有一台時就是主伺服器那一份。
  const parts = useAcrossSites((site) => policyQuery(view, site));
  const primary = parts[0];
  const pendingCount = sumParts(
    useAcrossSites((site) => policyQuery("pending", site)),
    (list) => list.length,
  );
  const rows = useMemo(() => byNewest(flattenParts(parts)), [parts]);
  const refresh = (site: Site) =>
    void client.invalidateQueries({ queryKey: siteKey(site, "policy") });
  return (
    <div className="mx-auto flex max-w-[720px] flex-col gap-3.5 px-4 pt-3 pb-7">
      {desktop ? (
        <h1 className="pt-6 text-2xl font-bold tracking-[-0.01em]">{t("page.policy")}</h1>
      ) : null}
      <p className="text-[13px] leading-relaxed text-muted">{t("policy.intro")}</p>
      <Segmented
        options={policyViews.map((v) => ({
          value: v,
          label:
            v === "pending" && pendingCount ? `${t(viewKeys[v])} ${pendingCount}` : t(viewKeys[v]),
        }))}
        value={view}
        onChange={(next) =>
          void navigate({
            to: "/policy",
            search: { view: next === "pending" ? undefined : next },
            replace: true,
          })
        }
        ariaLabel={t("policy.views")}
        compact={desktop}
      />
      <SiteProblems what={t("page.policy")} failed={failedSites(parts)} />
      {primary?.data === undefined ? (
        primary?.isError ? (
          <LoadFailed what={t("page.policy")} onRetry={() => primary.refetch()} />
        ) : (
          <LoadingBlock />
        )
      ) : rows.length === 0 ? (
        <EmptyState title={t(emptyKeys[view])} hint={t("policy.empty.hint")} />
      ) : (
        <ul aria-label={t("policy.list")} className="m-0 flex list-none flex-col gap-3 p-0">
          {rows.map(({ site, item: q }) => (
            <li key={itemKey(site, q.id ?? "")}>
              <RequestCard q={q} site={site} onDone={() => refresh(site)} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
