import { api, type SystemBackupSweepResponse } from "@evergenie/sdk-web";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ScanSearch } from "lucide-react";
import { useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { t } from "../../i18n/zh-TW";
import { bytesText, timeLabel } from "../../lib/format";
import { useNow } from "../../lib/now";
import { reasonOf } from "../../lib/reason";
import { backupSweepQuery } from "./backupQueries";
import { type SweepGroup, sweepCount, sweepDetail, sweepGroups } from "./backupSweepView";

// 設定 → 這台電腦 → 備份 → 備份掃描（DEV-163）：每週日清晨系統自己掃一次工作資料夾、這個家、AI 帳號資料夾，
// 這裡顯示上一次的報告（已到期的可刪／在用／沒查完、名字不合規、期限內幾個、略過的例外），加一顆「現在掃一次」。
// 只出報告、不刪；要刪跟電腦秘書說。分組與判斷都是伺服器做的，這裡只畫（FEN-N3）。

function Group({ g }: { g: SweepGroup }) {
  if (g.items.length === 0) return null;
  return (
    <details data-testid={`sweep-group-${g.key}`} className="px-4 py-1">
      <summary className="flex min-h-11 cursor-pointer items-center text-[14px] font-semibold">
        {t(`settings.sweep.group.${g.key}`, { n: g.items.length })}
      </summary>
      <ul className="m-0 flex list-none flex-col divide-y divide-border rounded-xl border border-border p-0">
        {g.items.map((it) => (
          <li key={it.path} data-testid="sweep-item" className="flex flex-col gap-0.5 px-3 py-2.5">
            <span className="text-[13px] font-medium break-all">
              {it.path}
              {it.dir ? t("settings.sweep.dir") : ""}
              {g.key !== "skipped" ? t("settings.sweep.size", { size: bytesText(it.size) }) : ""}
            </span>
            {sweepDetail(g.key, it) ? (
              <span className="text-[13px] leading-snug text-muted break-words">
                {sweepDetail(g.key, it)}
              </span>
            ) : null}
          </li>
        ))}
      </ul>
    </details>
  );
}

export function BackupSweep() {
  const q = useQuery(backupSweepQuery);
  const client = useQueryClient();
  const now = useNow();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const run = async () => {
    setBusy(true);
    setError("");
    try {
      const res = await api.system.backupSweep({ run: true });
      client.setQueryData<SystemBackupSweepResponse>(backupSweepQuery.queryKey, res);
    } catch (err) {
      setError(t("settings.sweep.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy(false);
    }
  };
  const rep = q.data?.report;
  const groups = sweepGroups(rep);
  return (
    <div data-testid="backup-sweep" className="flex flex-col">
      <p className="px-4 pt-3.5 pb-1 text-[13px] font-semibold text-muted">
        {t("settings.sweep.title")}
      </p>
      {q.data === undefined ? (
        q.isError ? (
          <LoadFailed what={t("settings.loadFailed")} onRetry={() => void q.refetch()} />
        ) : (
          <LoadingBlock />
        )
      ) : !rep ? (
        <p className="px-4 py-3.5 text-[13px] text-muted">{t("settings.sweep.none")}</p>
      ) : (
        <>
          <p
            data-testid="sweep-summary"
            className="px-4 py-3 text-[15px] leading-snug font-semibold"
          >
            {t("settings.sweep.summary", {
              deletable: sweepCount(groups, "deletable"),
              inUse: sweepCount(groups, "inUse"),
              unchecked: sweepCount(groups, "unchecked"),
              misnamed: sweepCount(groups, "misnamed"),
              current: rep.current,
              size: bytesText(rep.current_bytes),
              skipped: sweepCount(groups, "skipped"),
            })}
          </p>
          <p className="px-4 pb-2 text-[13px] text-muted">
            {t(
              rep.by === "system" ? "settings.sweep.when.schedule" : "settings.sweep.when.manual",
              {
                at: timeLabel(rep.at, now) || "—",
              },
            )}
            {rep.pruned > 0
              ? t("settings.sweep.pruned", { n: rep.pruned, size: bytesText(rep.pruned_bytes) })
              : ""}
          </p>
          {rep.errors && rep.errors.length > 0 ? (
            <div
              role="alert"
              data-testid="sweep-errors"
              className="mx-4 mb-2 rounded-xl bg-danger-soft px-3 py-2.5 text-[13px] leading-snug break-words text-danger"
            >
              <p className="font-semibold">{t("settings.sweep.errors")}</p>
              <ul className="mt-1 list-disc pl-5">
                {rep.errors.map((e, i) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: 同一份報告的出錯可能重複、順序固定
                  <li key={i}>{e}</li>
                ))}
              </ul>
            </div>
          ) : null}
          {groups.map((g) => (
            <Group key={g.key} g={g} />
          ))}
        </>
      )}
      <div className="flex flex-col gap-2 px-4 py-3">
        <ActionButton
          tone="outline"
          busy={busy}
          onClick={() => void run()}
          data-testid="sweep-run"
          className="w-full sm:w-auto sm:self-start"
        >
          <ScanSearch aria-hidden="true" className="size-4" />
          {busy ? t("settings.sweep.running") : t("settings.sweep.run")}
        </ActionButton>
        {q.data?.running && !busy ? (
          <p role="status" className="text-[13px] text-muted">
            {t("settings.sweep.busy")}
          </p>
        ) : null}
        {error ? (
          <p role="alert" className="text-[13px] leading-snug break-words text-danger">
            {error}
          </p>
        ) : null}
        <p className="text-[13px] leading-snug text-muted">{t("settings.sweep.hint")}</p>
      </div>
    </div>
  );
}
