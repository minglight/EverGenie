import { useParams } from "@tanstack/react-router";
import { SiteGate } from "../../components/SiteGate";
import { t } from "../../i18n/zh-TW";
import { ReminderView } from "./ReminderView";

export function ReminderDetailPage() {
  const { id } = useParams({ strict: false }) as { id: string };
  return (
    <div className="pb-2">
      {/* ?home= 指的那台（沒寫＝主伺服器，跟 S3 之前一樣；S3-7b）。 */}
      <SiteGate back={{ to: "/reminders", label: t("reminder.back") }}>
        {(site) => <ReminderView key={`${site.key}/${id}`} id={id} site={site} />}
      </SiteGate>
    </div>
  );
}
