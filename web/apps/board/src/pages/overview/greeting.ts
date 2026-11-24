import { t } from "../../i18n/zh-TW";
import { dateLabel, greetingPart } from "../../lib/format";

/** 「早安，<主人>」（home.toml user.name；還沒讀到就只有「早安」）；依瀏覽器時區的時間。 */
export function greetingText(now: Date, name: string): string {
  const part = greetingPart(now);
  return name ? t(`today.greeting.${part}`, { name }) : t(`today.greeting.${part}.plain`);
}

/** 「10 月 24 日（六）· 3 件等你處理」；還沒拿到數字時只寫日期。 */
export function todaySubtitle(now: Date, waitingTotal: number | undefined): string {
  const date = dateLabel(now);
  if (waitingTotal === undefined) return date;
  return waitingTotal > 0
    ? t("today.subtitle.waiting", { date, n: waitingTotal })
    : t("today.subtitle.none", { date });
}
