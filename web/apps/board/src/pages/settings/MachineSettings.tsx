import type { BackupFile } from "@evergenie/sdk-web";
import { useQuery } from "@tanstack/react-query";
import { Check, X } from "lucide-react";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { t } from "../../i18n/zh-TW";
import { bytesText, parseIso, timeLabel } from "../../lib/format";
import { useNow } from "../../lib/now";
import { AccountsSettings } from "./AccountsSettings";
import { BackupSweep } from "./BackupSweep";
import { HomeBackup } from "./HomeBackup";
import { NightQueue } from "./NightQueue";
import { backupsQuery, preflightQuery, usageQuery } from "./queries";
import { SettingGroup } from "./SettingRow";
import { SitesSettings } from "./SitesSettings";

type Q = { data: unknown; isError: boolean; refetch: () => unknown };

// 一段的載入狀態：還沒到顯示轉圈，失敗顯示「再試一次」。
function Loading({ q }: { q: Q }) {
  return q.isError ? (
    <LoadFailed what={t("settings.loadFailed")} onRetry={() => void q.refetch()} />
  ) : (
    <LoadingBlock />
  );
}

function Env() {
  const q = useQuery(preflightQuery);
  return (
    <SettingGroup title={t("settings.machine.env")}>
      {q.data === undefined ? (
        <Loading q={q} />
      ) : q.data.length === 0 ? (
        <p className="px-4 py-3.5 text-[13px] text-muted">{t("settings.machine.env.empty")}</p>
      ) : (
        q.data.map((c) => (
          <div
            key={c.id ?? c.name}
            data-testid="preflight-check"
            data-ok={c.ok}
            className="flex items-start gap-3 px-4 py-3.5"
          >
            <span
              aria-hidden="true"
              className={`mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full ${c.ok ? "bg-ok-soft text-ok" : "bg-danger-soft text-danger"}`}
            >
              {c.ok ? (
                <Check className="size-3.5" strokeWidth={3} />
              ) : (
                <X className="size-3.5" strokeWidth={3} />
              )}
            </span>
            <div className="min-w-0 flex-1">
              <p className="text-[15px] leading-snug font-semibold">
                {c.name || c.id}
                <span className="sr-only">
                  ：{t(c.ok ? "settings.machine.env.ok" : "settings.machine.env.bad")}
                </span>
              </p>
              {c.detail ? (
                <p className="mt-0.5 text-[13px] leading-snug text-muted break-words">{c.detail}</p>
              ) : null}
              {!c.ok && c.fix ? (
                <p className="mt-1 text-[13px] leading-snug break-words">{c.fix}</p>
              ) : null}
            </div>
          </div>
        ))
      )}
      <p className="px-4 py-3 text-[13px] text-muted">{t("settings.machine.env.hint")}</p>
    </SettingGroup>
  );
}

// 用量（DEV-153、WEB-193）：只列量得到的帳號；一個都沒量到（引擎還沒回報、這個方案量不到）就整區收起來，
// 不放一條空的用量條或「還沒量到」讓人以為壞了。讀取中、讀不到時照常顯示（看得出在等或出了什麼事）。
function Usage() {
  const q = useQuery(usageQuery);
  const now = useNow();
  const rows = (q.data ?? []).filter((u) => parseIso(u.measured_at) !== null);
  if (q.data !== undefined && rows.length === 0) return null;
  return (
    <SettingGroup title={t("settings.machine.usage")}>
      {q.data === undefined ? (
        <Loading q={q} />
      ) : (
        rows.map((u) => {
          const pct = Math.min(100, Math.max(0, Math.round((u.used_ratio ?? 0) * 100)));
          return (
            <div
              key={u.account}
              data-testid="usage-row"
              className="flex flex-col gap-2 px-4 py-3.5"
            >
              <div className="flex items-baseline justify-between gap-3">
                <p className="min-w-0 truncate text-[15px] font-semibold">{u.account}</p>
                <p className="shrink-0 text-[13px] text-muted">
                  {t("settings.machine.usage.used", { pct })}
                </p>
              </div>
              <div
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={pct}
                aria-label={u.account}
                className="h-2 overflow-hidden rounded-full bg-surface-2"
              >
                <div
                  className={`h-full rounded-full ${pct >= 90 ? "bg-danger" : pct >= 70 ? "bg-attention" : "bg-accent"}`}
                  style={{ width: `${pct}%` }}
                />
              </div>
              {parseIso(u.resets_at) ? (
                <p className="text-[13px] text-muted">
                  {t("settings.machine.usage.resets", { at: timeLabel(u.resets_at, now) })}
                </p>
              ) : null}
              {u.note ? (
                <p className="text-[13px] leading-snug text-muted break-words">{u.note}</p>
              ) : null}
            </div>
          );
        })
      )}
    </SettingGroup>
  );
}

interface Group {
  name: string;
  count: number;
  newest: string | undefined;
}

// 每組（第一層資料夾）的份數與最新時間，最新的在前。
function groupsOf(files: BackupFile[]): Group[] {
  const by = new Map<string, Group>();
  for (const f of files) {
    const name = f.group || t("settings.machine.top");
    const g = by.get(name) ?? { name, count: 0, newest: undefined };
    g.count++;
    const at = parseIso(f.mod_time)?.getTime() ?? 0;
    if (at > (parseIso(g.newest)?.getTime() ?? 0)) g.newest = f.mod_time;
    by.set(name, g);
  }
  return [...by.values()].sort(
    (a, b) =>
      (parseIso(b.newest)?.getTime() ?? 0) - (parseIso(a.newest)?.getTime() ?? 0) ||
      a.name.localeCompare(b.name),
  );
}

function Backups() {
  const q = useQuery(backupsQuery);
  const now = useNow();
  const files = q.data?.files ?? [];
  return (
    <SettingGroup title={t("settings.machine.backups")}>
      <HomeBackup />
      <p className="px-4 pt-3.5 pb-1 text-[13px] font-semibold text-muted">
        {t("settings.backup.small.title")}
      </p>
      {q.data === undefined ? (
        <Loading q={q} />
      ) : files.length === 0 ? (
        <p className="px-4 py-3.5 text-[13px] text-muted">{t("settings.machine.backups.empty")}</p>
      ) : (
        <>
          <p data-testid="backups-total" className="px-4 py-3.5 text-[15px] font-semibold">
            {t("settings.machine.backups.total", {
              size: bytesText(q.data.total),
              n: files.length,
            })}
          </p>
          {groupsOf(files).map((g) => (
            <p
              key={g.name}
              data-testid="backup-group"
              className="px-4 py-3 text-[14px] leading-snug"
            >
              {t("settings.machine.backups.group", {
                group: g.name,
                n: g.count,
                at: timeLabel(g.newest, now) || "—",
              })}
            </p>
          ))}
        </>
      )}
      <p className="px-4 py-3 text-[13px] text-muted">{t("settings.machine.backups.hint")}</p>
      <BackupSweep />
    </SettingGroup>
  );
}

// 設定 → 電腦：最上面是看板連的電腦與誰可以連進這台（S3-9，WEB-80），之後只講這台自己（WEB-34）：
// 環境、AI 帳號、用量、夜班清單（可以新增與刪除待做）、備份；AI 帳號、夜班清單可以在這裡改，其他唯讀（要修、要清理跟任何一位秘書說）。
export function MachineSettings() {
  return (
    <div className="flex flex-col gap-5">
      <SitesSettings />
      <Env />
      <AccountsSettings />
      <Usage />
      <NightQueue />
      <Backups />
    </div>
  );
}
