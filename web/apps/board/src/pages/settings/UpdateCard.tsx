import { api } from "@evergenie/sdk-web";
import { Markdown, showToast } from "@evergenie/ui";
import { useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { LoadingBlock } from "../../components/StateViews";
import {
  UpgradeProgress,
  UpgradeRestarting,
  UpgradeResult,
  upgradeOutcome,
  useUpgradeStatus,
} from "../../components/UpgradeStatus";
import { t } from "../../i18n/zh-TW";
import { relativeAgo } from "../../lib/format";
import { useNow } from "../../lib/now";
import { reasonOf } from "../../lib/reason";
import { SettingGroup } from "./SettingRow";

const channelKey = {
  release: "settings.update.channel.release",
  safe: "settings.update.channel.safe",
  off: "settings.update.channel.off",
} as const;

function channelText(channel: string | undefined): string {
  return t(channelKey[(channel as keyof typeof channelKey) ?? "off"] ?? channelKey.off);
}

// 設定 → 系統健康最上面的「版本與更新」（REQ-281）：現在版本、更新方式、有新版時的發版說明與三個按鈕、升級進度與結果。
// 升級中看板會重開而連不上：輪詢失敗不顯示錯誤，寫「正在重開，等一下會自己回來」。現況、進度與結果跟今天頁的
// 升級結果橫幅共用 components/UpgradeStatus（WEB-51、WEB-173）。
export function UpdateCard() {
  const now = useNow();
  const { query, info, waiting, upgrading, disconnected, put, started } = useUpgradeStatus();
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState<"" | "check" | "now" | "tonight" | "skip">("");
  const [error, setError] = useState("");

  const title = t("settings.update.title");
  const restarting = <UpgradeRestarting />;

  if (info === undefined) {
    if (waiting && disconnected) {
      return (
        <SettingGroup title={title}>
          <div className="px-4 py-3.5">{restarting}</div>
        </SettingGroup>
      );
    }
    if (query.isError) {
      return (
        <SettingGroup title={title}>
          <div className="flex flex-col items-start gap-2 px-4 py-3.5">
            <p role="alert" className="text-[13px] leading-snug text-muted">
              {t("ui.loadFailed", { what: title })}
            </p>
            <ActionButton tone="outline" onClick={() => void query.refetch()}>
              {t("ui.retry")}
            </ActionButton>
          </div>
        </SettingGroup>
      );
    }
    return (
      <SettingGroup title={title}>
        <LoadingBlock />
      </SettingGroup>
    );
  }

  const target = info.target ?? "";
  const hasNew = target !== "";
  const tonightAt = info.tonight_at || "03:30";
  const canUpdate = info.core_found;

  const check = async () => {
    setBusy("check");
    setError("");
    try {
      put(await api.system.updateCheck({}));
    } catch (err) {
      setError(t("settings.update.checkFailed", { reason: reasonOf(err) }));
    } finally {
      setBusy("");
    }
  };

  const updateNow = async () => {
    setConfirm(false);
    setBusy("now");
    setError("");
    try {
      const res = await api.system.updateApply({ version: target });
      started(info);
      put(res);
    } catch (err) {
      // 升級一開始看板就可能重開而斷線：已經送出去的不當成失敗。
      setError(t("settings.update.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy("");
    }
  };

  const decide = async (decision: "tonight" | "skip") => {
    setBusy(decision);
    setError("");
    try {
      put(await api.system.updateDecide({ version: target, decision }));
      if (decision === "skip") showToast(t("settings.update.skipped", { version: target }));
    } catch (err) {
      setError(t("settings.update.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy("");
    }
  };

  const progressTo = info.state?.to || target;
  const showResult = !upgrading && upgradeOutcome(info.state) !== null;

  return (
    <SettingGroup title={title}>
      <div data-testid="update-card" className="flex flex-col gap-3 px-4 py-3.5">
        <dl className="m-0 flex flex-col gap-1.5 sm:flex-row sm:gap-8">
          <div>
            <dt className="text-xs text-muted">{t("settings.update.current")}</dt>
            <dd
              data-testid="update-current"
              className="m-0 font-mono text-[15px] font-semibold break-all"
            >
              {info.current || "—"}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-muted">{t("settings.update.channel")}</dt>
            <dd data-testid="update-channel" className="m-0 text-[15px] font-semibold">
              {channelText(info.channel)}
            </dd>
          </div>
        </dl>

        {!info.core_found ? (
          <p data-testid="update-no-core" className="text-[13px] leading-snug text-muted">
            {t("settings.update.noCore")}
          </p>
        ) : null}

        {upgrading ? (
          <UpgradeProgress info={info} version={progressTo} disconnected={disconnected} />
        ) : null}

        {showResult && info.state ? <UpgradeResult state={info.state} /> : null}

        {canUpdate && !upgrading ? (
          <>
            {hasNew ? (
              <div data-testid="update-new" className="flex flex-col gap-2">
                <p className="text-[15px] leading-snug font-semibold">
                  {t("settings.update.new", { version: target })}
                </p>
                <div className="max-h-64 overflow-y-auto rounded-xl border border-border bg-bg px-3.5 py-2.5 text-[14px]">
                  <p className="mb-1 text-xs text-muted">{t("settings.update.notes")}</p>
                  {info.notes ? (
                    <Markdown>{info.notes}</Markdown>
                  ) : (
                    <p className="text-muted">{t("settings.update.notes.none")}</p>
                  )}
                </div>
                {info.decision === "tonight" ? (
                  <p
                    data-testid="update-tonight-set"
                    className="text-[13px] leading-snug text-muted"
                  >
                    {t("settings.update.tonightSet", { time: tonightAt })}
                  </p>
                ) : null}
                <div className="flex flex-wrap gap-2">
                  <ActionButton
                    tone="primary"
                    busy={busy === "now"}
                    disabled={busy !== ""}
                    onClick={() => setConfirm(true)}
                  >
                    {t("settings.update.now")}
                  </ActionButton>
                  {info.decision === "tonight" ? null : (
                    <ActionButton
                      tone="outline"
                      busy={busy === "tonight"}
                      disabled={busy !== ""}
                      onClick={() => void decide("tonight")}
                    >
                      {t("settings.update.tonight", { time: tonightAt })}
                    </ActionButton>
                  )}
                  <ActionButton
                    tone="quiet"
                    busy={busy === "skip"}
                    disabled={busy !== ""}
                    onClick={() => void decide("skip")}
                  >
                    {t("settings.update.skip")}
                  </ActionButton>
                </div>
              </div>
            ) : (
              <p data-testid="update-latest" className="text-[13px] leading-snug text-muted">
                {info.detail || t("settings.update.latest")}
              </p>
            )}
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <ActionButton
                tone="outline"
                busy={busy === "check"}
                disabled={busy !== ""}
                onClick={() => void check()}
              >
                {busy === "check" ? t("settings.update.checking") : t("settings.update.check")}
              </ActionButton>
              {relativeAgo(info.checked_at, now) ? (
                <span className="text-xs text-muted">
                  {t("settings.update.checked", { ago: relativeAgo(info.checked_at, now) })}
                </span>
              ) : null}
            </div>
          </>
        ) : null}

        {error ? (
          <p role="alert" className="text-xs leading-snug text-danger">
            {error}
          </p>
        ) : null}
      </div>
      <ConfirmDialog
        open={confirm}
        tone="primary"
        title={t("settings.update.confirm.title", { version: target })}
        body={t("settings.update.confirm.body", { current: info.current || "" })}
        confirmLabel={t("settings.update.confirm.yes")}
        cancelLabel={t("settings.update.confirm.no")}
        onCancel={() => setConfirm(false)}
        onConfirm={() => void updateNow()}
      />
    </SettingGroup>
  );
}
