import type { WebPhone, WebPhoneSetupResponse } from "@evergenie/sdk-web";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Check, ChevronRight, TriangleAlert } from "lucide-react";
import { type ReactNode, useState } from "react";
import { ActionButton, actionButtonClass } from "../../components/ActionButton";
import { Pill } from "../../components/Pill";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { closeFunnel, openedViaTailnet, phoneQuery, setupPhone } from "../../data/phone";
import { type TextKey, t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { PhoneQr } from "./PhoneQr";

type Done = "done" | "todo" | "manual";

const badgeText: Record<Done, TextKey> = {
  done: "settings.phone.done",
  todo: "settings.phone.todo",
  manual: "settings.phone.manual",
};

function Step({
  n,
  state,
  title,
  hint,
  children,
}: {
  n: number;
  state: Done;
  title: string;
  hint: string;
  children?: ReactNode;
}) {
  return (
    <section
      data-testid={`phone-step-${n}`}
      data-state={state}
      className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface px-4 py-3.5 shadow-[var(--shadow-card)]"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-[15px] leading-snug font-semibold break-words">{title}</h3>
          <p className="mt-0.5 text-[13px] leading-snug text-muted">{hint}</p>
        </div>
        <Pill tone={state === "done" ? "ok" : state === "todo" ? "attention" : "muted"}>
          {state === "done" ? <Check className="mr-0.5 size-3.5" aria-hidden="true" /> : null}
          {t(badgeText[state])}
        </Pill>
      </div>
      {children}
    </section>
  );
}

const note = "text-[14px] leading-relaxed break-words";
const outLink = actionButtonClass("outline", "page", "w-fit max-w-full no-underline");

/** 看板被 Funnel 開到整個網路：紅色警告＋一顆關掉的按鈕（手機、別台開的看板也按得動：關掉是變安全的動作，SET-12）。 */
function FunnelWarning({ phone }: { phone: WebPhone }) {
  const client = useQueryClient();
  const close = useMutation({ mutationFn: () => closeFunnel(client) });
  return (
    <section
      role="alert"
      data-testid="phone-funnel"
      className="flex flex-col gap-3 rounded-[18px] border border-danger bg-danger-soft px-4 py-3.5"
    >
      <h3 className="flex items-start gap-2 text-[16px] leading-snug font-bold text-danger">
        <TriangleAlert className="mt-0.5 size-5 shrink-0" aria-hidden="true" />
        {t("settings.phone.funnel.title")}
      </h3>
      <p className={note}>{t("settings.phone.funnel.body", { url: phone.funnel_url ?? "" })}</p>
      <ActionButton
        tone="danger"
        className="w-fit"
        data-testid="phone-close-funnel"
        busy={close.isPending}
        onClick={() => close.mutate()}
      >
        {t("settings.phone.funnel.close")}
      </ActionButton>
      {close.isError ? (
        <p role="alert" className="text-[14px] text-danger">
          {t("settings.phone.funnel.failed", { reason: reasonOf(close.error) })}
        </p>
      ) : null}
    </section>
  );
}

const step1Text: Record<string, TextKey> = {
  missing: "settings.phone.step1.missing",
  stopped: "settings.phone.step1.stopped",
  login: "settings.phone.step1.login",
  ready: "settings.phone.step1.ready",
};

// 設定 → 手機連線（DEV-153）：四步把看板接到手機，每步看得到做好了沒。
// 會動這台電腦的按鈕（讓手機連得到、關掉對外公開）只在這台 Mac 本機開的看板上出現；伺服器端也擋（board WEB-187）。
export function PhoneSettings() {
  const client = useQueryClient();
  const phone = useQuery(phoneQuery);
  const [result, setResult] = useState<WebPhoneSetupResponse | null>(null);
  const [copied, setCopied] = useState(false);
  const setup = useMutation({
    mutationFn: () => setupPhone(client),
    onMutate: () => setResult(null),
    onSuccess: setResult,
  });

  if (phone.data === undefined) {
    return phone.isError ? (
      <LoadFailed what={t("settings.phone.loadWhat")} onRetry={() => void phone.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const p = phone.data;
  const ready = p.tailscale === "ready";
  const mounted = p.mounted && Boolean(p.url);
  const here = openedViaTailnet(p, window.location.origin);
  const failed = result && !result.ok ? result : null;

  return (
    <div className="flex flex-col gap-4">
      {p.funnel ? <FunnelWarning phone={p} /> : null}

      <Step
        n={1}
        state={ready ? "done" : "todo"}
        title={t("settings.phone.step1")}
        hint={t("settings.phone.step1.hint")}
      >
        <p className={note} data-testid="phone-tailscale" data-tailscale={p.tailscale}>
          {t(step1Text[p.tailscale ?? ""] ?? "settings.phone.step1.missing")}
          {ready && p.account
            ? ` ${t("settings.phone.step1.account", { account: p.account })}`
            : ""}
        </p>
        {ready ? null : (
          <div className="flex flex-wrap gap-2">
            {p.tailscale === "missing" && p.download_url ? (
              // hg-allow-new-tab: 站外連結（Tailscale 官網的下載頁）
              <a href={p.download_url} target="_blank" rel="noreferrer" className={outLink}>
                {t("settings.phone.step1.download")}
              </a>
            ) : null}
            <ActionButton
              tone="secondary"
              data-testid="phone-recheck"
              busy={phone.isFetching}
              onClick={() => void phone.refetch()}
            >
              {t("settings.phone.recheck")}
            </ActionButton>
          </div>
        )}
      </Step>

      <Step
        n={2}
        state={mounted ? "done" : "todo"}
        title={t("settings.phone.step2")}
        hint={t("settings.phone.step2.hint")}
      >
        {mounted ? (
          <p className={note}>
            {t("settings.phone.step2.ready")}
            {p.push_ready ? "" : ` ${t("settings.phone.step2.pushMissing")}`}
          </p>
        ) : null}
        {p.problem ? (
          <p className={`${note} text-muted`} data-testid="phone-problem">
            {p.problem}
          </p>
        ) : null}
        {!p.local ? (
          mounted && p.push_ready ? null : (
            <p className={note} data-testid="phone-not-here">
              {t("settings.phone.step2.notHere")}
            </p>
          )
        ) : !ready ? (
          <p className={`${note} text-muted`}>{t("settings.phone.step2.wait")}</p>
        ) : mounted && p.push_ready ? null : (
          <ActionButton
            tone={mounted ? "secondary" : "primary"}
            className="w-fit"
            data-testid="phone-setup"
            busy={setup.isPending}
            onClick={() => setup.mutate()}
          >
            {t(mounted || failed ? "settings.phone.step2.again" : "settings.phone.step2.go")}
          </ActionButton>
        )}
        {failed ? (
          <div role="alert" data-testid="phone-setup-failed" className="flex flex-col gap-2">
            <p className={`${note} text-danger`}>{failed.message}</p>
            {failed.help_url ? (
              <>
                {/* hg-allow-new-tab: 站外連結（Tailscale 後台的設定網頁） */}
                <a href={failed.help_url} target="_blank" rel="noreferrer" className={outLink}>
                  {t("settings.phone.step2.help")}
                </a>
                <p className={note}>{t("settings.phone.step2.helpAfter")}</p>
              </>
            ) : null}
          </div>
        ) : result?.ok && result.message ? (
          <p role="status" className={note}>
            {result.message}
          </p>
        ) : null}
        {setup.isError ? (
          <p role="alert" data-testid="phone-setup-error" className="text-[14px] text-danger">
            {t("settings.phone.step2.failed", { reason: reasonOf(setup.error) })}
          </p>
        ) : null}
      </Step>

      <Step
        n={3}
        state={here ? "done" : "manual"}
        title={t("settings.phone.step3")}
        hint={t("settings.phone.step3.hint")}
      >
        {here ? (
          <p className={note}>{t("settings.phone.step3.here")}</p>
        ) : p.account ? (
          <p className={note}>{t("settings.phone.step3.account", { account: p.account })}</p>
        ) : null}
      </Step>

      <Step
        n={4}
        state={here ? "done" : "manual"}
        title={t("settings.phone.step4")}
        hint={t(here ? "settings.phone.step4.here" : "settings.phone.step4.hint")}
      >
        {mounted && p.url ? (
          <>
            {here ? null : (
              <div className="flex justify-center py-1">
                <PhoneQr value={p.url} label={t("settings.phone.step4.qr")} />
              </div>
            )}
            <p
              data-testid="phone-url"
              className="rounded-xl bg-surface-2 px-3 py-2.5 text-[14px] break-all select-all"
            >
              {p.url}
            </p>
            <ActionButton
              tone="secondary"
              className="w-fit"
              onClick={() => {
                void navigator.clipboard
                  ?.writeText(p.url ?? "")
                  .then(() => setCopied(true))
                  .catch(() => setCopied(false));
              }}
            >
              {t(copied ? "settings.phone.step4.copied" : "settings.phone.step4.copy")}
            </ActionButton>
          </>
        ) : (
          <p className={`${note} text-muted`}>{t("settings.phone.step4.wait")}</p>
        )}
        <Link
          to="/settings/devices"
          data-testid="phone-next"
          className="flex min-h-11 items-center justify-between gap-2 border-t border-border pt-3 text-[15px] font-medium text-fg no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        >
          <span className="min-w-0">{t("settings.phone.next")}</span>
          <ChevronRight className="size-5 shrink-0 text-muted" aria-hidden="true" />
        </Link>
      </Step>

      <p
        data-testid="phone-tailnet-note"
        className="rounded-[14px] bg-attention-soft px-4 py-3 text-[14px] leading-relaxed text-attention-fg"
      >
        {t("settings.phone.tailnet")}
      </p>
    </div>
  );
}
