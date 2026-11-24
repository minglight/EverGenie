import { api, type UpdateState } from "@evergenie/sdk-web";
import { useId, useState } from "react";
import { t } from "../i18n/zh-TW";
import { relativeAgo } from "../lib/format";
import { useNow } from "../lib/now";
import { reasonOf } from "../lib/reason";
import { ActionButton } from "./ActionButton";
import { ConfirmDialog } from "./ConfirmDialog";
import {
  UpgradeProgress,
  UpgradeResult,
  upgradeFailed,
  upgradeOutcome,
  useUpgradeStatus,
} from "./UpgradeStatus";

// 今天頁的升級結果橫幅（WEB-173，DEV-155 S6）：服務起得來、但上一次更新沒成功時，在今天頁最上面講結果與可以按的下一步：
// 「再試一次」（舊版照常、還有新版可以升）、「退回上一版」（也沒換回：照 rescue.json 再試一次換回，update_rollback）、
// 「看細節」（原因、版本、時間、升級日誌的位置）、「知道了」（收起這一次；也沒換回、還能按「退回上一版」的不給收，要處理）。
// 按了「再試一次」「退回上一版」之後就地顯示進度（看板會重開而連不上，不當成錯誤）。現況與結果跟設定的版本與更新卡共用 UpgradeStatus。

/** 超過這麼久的「沒成功」（舊版照常、已換回）不再擺在今天頁；也沒換回、還能按「退回上一版」的一直擺到處理好。 */
const STALE_MS = 7 * 24 * 60 * 60_000;
const DISMISS_KEY = "eg.upgradeBanner.dismissed";

function readDismissed(): string {
  try {
    return window.localStorage.getItem(DISMISS_KEY) ?? "";
  } catch {
    return "";
  }
}

function writeDismissed(startedAt: string) {
  try {
    window.localStorage.setItem(DISMISS_KEY, startedAt);
  } catch {
    // 私密模式或額度滿了：只是這一頁收起來，下次還會出現。
  }
}

function stale(state: UpdateState, now: number): boolean {
  const at = Date.parse(state.finished_at || state.started_at || "");
  return Number.isFinite(at) && now - at > STALE_MS;
}

export function UpgradeBanner() {
  const now = useNow();
  const { info, upgrading, disconnected, put, started } = useUpgradeStatus();
  const [dismissed, setDismissed] = useState(readDismissed);
  // 這一頁按過「再試一次」或「退回上一版」：之後的進度與結果都擺在這裡（就算結果是成功）。
  const [acted, setActed] = useState<{ kind: "upgrade" | "rescue"; version: string } | null>(null);
  const [details, setDetails] = useState(false);
  const [confirm, setConfirm] = useState<"" | "retry" | "rescue">("");
  const [busy, setBusy] = useState<"" | "retry" | "rescue">("");
  const [error, setError] = useState("");
  const detailsId = useId();

  const state = info?.state;
  if (info === undefined) return null;
  if (acted && upgrading) {
    return (
      <section aria-label={t("upgrade.banner.label")} data-testid="upgrade-banner">
        <UpgradeProgress
          info={info}
          kind={acted.kind}
          version={acted.version}
          disconnected={disconnected}
        />
      </section>
    );
  }
  if (!state || upgrading) return null;
  const outcome = upgradeOutcome(state);
  const failed = upgradeFailed(state);
  if (!failed && !(acted && outcome !== null)) return null;
  const stranded = outcome === "stranded";
  // 要處理的：也沒換回、而且有 rescue.json（能按「退回上一版」）。手動退回退到一半也是 stranded，但沒有紀錄可以照著換回，
  // 下一步是請秘書處理，那種給收起來。
  const mustHandle = stranded && Boolean(info.rescue);
  if (
    !acted &&
    !mustHandle &&
    (dismissed === (state.started_at ?? "") || stale(state, now.getTime()))
  )
    return null;

  const target = info.target ?? "";
  const canRetry = failed && !stranded && !info.rescue && info.core_found && target !== "";
  const canRescue = Boolean(info.rescue) && info.core_found;
  const vars = { from: state.from || "", to: state.to || "" };

  const run = async (what: "retry" | "rescue") => {
    setConfirm("");
    setBusy(what);
    setError("");
    try {
      const res =
        what === "retry"
          ? await api.system.updateApply({ version: target })
          : await api.system.updateRollback({});
      started(info);
      setActed(
        what === "retry"
          ? { kind: "upgrade", version: target }
          : { kind: "rescue", version: state.from || "" },
      );
      put(res);
    } catch (err) {
      setError(t("upgrade.banner.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy("");
    }
  };

  const dismiss = () => {
    writeDismissed(state.started_at ?? "");
    setDismissed(state.started_at ?? "");
    setActed(null);
  };

  const ago = relativeAgo(state.finished_at || state.started_at, now);
  return (
    <section
      aria-label={t("upgrade.banner.label")}
      data-testid="upgrade-banner"
      data-outcome={outcome ?? ""}
      className="flex flex-col gap-2"
    >
      <UpgradeResult state={state} />
      <div className="flex flex-wrap gap-2">
        {canRescue ? (
          <ActionButton
            tone="primary"
            busy={busy === "rescue"}
            disabled={busy !== ""}
            onClick={() => setConfirm("rescue")}
          >
            {t("upgrade.banner.rescue")}
          </ActionButton>
        ) : null}
        {canRetry ? (
          <ActionButton
            tone="primary"
            busy={busy === "retry"}
            disabled={busy !== ""}
            onClick={() => setConfirm("retry")}
          >
            {t("upgrade.banner.retry")}
          </ActionButton>
        ) : null}
        <ActionButton
          tone="outline"
          aria-expanded={details}
          aria-controls={details ? detailsId : undefined}
          onClick={() => setDetails((v) => !v)}
        >
          {details ? t("upgrade.banner.hideDetails") : t("upgrade.banner.details")}
        </ActionButton>
        {mustHandle ? null : (
          <ActionButton tone="quiet" disabled={busy !== ""} onClick={dismiss}>
            {t("upgrade.banner.dismiss")}
          </ActionButton>
        )}
      </div>
      {details ? (
        <div
          id={detailsId}
          data-testid="upgrade-details"
          className="flex flex-col gap-1 rounded-xl bg-surface-2 px-3 py-2.5 text-[13px] leading-snug text-muted"
        >
          <p>{t("upgrade.banner.current", { version: info.current || "—" })}</p>
          {vars.from || vars.to ? <p>{t("upgrade.banner.versions", vars)}</p> : null}
          {ago ? <p>{t("upgrade.banner.when", { ago })}</p> : null}
          {state.log ? (
            <p className="break-all">{t("upgrade.banner.log", { path: state.log })}</p>
          ) : null}
        </div>
      ) : null}
      {error ? (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
      <ConfirmDialog
        open={confirm === "retry"}
        tone="primary"
        title={t("settings.update.confirm.title", { version: target })}
        body={t("settings.update.confirm.body", { current: info.current || "" })}
        confirmLabel={t("settings.update.confirm.yes")}
        cancelLabel={t("settings.update.confirm.no")}
        onCancel={() => setConfirm("")}
        onConfirm={() => void run("retry")}
      />
      <ConfirmDialog
        open={confirm === "rescue"}
        tone="primary"
        title={t("upgrade.rescue.confirm.title", vars)}
        body={t("upgrade.rescue.confirm.body", vars)}
        confirmLabel={t("upgrade.rescue.confirm.yes")}
        cancelLabel={t("settings.update.confirm.no")}
        onCancel={() => setConfirm("")}
        onConfirm={() => void run("rescue")}
      />
    </section>
  );
}
