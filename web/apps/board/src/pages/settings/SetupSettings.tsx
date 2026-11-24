import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type ReactNode, useEffect, useState } from "react";
import { Pill } from "../../components/Pill";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { t } from "../../i18n/zh-TW";
import { AgentsSettings } from "./AgentsSettings";
import { localSubscription, pushSupported } from "./push";
import { agentSettingsQuery } from "./queries";

type Route = "/settings/devices" | "/settings/phone";

// 一步一張卡：標題、一句說明、「完成／還沒／選用」。
function StepCard({
  testId,
  title,
  hint,
  badge,
  children,
}: {
  testId: string;
  title: string;
  hint: string;
  badge: ReactNode;
  children?: ReactNode;
}) {
  return (
    <section
      data-testid={testId}
      className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface px-4 py-3.5 shadow-[var(--shadow-card)]"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-[15px] leading-snug font-semibold">{title}</h3>
          <p className="mt-0.5 text-[13px] leading-snug text-muted">{hint}</p>
        </div>
        {badge}
      </div>
      {children}
    </section>
  );
}

function DoneBadge({ done }: { done: boolean }) {
  return (
    <Pill tone={done ? "ok" : "attention"}>
      {t(done ? "settings.setup.done" : "settings.setup.todo")}
    </Pill>
  );
}

function GoLink({ to, label }: { to: Route; label?: string }) {
  return (
    <Link
      to={to}
      className="flex min-h-11 w-fit items-center rounded-xl border border-border-strong bg-surface px-4 text-[15px] font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
    >
      {label ?? t("settings.setup.go")}
    </Link>
  );
}

// 設定 → 設定精靈（REQ-238，作者 2026-10-28：「讓 user 選功能，配合秘書，就可以開始使用了，
// 那些進階功能設定都讓 user 去跟秘書討論之後再設定就好」）：兩步——①選職位、每個職位選一位角色；②開始用。
// 權限照預設（全部讓 AI 做，個別調整在「權限」分頁）；選用套件在「套件」分頁開；推播只是第二步裡的一行選用。
export function SetupSettings() {
  const agents = useQuery(agentSettingsQuery);
  const [subscribed, setSubscribed] = useState(false);

  // 這台裝置有沒有訂閱推播：沿用裝置推播頁的判斷（push.ts）。
  useEffect(() => {
    let alive = true;
    void (async () => {
      try {
        const sub =
          pushSupported() && Notification.permission !== "denied"
            ? await localSubscription()
            : null;
        if (alive) setSubscribed(Boolean(sub));
      } catch {
        if (alive) setSubscribed(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  if (agents.data === undefined) {
    return agents.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void agents.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const anyEnabled = (agents.data.agents ?? []).some((a) => a.enabled);

  return (
    <div className="flex flex-col gap-4">
      <StepCard
        testId="setup-step-agents"
        title={t("settings.setup.step1")}
        hint={t("settings.setup.step1.hint")}
        badge={<DoneBadge done={anyEnabled} />}
      >
        <AgentsSettings compact />
      </StepCard>
      <StepCard
        testId="setup-step-start"
        title={t("settings.setup.step2")}
        hint={t(anyEnabled ? "settings.setup.step2.hint" : "settings.setup.step2.wait")}
        badge={null}
      >
        {anyEnabled ? (
          <Link
            to="/"
            className="flex min-h-11 w-fit items-center rounded-xl bg-accent px-4 text-[15px] font-semibold text-accent-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
          >
            {t("settings.setup.start")}
          </Link>
        ) : null}
        <div
          data-testid="setup-push"
          className="flex flex-wrap items-center gap-x-3 gap-y-2 border-t border-border pt-3"
        >
          <p className="min-w-0 flex-1 text-[13px] leading-snug text-muted">
            {t(subscribed ? "settings.setup.push.on" : "settings.setup.push.off")}
          </p>
          {subscribed ? null : <GoLink to="/settings/devices" />}
        </div>
        <div
          data-testid="setup-phone"
          className="flex flex-wrap items-center gap-x-3 gap-y-2 border-t border-border pt-3"
        >
          <p className="min-w-0 flex-1 text-[13px] leading-snug text-muted">
            {t("settings.setup.phone")}
          </p>
          <GoLink to="/settings/phone" label={t("settings.setup.phone.go")} />
        </div>
      </StepCard>
    </div>
  );
}
