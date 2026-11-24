import { useQuery } from "@tanstack/react-query";
import { AnswerButtons, isPending } from "../../components/AnswerPanel";
import { notificationsQuery } from "../../data/queries";
import { PRIMARY, type SiteRef } from "../../data/sites";
import { t } from "../../i18n/zh-TW";

/** 這個討論串裡等你回答的「工作 session 超過上限」提問（P7-5、D-33）：照開／先排隊／關掉 <單號>。
 * 它是 inline 通知：只在看板和這個討論串，不推播。權限與問題另有 PendingCard，這裡不重複。
 * 別台的對話讀那台的通知、回答送那台（S3-7b）。 */
export function WorkAsk({ session, site = PRIMARY }: { session: string; site?: SiteRef }) {
  const { data } = useQuery(notificationsQuery(site));
  const asks = (data ?? []).filter((n) => n.inline && isPending(n) && n.room === session);
  if (asks.length === 0) return null;
  return (
    <>
      {asks.map((n) => (
        <section
          key={`${site.key}/${n.id}`}
          aria-label={t("chat.workAsk")}
          data-testid="work-ask"
          className="hg-rise flex flex-col gap-2 rounded-[14px] border border-attention bg-attention-soft p-3"
        >
          <p className="m-0 text-[15px] font-semibold">{n.title}</p>
          {n.context ? (
            <p className="m-0 whitespace-pre-line text-[13px] text-muted">{n.context}</p>
          ) : null}
          <AnswerButtons notification={n} size="card" site={site} />
        </section>
      ))}
    </>
  );
}
