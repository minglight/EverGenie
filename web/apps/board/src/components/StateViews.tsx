import { Spinner, LoadFailed as UiLoadFailed } from "@evergenie/ui";
import { Link } from "@tanstack/react-router";
import { CircleHelp } from "lucide-react";
import { t } from "../i18n/zh-TW";
import { PageTitle } from "./PageTitle";

export function LoadingBlock() {
  return (
    <div className="flex justify-center py-16">
      <Spinner label={t("ui.loading")} />
    </div>
  );
}

/** 核心頁的「載不進來」：就是 `@evergenie/ui` 的那一個（套件頁也用它，同一句話只有一份）；核心頁有輪詢，會自己再試。 */
export function LoadFailed({ what, onRetry }: { what: string; onRetry: () => void }) {
  return <UiLoadFailed what={what} onRetry={onRetry} auto />;
}

/** 找不到這筆資料（編號打錯、已被整理掉）：白話說明加回清單的連結。 */
export function NotFoundBlock({
  title,
  hint,
  backLabel,
  to,
}: {
  title: string;
  hint: string;
  backLabel: string;
  to: "/messages" | "/tasks" | "/reminders" | "/tasks/jobs" | "/goals" | "/personas";
}) {
  return (
    <div className="flex flex-col items-center gap-1.5 px-6 py-16 text-center">
      {/* 找不到不是好消息：灰色問號，不用綠勾（DEV-156 U-28）。 */}
      <CircleHelp className="size-7 text-muted" strokeWidth={1.8} aria-hidden="true" />
      <PageTitle className="text-[17px] font-semibold">{title}</PageTitle>
      <p className="text-[13px] text-muted">{hint}</p>
      <Link
        to={to}
        replace
        className="mt-3 flex min-h-11 items-center rounded-xl bg-accent px-5 text-[15px] font-semibold text-accent-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      >
        {backLabel}
      </Link>
    </div>
  );
}
