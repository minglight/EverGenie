import { api, type Package, type PackageState } from "@evergenie/sdk-web";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Pill, type PillTone } from "../../components/Pill";
import { Toggle } from "../../components/Toggle";
import { invalidatePackageViews } from "../../data/queries";
import { type TextKey, t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { sourceLabel } from "./packageSource";

const stateText = {
  running: "settings.packages.state.running",
  building: "settings.packages.state.building",
  stopped: "settings.packages.state.stopped",
  failed: "settings.packages.state.failed",
  rejected: "settings.packages.state.rejected",
} as const satisfies Record<PackageState, TextKey>;

const stateTone: Record<PackageState, PillTone> = {
  running: "ok",
  building: "accent",
  stopped: "muted",
  failed: "danger",
  rejected: "attention",
};

/**
 * 名稱旁的狀態標籤（PKG-53）：開關開著、程式卻停著時不只寫「停止」（跟旁邊開著的開關看起來矛盾），
 * 寫「開著但沒在跑」、用提醒色；其他照狀態。
 */
export function stateBadge(
  pkg: Pick<Package, "state" | "enabled">,
): { text: TextKey; tone: PillTone } | null {
  const state = pkg.state;
  if (!state) return null;
  if (state === "stopped" && pkg.enabled) {
    return { text: "settings.packages.state.stoppedOn", tone: "attention" };
  }
  return { text: stateText[state], tone: stateTone[state] };
}

/** 官方更新的差異清單（只在 fork 的套件展開時才向伺服器要）。 */
function Updates({ id }: { id: string }) {
  const q = useQuery({
    queryKey: ["package.list_updates", id],
    queryFn: () => api.package.listUpdates({ id }),
    staleTime: 0,
  });
  if (q.data === undefined) {
    return (
      <p role="status" className="text-[13px] text-muted">
        {q.isError
          ? t("settings.packages.failed", { reason: reasonOf(q.error) })
          : t("settings.packages.updates.loading")}
      </p>
    );
  }
  const { items = [], base_version, latest_version, report_path } = q.data;
  return (
    <div data-testid="package-updates" className="flex flex-col gap-2 rounded-xl bg-surface-2 p-3">
      {items.length === 0 ? (
        <p className="text-[13px] text-muted">{t("settings.packages.updates.none")}</p>
      ) : (
        <>
          {base_version || latest_version ? (
            <p className="text-[13px] text-muted">
              {t("settings.packages.updates.versions", {
                base: base_version || "—",
                latest: latest_version || "—",
              })}
            </p>
          ) : null}
          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {items.map((it) => (
              <li key={it.path} className="flex flex-col gap-0.5">
                <p className="flex flex-wrap items-center gap-2 text-[13px]">
                  <span className="font-mono font-semibold break-all">{it.path}</span>
                  {it.conflicts_with_fork ? (
                    <Pill tone="danger">{t("settings.packages.updates.conflict")}</Pill>
                  ) : null}
                </p>
                {it.summary ? (
                  <p className="text-[13px] leading-snug text-muted">{it.summary}</p>
                ) : null}
              </li>
            ))}
          </ul>
        </>
      )}
      {report_path ? (
        <p className="text-[13px] leading-snug">
          {t("settings.packages.updates.report", { path: report_path })}
        </p>
      ) : null}
    </div>
  );
}

// 一個套件一列：名稱、版本、說明、狀態、啟用開關，以及重新載入／解除安裝／複製／看官方更新。
export function PackageRow({ pkg }: { pkg: Package }) {
  const client = useQueryClient();
  const id = pkg.id ?? "";
  const name = pkg.name || id;
  const [confirm, setConfirm] = useState<"disable" | "uninstall" | null>(null);
  const [showUpdates, setShowUpdates] = useState(false);
  const [error, setError] = useState("");

  const run = useMutation({
    mutationFn: async (action: () => Promise<unknown>) => action(),
    onMutate: () => setError(""),
    onError: (err) => setError(t("settings.packages.failed", { reason: reasonOf(err) })),
    onSettled: () => invalidatePackageViews(client),
  });
  const setEnabled = (enabled: boolean) =>
    run.mutate(() => api.package.setEnabled({ id, enabled }));
  const state = pkg.state;
  const badge = stateBadge(pkg);

  return (
    <div data-testid={`package-${id}`} className="flex flex-col gap-2.5 px-4 py-3.5">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[15px] leading-snug font-semibold">
            <span className="break-words">{name}</span>
            {badge ? <Pill tone={badge.tone}>{t(badge.text)}</Pill> : null}
          </p>
          <p className="mt-0.5 text-xs text-muted">
            {t("settings.packages.version", { v: pkg.version || "—" })}
          </p>
          {sourceLabel(pkg.source?.url) ? (
            <p data-testid={`package-source-${id}`} className="mt-0.5 text-xs break-all text-muted">
              {sourceLabel(pkg.source?.url)}
            </p>
          ) : null}
          {pkg.description ? (
            <p className="mt-1 text-[13px] leading-snug text-muted break-words">
              {pkg.description}
            </p>
          ) : null}
          {pkg.error ? (
            <p role="status" className="mt-1 text-[13px] leading-snug text-danger break-words">
              {pkg.error}
            </p>
          ) : null}
        </div>
        <Toggle
          checked={pkg.enabled}
          label={t("settings.packages.enabled", { name })}
          disabled={run.isPending}
          onChange={(next) => (next ? setEnabled(true) : setConfirm("disable"))}
        />
      </div>
      <div className="flex flex-wrap gap-2">
        {state === "failed" && pkg.kind === "program" ? (
          <ActionButton
            tone="outline"
            size="pane"
            busy={run.isPending}
            onClick={() => run.mutate(() => api.package.reload({ id }))}
          >
            {t("settings.packages.reload")}
          </ActionButton>
        ) : null}
        {pkg.tier === "mine" && pkg.fork ? (
          <ActionButton tone="outline" size="pane" onClick={() => setShowUpdates((v) => !v)}>
            {t(showUpdates ? "settings.packages.updates.hide" : "settings.packages.updates")}
          </ActionButton>
        ) : null}
        {pkg.tier === "installed" ? (
          <ActionButton
            tone="quiet"
            size="pane"
            disabled={run.isPending}
            onClick={() => setConfirm("uninstall")}
          >
            {t("settings.packages.uninstall")}
          </ActionButton>
        ) : null}
      </div>
      {showUpdates ? <Updates id={id} /> : null}
      {error ? (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
      <ConfirmDialog
        open={confirm === "disable"}
        title={t("settings.packages.disable.title", { name })}
        body={t("settings.packages.disable.body")}
        confirmLabel={t("settings.packages.disable.yes")}
        cancelLabel={t("settings.packages.disable.no")}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          setConfirm(null);
          setEnabled(false);
        }}
      />
      <ConfirmDialog
        open={confirm === "uninstall"}
        title={t("settings.packages.uninstall.title", { name })}
        body={t("settings.packages.uninstall.body")}
        confirmLabel={t("settings.packages.uninstall.yes")}
        cancelLabel={t("settings.packages.disable.no")}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          setConfirm(null);
          run.mutate(() => api.package.uninstall({ id }));
        }}
      />
    </div>
  );
}
