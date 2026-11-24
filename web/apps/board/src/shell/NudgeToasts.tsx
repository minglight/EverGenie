import { Link } from "@tanstack/react-router";
import { X } from "lucide-react";
import { nudgeIsHere, resolveNudge, type SiteNudge, useNudges } from "../data/viewing";
import { t } from "../i18n/zh-TW";

/** 站內提示（P7-6、D-34）：看板開著、但不在那張單／那個討論串時，「還在等你」的提醒顯示在這裡（不推播）。
 * 在那一頁時畫成頁內卡片（NudgeCard），這裡就不重複。按「知道了」其他地方一起收。
 * 多台（S3-8）：viewed＝這一頁看的是哪一台；別台的提醒「去看看」帶 `?home=<那台>`。 */
export function NudgeToasts({ pathname, viewed = "" }: { pathname: string; viewed?: string }) {
  const toasts = useNudges().filter((n) => !n.resolved && !nudgeIsHere(n, pathname, viewed));
  if (toasts.length === 0) return null;
  return (
    <div
      aria-live="polite"
      className="pointer-events-none fixed right-3 bottom-[calc(env(safe-area-inset-bottom,0px)+94px)] left-3 z-50 flex flex-col items-stretch gap-2 md:right-5 md:bottom-5 md:left-auto md:w-[380px]"
    >
      {toasts.map((n) => (
        <NudgeToast key={n.id} nudge={n} />
      ))}
    </div>
  );
}

function NudgeToast({ nudge: n }: { nudge: SiteNudge }) {
  const [path = "/", query] = (n.url ?? "/").split("?");
  const search = Object.fromEntries(new URLSearchParams(query ?? ""));
  if (n.site) search.home = n.site;
  return (
    <section
      aria-label={t("nudge.toast")}
      data-testid="nudge-toast"
      className="hg-rise pointer-events-auto flex items-start gap-3 rounded-2xl border border-attention bg-attention-soft p-3 shadow-[0_8px_24px_rgb(0_0_0/0.18)]"
    >
      <div className="min-w-0 flex-1">
        <p className="m-0 text-[15px] font-semibold">{n.title}</p>
        <p className="m-0 mt-0.5 line-clamp-2 text-[13px] text-muted">{n.todo}</p>
        <Link
          to={path as "/"}
          search={search as never}
          className="mt-1 inline-flex h-11 items-center text-[14px] font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
        >
          {t("nudge.go")}
        </Link>
      </div>
      <button
        type="button"
        aria-label={t("nudge.ack")}
        title={t("nudge.ack")}
        onClick={() => void resolveNudge(n.id ?? "", n.site)}
        className="flex size-11 shrink-0 items-center justify-center rounded-full text-muted hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
      >
        <X className="size-4" aria-hidden="true" />
      </button>
    </section>
  );
}
