import { ApiError, api } from "@evergenie/sdk-web";
import { Button } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { siteName } from "../../components/SiteTag";
import { LoadingBlock } from "../../components/StateViews";
import { type Site, useSites } from "../../data/sites";
import { type TextKey, t } from "../../i18n/zh-TW";
import { keysOf, keyToBytes, localSubscription, pushSupported, readyRegistration } from "./push";
import { type SitePush, subscribeOtherSites, unsubscribeOtherSites } from "./pushSites";
import { pushKeyQuery } from "./queries";
import { SettingGroup, SettingRow } from "./SettingRow";
import { VoiceShortcut } from "./VoiceShortcut";

type Phase = "checking" | "unsupported" | "denied" | "off" | "on";
type Busy = "enable" | "disable" | "test" | null;

// 伺服器的白話錯誤直接顯示；瀏覽器自己丟的（英文、內部）一律換成一句白話。
function reasonOf(err: unknown): string {
  return err instanceof ApiError ? err.message : t("settings.devices.browserFailed");
}

const steps: TextKey[] = [
  "settings.devices.ios.step1",
  "settings.devices.ios.step2",
  "settings.devices.ios.step3",
];

function IosSteps() {
  return (
    <SettingGroup title={t("settings.devices.ios.title")}>
      <ol className="flex flex-col gap-3 px-4 py-3.5">
        {steps.map((key, i) => (
          <li key={key} className="flex items-start gap-3">
            <span
              aria-hidden="true"
              className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[13px] font-bold text-accent"
            >
              {i + 1}
            </span>
            <span className="pt-px text-[14px] leading-snug">{t(key)}</span>
          </li>
        ))}
      </ol>
    </SettingGroup>
  );
}

const sitePushText: Record<SitePush["state"], TextKey> = {
  on: "settings.devices.sites.on",
  mismatch: "settings.devices.sites.mismatch",
  missing: "settings.devices.sites.missing",
  unreachable: "settings.devices.sites.unreachable",
  failed: "settings.devices.sites.failed",
};

// 別台的推播（S3-8）：這台裝置開著推播時，每台別台一列，寫進去了沒、為什麼收不到。
function OtherSites({ sites, results }: { sites: Site[]; results: Record<string, SitePush> }) {
  return (
    <SettingGroup title={t("settings.devices.sites.title")}>
      {sites.map((s) => {
        const r = results[s.key];
        const text = r ? t(sitePushText[r.state]) : t("settings.devices.sites.checking");
        return (
          <SettingRow
            key={s.key}
            title={siteName(s)}
            hint={r?.reason ? `${text}（${r.reason}）` : text}
          >
            <span
              data-testid="push-site"
              data-site={s.key}
              data-state={r?.state ?? "checking"}
              className={`text-[13px] font-medium ${r?.state === "on" ? "text-ok" : "text-muted"}`}
            >
              {r?.state === "on"
                ? t("settings.devices.sites.onShort")
                : t("settings.devices.sites.offShort")}
            </span>
          </SettingRow>
        );
      })}
    </SettingGroup>
  );
}

// 設定 → 裝置推播：在這台裝置開啟／關閉推播、送測試通知。
// 多台（S3-8）：同一個訂閱也寫進每台別台（金鑰一樣的才寫）；開著推播時每次進這頁都補寫一次（照 endpoint 新增或更新，
// 重複寫沒有副作用），後來才加進看板的台也收得到。
export function DevicesSettings() {
  const key = useQuery(pushKeyQuery);
  const [phase, setPhase] = useState<Phase>("checking");
  const [busy, setBusy] = useState<Busy>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const sites = useSites();
  const others = sites.filter((s) => !s.primary);
  const [siteResults, setSiteResults] = useState<Record<string, SitePush>>({});
  // 這次進頁已經寫過的台（key＋狀態）：同一台變成能用了再寫一次，不重複寫。
  const synced = useRef(new Set<string>());

  // 這台裝置現在的狀態：不支援／被封鎖／已訂閱／還沒訂閱。
  const check = useCallback(async () => {
    if (!pushSupported()) return setPhase("unsupported");
    if (Notification.permission === "denied") return setPhase("denied");
    try {
      setPhase((await localSubscription()) ? "on" : "off");
    } catch {
      setPhase("off");
    }
  }, []);
  useEffect(() => {
    void check();
  }, [check]);

  const primaryKey = key.data?.public_key ?? "";
  useEffect(() => {
    if (phase !== "on" || primaryKey === "") return;
    // 還在試連的台先不寫（那列保持「正在確認…」），試出結果再寫。
    const todo = sites.filter(
      (s) => !s.primary && s.state !== "checking" && !synced.current.has(`${s.key}\n${s.state}`),
    );
    if (todo.length === 0) return;
    for (const s of todo) synced.current.add(`${s.key}\n${s.state}`);
    void (async () => {
      const sub = await localSubscription().catch(() => null);
      if (!sub) return;
      const got = await subscribeOtherSites(todo, keysOf(sub), primaryKey);
      setSiteResults((prev) => ({ ...prev, ...got }));
    })();
  }, [phase, primaryKey, sites]);

  const enable = async () => {
    setBusy("enable");
    setError("");
    setNotice("");
    try {
      const permission = await Notification.requestPermission();
      if (permission !== "granted") {
        setPhase(permission === "denied" ? "denied" : "off");
        setError(t("settings.devices.notGranted"));
        return;
      }
      const { public_key } = await api.notify.pushKey({});
      const reg = await readyRegistration();
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: keyToBytes(public_key ?? ""),
      });
      try {
        await api.notify.subscribePush(keysOf(sub));
      } catch (err) {
        // 伺服器沒記下來：把剛訂的退掉，才不會留下一個收不到的訂閱。
        await sub.unsubscribe().catch(() => {});
        throw err;
      }
      // 別台由上面的 effect 寫（phase 變成 on 之後）。
      synced.current.clear();
      setSiteResults({});
      setPhase("on");
    } catch (err) {
      setError(t("settings.devices.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy(null);
      void key.refetch();
    }
  };

  const disable = async () => {
    setBusy("disable");
    setError("");
    setNotice("");
    try {
      const sub = await localSubscription();
      if (sub) {
        await api.notify.unsubscribePush({ endpoint: sub.endpoint });
        await unsubscribeOtherSites(sites, sub.endpoint);
        await sub.unsubscribe().catch(() => {});
      }
      synced.current.clear();
      setSiteResults({});
      setPhase("off");
    } catch (err) {
      setError(t("settings.devices.failedOff", { reason: reasonOf(err) }));
    } finally {
      setBusy(null);
      void key.refetch();
    }
  };

  const sendTest = async () => {
    setBusy("test");
    setError("");
    setNotice("");
    try {
      const sub = await localSubscription();
      await api.notify.pushTest({ endpoint: sub?.endpoint ?? "" });
      setNotice(t("settings.devices.testSent"));
    } catch (err) {
      setError(reasonOf(err));
    } finally {
      setBusy(null);
    }
  };

  const keyMissing = key.isError && key.error.message === t("settings.devices.keyMissing.server");
  const count = key.data?.subscriptions;

  let title = t("settings.devices.this");
  let hint = "";
  if (phase === "unsupported") {
    title = t("settings.devices.unsupported");
    hint = t("settings.devices.unsupported.hint");
  } else if (phase === "denied") {
    title = t("settings.devices.denied");
    hint = t("settings.devices.denied.hint");
  } else if (keyMissing) {
    title = t("settings.devices.keyMissing");
    hint = t("settings.devices.keyMissing.hint");
  } else if (phase === "on") {
    title = t("settings.devices.status.on");
  } else if (phase === "off") {
    title = t("settings.devices.status.off");
  }

  if (phase === "checking") return <LoadingBlock />;

  const blocked = phase === "unsupported" || phase === "denied" || key.isError;
  return (
    <div className="flex flex-col gap-5">
      <SettingGroup title={t("settings.devices.this")}>
        <SettingRow
          title={title}
          hint={hint}
          error={error || (key.isError && !keyMissing ? key.error.message : "")}
        >
          <div className="flex flex-wrap gap-2 md:justify-end">
            {phase === "on" ? (
              <>
                <Button onClick={() => void sendTest()} disabled={busy !== null}>
                  {busy === "test" ? t("settings.devices.testing") : t("settings.devices.test")}
                </Button>
                <Button onClick={() => void disable()} disabled={busy !== null}>
                  {t("settings.devices.disable")}
                </Button>
              </>
            ) : (
              <Button
                variant="primary"
                onClick={() => void enable()}
                disabled={busy !== null || blocked}
                // 這台開不了時按鈕是停用的：不用半透明（字只剩 2.98:1），改灰底灰字（淺 5.31:1、深 5.90:1，DEV-156 U-26）；
                // 開不了的原因寫在左邊那兩行。
                className={
                  blocked
                    ? "disabled:bg-surface-2 disabled:text-muted disabled:opacity-100"
                    : undefined
                }
                data-testid="push-enable"
              >
                {busy === "enable" ? t("settings.devices.enabling") : t("settings.devices.enable")}
              </Button>
            )}
          </div>
        </SettingRow>
        {notice ? (
          <p role="status" className="px-4 py-3 text-[13px] font-medium text-ok">
            {notice}
          </p>
        ) : null}
      </SettingGroup>
      {count !== undefined ? (
        <p className="px-1 text-[13px] text-muted" data-testid="push-count">
          {count > 0 ? t("settings.devices.count", { n: count }) : t("settings.devices.count.none")}
        </p>
      ) : null}
      {phase === "on" && others.length > 0 ? (
        <OtherSites sites={others} results={siteResults} />
      ) : null}
      <IosSteps />
      <VoiceShortcut />
    </div>
  );
}
