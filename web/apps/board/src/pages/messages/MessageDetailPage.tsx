import { useParams } from "@tanstack/react-router";
import { NotificationView } from "../../components/NotificationView";
import { LoadingBlock, NotFoundBlock } from "../../components/StateViews";
import { useHomeSite } from "../../data/sites";
import { itemKey } from "../../data/waiting";
import { t } from "../../i18n/zh-TW";
import { useIsWide } from "../../lib/media";
import { MessagesPage } from "./MessagesPage";

// /messages/$id：通知的深連結落地頁。選項按鈕在第一屏（REQ-138）。
// ≥ 1024px 是訊息頁的兩欄（左清單、右這一則），推播或別處的連結進來就在清單旁打開。
// 別台的訊息網址帶 ?home=<台名>（S3-7a）：從那台讀、回答送到那台，重新整理還在；那台還在試連就先轉圈，設定裡沒有那台就說找不到。
export function MessageDetailPage() {
  const { id } = useParams({ strict: false }) as { id: string };
  const wide = useIsWide();
  const { site, pending } = useHomeSite();
  if (wide)
    return <MessagesPage selected={id} selectedSite={site ?? null} selectedPending={pending} />;
  return (
    <div className="pb-2">
      {site ? (
        <NotificationView key={itemKey(site, id)} id={id} site={site} />
      ) : pending ? (
        <LoadingBlock />
      ) : (
        <NotFoundBlock
          title={t("message.notFound")}
          hint={t("message.notFound.hint")}
          backLabel={t("message.back")}
          to="/messages"
        />
      )}
    </div>
  );
}
