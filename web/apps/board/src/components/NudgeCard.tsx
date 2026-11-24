import { useState } from "react";
import { useViewedHome } from "../data/sites";
import { resolveNudge, type SiteNudge, useNudges } from "../data/viewing";
import { t } from "../i18n/zh-TW";
import { ActionButton } from "./ActionButton";

/** 這張單／這個討論串上「還在等你」的提醒（P7-6、D-34）：頁內黃色卡片。
 * 只顯示伺服器說還沒收掉的；2 分鐘沒按、或離開這一頁，伺服器會改推到手機；任一處回答，卡片自己消失。
 * site＝這張單／討論串是哪一台的（台的 key，主伺服器 ""）：別台同名的不算（S3-8）。沒給就照網址的 `?home=`
 * （跟外殼報在場、判斷站內提示要不要藏起來用同一個來源，兩邊才一致）。 */
export function NudgeCard({
  session,
  task,
  site,
}: {
  session?: string;
  task?: string;
  site?: string;
}) {
  const viewed = useViewedHome();
  const here = site ?? viewed;
  // 帶 key 的是綁在對話上的等待（選擇題、權限確認，DEV-49 S2）：對話裡已經有確認卡，這裡不重複。
  const nudges = useNudges().filter(
    (n) =>
      (n.site ?? "") === here &&
      !n.resolved &&
      !n.key &&
      ((!!session && n.session === session) || (!!task && n.task === task)),
  );
  if (nudges.length === 0) return null;
  return (
    <>
      {nudges.map((n) => (
        <NudgeItem key={n.id} nudge={n} />
      ))}
    </>
  );
}

function NudgeItem({ nudge: n }: { nudge: SiteNudge }) {
  const [busy, setBusy] = useState(false);
  return (
    <section
      aria-label={t("nudge.card")}
      data-testid="nudge-card"
      className="hg-rise flex flex-col gap-2 rounded-[14px] border border-attention bg-attention-soft p-3"
    >
      <p className="m-0 text-[15px] font-semibold">{n.title}</p>
      {n.context ? <p className="m-0 text-[13px] text-muted">{n.context}</p> : null}
      <p className="m-0 text-[14px]">{n.todo}</p>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <ActionButton
          size="card"
          tone="secondary"
          busy={busy}
          onClick={() => {
            setBusy(true);
            void resolveNudge(n.id ?? "", n.site).finally(() => setBusy(false));
          }}
        >
          {t("nudge.ack")}
        </ActionButton>
        <span className="text-xs text-muted">{t("nudge.escalate")}</span>
      </div>
    </section>
  );
}
