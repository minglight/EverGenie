import type { UpdateInfo, UpdateState } from "@evergenie/sdk-web";
import { cn, Spinner } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { updateStatusQuery } from "../data/queries";
import { t } from "../i18n/zh-TW";

// 升級的現況與結果（REQ-281、DEV-155 S6）：設定的「版本與更新」卡（WEB-51）與今天頁的升級結果橫幅（WEB-173）共用。
// 結果照後端的 outcome 分三種講（system 卡 UPD-14）：舊版照常（untouched）、已換回（rolled_back）、也沒換回（stranded）；
// 原因（reason）與下一步（next）都是後端給的白話，英文細節只在升級日誌（log 是它的位置，給秘書查）。

const POLL_MS = 3_000;
// 按了「現在更新」（或「再試一次」「退回上一版」）之後最多等這麼久；超過就不再當成「還在升級」，免得畫面永遠停在重開中。
const WAIT_LIMIT_MS = 5 * 60_000;
const TERMINAL = new Set(["done", "failed", "rolled_back"]);
const PHASES = ["fetch", "backup", "build", "migrate", "restart", "verify"] as const;

/** 升級正在跑：有階段、而且不是結束的那三種。 */
export function isUpgrading(state: UpdateState | undefined): boolean {
  return Boolean(state?.phase) && !TERMINAL.has(state?.phase ?? "");
}

/** 結束了的那一次是怎麼結束的；還在跑或沒有紀錄回 null。 */
export type UpgradeOutcome = "done" | "untouched" | "rolled_back" | "stranded" | "failed";

export function upgradeOutcome(state: UpdateState | undefined): UpgradeOutcome | null {
  const phase = state?.phase ?? "";
  if (!TERMINAL.has(phase)) return null;
  if (phase === "done") return "done";
  switch (state?.outcome) {
    case "untouched":
    case "rolled_back":
    case "stranded":
      return state.outcome;
  }
  // 比這個功能舊的紀錄沒有 outcome：rolled_back 一定是換回了；failed 不知道換回了沒，只說沒成功。
  return phase === "rolled_back" ? "rolled_back" : "failed";
}

/** 這一次沒成功（給橫幅用）：done 以外的結果。 */
export function upgradeFailed(state: UpdateState | undefined): boolean {
  const o = upgradeOutcome(state);
  return o !== null && o !== "done";
}

/** 結果的第一句（白話）：照種類（升級、退回、換回）與結果。 */
export function upgradeHeadline(state: UpdateState): string {
  const vars = { from: state.from || "", to: state.to || "" };
  const outcome = upgradeOutcome(state);
  if (state.kind === "rescue") {
    return outcome === "rolled_back"
      ? t("upgrade.rescue.done", vars)
      : t("upgrade.rescue.stranded", vars);
  }
  if (state.kind === "rollback") {
    if (outcome === "done") return t("upgrade.rollback.done", vars);
    if (outcome === "stranded") return t("upgrade.rollback.stranded", vars);
    return t("upgrade.rollback.untouched", vars);
  }
  switch (outcome) {
    case "done":
      return t("upgrade.result.done", vars);
    case "untouched":
      return vars.to
        ? t("upgrade.result.untouched", vars)
        : t("upgrade.result.untouchedNoTo", vars);
    case "rolled_back":
      return state.restored
        ? t("upgrade.result.rolledBackRestored", vars)
        : t("upgrade.result.rolledBack", vars);
    case "stranded":
      return t("upgrade.result.stranded", vars);
    default:
      return t("upgrade.result.failed", vars);
  }
}

/** 一句結果＋原因＋下一步（設定卡與橫幅都用；data-result 是結果種類）。 */
export function UpgradeResult({ state, className }: { state: UpdateState; className?: string }) {
  const outcome = upgradeOutcome(state);
  if (outcome === null) return null;
  const ok = outcome === "done" || (state.kind === "rescue" && outcome === "rolled_back");
  return (
    <div
      data-testid="update-result"
      data-result={outcome}
      className={cn(
        "flex flex-col gap-1 text-[13px] leading-snug",
        ok ? "text-ok" : "rounded-xl bg-attention-soft px-3 py-2 text-attention-fg",
        className,
      )}
    >
      <p data-testid="update-headline" className={ok ? "" : "font-semibold"}>
        {upgradeHeadline(state)}
      </p>
      {/* 成功（含「退回上一版」換回了）不寫原因：那時的 reason 就是標題那件事。 */}
      {!ok && state.reason ? (
        <p data-testid="update-reason">{t("upgrade.reason", { reason: state.reason })}</p>
      ) : null}
      {outcome !== "done" && state.next ? (
        <p data-testid="update-next">{t("upgrade.next", { next: state.next })}</p>
      ) : null}
    </div>
  );
}

function phaseText(phase: string | undefined): string {
  return (PHASES as readonly string[]).includes(phase ?? "")
    ? t(`settings.update.phase.${phase as (typeof PHASES)[number]}`)
    : t("settings.update.phase.starting");
}

/** 升級進行中：白話進度；看板在重開而連不上時多一句「正在重開，等一下會自己回來」。 */
export function UpgradeProgress({
  info,
  kind,
  version,
  disconnected,
}: {
  info: UpdateInfo | undefined;
  /** 剛按下去、還沒看到這一次的進度時，照按的是哪一種講（rescue＝退回上一版）；看到進度之後照進度的種類。 */
  kind?: "upgrade" | "rescue";
  /** 剛按下去時要講的版本（更新：新版；退回上一版：要換回的版本）。 */
  version: string;
  disconnected: boolean;
}) {
  const state = info?.state;
  const running = isUpgrading(state);
  const k = running ? state?.kind : kind;
  // 換回舊版（「退回上一版」、手動退回）時講「正在換回」「正在退回」，不講「正在更新到」。
  const title =
    k === "rescue"
      ? t("upgrade.progress.rescue", { version: (running && state?.from) || version })
      : k === "rollback"
        ? t("upgrade.progress.rollback", { version: (running && state?.to) || version })
        : t("settings.update.progress", { version: (running && state?.to) || version });
  return (
    <div
      role="status"
      data-testid="update-progress"
      className="flex flex-col gap-1.5 rounded-xl bg-surface-2 px-3 py-2.5"
    >
      <p className="flex items-center gap-2 text-[14px] leading-snug font-semibold">
        <Spinner className="size-4" />
        {title}
      </p>
      <p data-testid="update-phase" className="text-[13px] leading-snug text-muted">
        {phaseText(running ? state?.phase : undefined)}
      </p>
      {disconnected ? <UpgradeRestarting /> : null}
    </div>
  );
}

export function UpgradeRestarting() {
  return (
    <p
      role="status"
      data-testid="update-restarting"
      className="text-[13px] leading-snug text-muted"
    >
      {t("settings.update.restarting")}
    </p>
  );
}

/**
 * 版本與更新的現況：升級進行中（或剛按下去、還沒看到這一次的結果）每 3 秒再抓一次；看板重開而連不上時不當成錯誤。
 * `started()` 在送出更新／退回之後叫：之後等到出現「這一次」的結果才停（最多 5 分鐘）。
 */
export function useUpgradeStatus() {
  const client = useQueryClient();
  // 按下去的時刻，以及按下去時上一次升級的開始時間（用來認出「這一次」的結果）。
  const [clicked, setClicked] = useState<{ at: number; prev: string } | null>(null);
  const waitingFor = (info: UpdateInfo | undefined): boolean => {
    if (clicked === null || Date.now() - clicked.at > WAIT_LIMIT_MS) return false;
    const s = info?.state;
    return !(s && !isUpgrading(s) && (s.started_at ?? "") !== clicked.prev);
  };
  const options = updateStatusQuery();
  const query = useQuery({
    ...options,
    refetchInterval: (q) =>
      isUpgrading(q.state.data?.state) || waitingFor(q.state.data) ? POLL_MS : false,
  });
  const info = query.data;
  const waiting = waitingFor(info);
  return {
    query,
    info,
    waiting,
    upgrading: isUpgrading(info?.state) || waiting,
    disconnected: query.isError || query.failureCount > 0,
    put: (next: UpdateInfo) => client.setQueryData(options.queryKey, next),
    started: (before: UpdateInfo | undefined) =>
      setClicked({ at: Date.now(), prev: before?.state?.started_at ?? "" }),
  };
}
