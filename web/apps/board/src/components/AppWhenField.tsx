import { WhenField, type WhenShortcut } from "@evergenie/ui";
import { t } from "../i18n/zh-TW";
import { describeWhen, whenInputText } from "../lib/format";
import { parseWhenOnly } from "../lib/when";

function shortcuts(): WhenShortcut[] {
  return [
    {
      label: t("when.tonight"),
      date: () => {
        const now = new Date();
        const d = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 20, 0);
        if (d.getTime() <= now.getTime()) d.setDate(d.getDate() + 1);
        return d;
      },
    },
    {
      label: t("when.tomorrow"),
      date: () => {
        const now = new Date();
        return new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, 9, 0);
      },
    },
    {
      label: t("when.nextMonday"),
      date: () => {
        const now = new Date();
        const ahead = (8 - now.getDay()) % 7 || 7;
        return new Date(now.getFullYear(), now.getMonth(), now.getDate() + ahead, 9, 0);
      },
    },
  ];
}

export interface AppWhenFieldProps {
  value: Date | null;
  onChange: (value: Date | null) => void;
  error?: string;
  autoFocus?: boolean;
  onSubmit?: () => void;
}

// 時間欄位接上 when.ts 與繁中字串（解析器用瀏覽器的時區）。
export function AppWhenField(props: AppWhenFieldProps) {
  return (
    <WhenField
      label={t("when.label")}
      parse={(text) => parseWhenOnly(text)}
      describe={(d) => describeWhen(d)}
      toText={whenInputText}
      shortcuts={shortcuts()}
      placeholder={t("when.placeholder")}
      emptyHint={t("when.empty")}
      invalidText={t("when.invalid")}
      calendarLabel={t("when.calendar")}
      {...props}
    />
  );
}
