import { api } from "@evergenie/sdk-web";
import { Button, EmptyState } from "@evergenie/ui";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { useAgents } from "../../data/agents";
import { notificationQuery, reminderQuery, taskQuery } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { dayGroupLabel, parseIso } from "../../lib/format";
import { uniqueKeys } from "../../lib/keys";
import { useNow } from "../../lib/now";
import { filterKinds, groupByDay, kindText, opText, targetOf, whoText } from "./logText";

const PAGE = 50;
const clock = new Intl.DateTimeFormat("zh-TW", {
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});
const fieldClass =
  "h-11 min-w-0 rounded-[10px] border border-border bg-bg px-3 text-base text-fg focus-visible:outline-2 focus-visible:outline-focus";

function Filter({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 text-xs text-muted">
      <span aria-hidden="true">{label}</span>
      {children}
    </div>
  );
}

// 編號旁邊的白話（UXC-2）：任務單、待辦、訊息讀它現在的標題（跟詳情頁共用快取）；
// 讀不到（已刪、已整理掉）或別的種類就不顯示，只留種類名＋編號。
function useTitle(kind: string | undefined, id: string | undefined): string {
  const key = id ?? "";
  const task = useQuery({ ...taskQuery(key), enabled: kind === "task" && key !== "" });
  const reminder = useQuery({ ...reminderQuery(key), enabled: kind === "reminder" && key !== "" });
  const message = useQuery({
    ...notificationQuery(key),
    enabled: kind === "message" && key !== "",
  });
  if (kind === "task") return task.data?.title ?? "";
  if (kind === "reminder") return reminder.data?.content ?? "";
  if (kind === "message") return message.data?.title ?? "";
  return "";
}

function RecordTitle({ kind, id }: { kind: string | undefined; id: string | undefined }) {
  const title = useTitle(kind, id);
  return title ? <span className="text-muted"> · {title}</span> : null;
}

// 設定 → 紀錄（REQ-111）：最近 30 天誰在家裡的資料上做了什麼（取代舊系統的 changes.log）。
// 新到舊，上面可以篩誰、種類、日期，底下「載入更早的」。
export function LogSettings() {
  const { all: list } = useAgents();
  const now = useNow();
  const [who, setWho] = useState("");
  const [kind, setKind] = useState("");
  const [since, setSince] = useState("");
  const [until, setUntil] = useState("");

  const q = useInfiniteQuery({
    queryKey: ["vault.changes", who, kind, since, until],
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      api.vault.changes({
        by: who || undefined,
        kind: kind || undefined,
        since: since || undefined,
        until: until || undefined,
        limit: PAGE,
        before: pageParam || undefined,
      }),
    getNextPageParam: (last) => last.next_before || undefined,
    staleTime: 10_000,
    refetchInterval: 30_000,
  });

  const nameOf = (id: string) => list.find((a) => a.id === id)?.name || undefined;
  const items = q.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const days = groupByDay(items);
  const keys = Object.fromEntries(
    days.map((d) => [
      d.day,
      uniqueKeys(d.items.map((it) => `${it.at}|${it.by}|${it.kind}|${it.id}|${it.op}`)),
    ]),
  );
  const filtered = who !== "" || kind !== "" || since !== "" || until !== "";

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 rounded-[18px] border border-border bg-surface p-3.5 shadow-[var(--shadow-card)] sm:grid-cols-4">
        <Filter label={t("settings.log.who")}>
          <select
            data-testid="log-who"
            aria-label={t("settings.log.who")}
            value={who}
            onChange={(e) => setWho(e.target.value)}
            className={fieldClass}
          >
            <option value="">{t("settings.log.all")}</option>
            <option value="owner">{t("settings.log.owner")}</option>
            {list.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name || a.id}
              </option>
            ))}
          </select>
        </Filter>
        <Filter label={t("settings.log.kind")}>
          <select
            data-testid="log-kind"
            aria-label={t("settings.log.kind")}
            value={kind}
            onChange={(e) => setKind(e.target.value)}
            className={fieldClass}
          >
            <option value="">{t("settings.log.all")}</option>
            {filterKinds.map((k) => (
              <option key={k} value={k}>
                {kindText(k)}
              </option>
            ))}
          </select>
        </Filter>
        <Filter label={t("settings.log.since")}>
          <input
            type="date"
            data-testid="log-since"
            aria-label={t("settings.log.since")}
            value={since}
            max={until || undefined}
            onChange={(e) => setSince(e.target.value)}
            className={fieldClass}
          />
        </Filter>
        <Filter label={t("settings.log.until")}>
          <input
            type="date"
            data-testid="log-until"
            aria-label={t("settings.log.until")}
            value={until}
            min={since || undefined}
            onChange={(e) => setUntil(e.target.value)}
            className={fieldClass}
          />
        </Filter>
      </div>

      {q.data === undefined ? (
        q.isError ? (
          <LoadFailed what={t("settings.loadFailed")} onRetry={() => void q.refetch()} />
        ) : (
          <LoadingBlock />
        )
      ) : days.length === 0 ? (
        <EmptyState
          title={t(filtered ? "settings.log.emptyFiltered" : "settings.log.empty")}
          hint={t(filtered ? "settings.log.emptyFiltered.hint" : "settings.log.empty.hint")}
        />
      ) : (
        <div className="flex flex-col gap-3">
          {days.map((d) => (
            <section key={d.day} aria-label={d.day} className="flex flex-col gap-1.5">
              <h2 className="px-1 text-[13px] font-semibold text-muted">
                {dayGroupLabel(d.at, now)}
              </h2>
              <ul className="m-0 flex list-none flex-col divide-y divide-border overflow-hidden rounded-[18px] border border-border bg-surface p-0 shadow-[var(--shadow-card)]">
                {d.items.map((it, i) => {
                  const key = keys[d.day]?.[i];
                  const at = parseIso(it.at);
                  const target = targetOf(it.kind, it.id);
                  const id =
                    target && "id" in target ? (
                      <Link
                        to={target.to}
                        params={{ id: target.id }}
                        className="font-mono text-accent underline"
                      >
                        {it.id}
                      </Link>
                    ) : target ? (
                      <Link to={target.to} className="font-mono text-accent underline">
                        {it.id}
                      </Link>
                    ) : (
                      <span className="font-mono">{it.id}</span>
                    );
                  return (
                    <li
                      key={key}
                      data-testid="log-row"
                      className="flex items-baseline gap-3 px-4 py-3 text-[14px] leading-snug"
                    >
                      <span className="w-11 shrink-0 font-mono text-[13px] text-muted">
                        {at ? clock.format(at) : ""}
                      </span>
                      <span className="min-w-0 break-words">
                        {whoText(it.by, nameOf)} {opText(it.op)} {kindText(it.kind, it.id)} {id}
                        <RecordTitle kind={it.kind} id={it.id} />
                      </span>
                    </li>
                  );
                })}
              </ul>
            </section>
          ))}
          {q.hasNextPage ? (
            <Button
              variant="secondary"
              className="self-center"
              disabled={q.isFetchingNextPage}
              onClick={() => void q.fetchNextPage()}
            >
              {t("settings.log.more")}
            </Button>
          ) : null}
        </div>
      )}
      <p className="px-1 text-[13px] text-muted">{t("settings.log.keep")}</p>
    </div>
  );
}
