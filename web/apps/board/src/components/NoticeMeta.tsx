import type { Category, Notification } from "@evergenie/sdk-web";
import type { ReactNode } from "react";
import { t } from "../i18n/zh-TW";
import { Pill, type PillTone } from "./Pill";

export const categoryTone: Record<Category, PillTone> = {
  approve: "attention",
  done: "ok",
  due: "accent",
  digest: "muted",
  opening: "accent",
};

export const categoryKey = {
  approve: "notice.cat.approve",
  done: "notice.cat.done",
  due: "notice.cat.due",
  digest: "notice.cat.digest",
  opening: "notice.cat.opening",
} as const satisfies Record<Category, string>;

export function CategoryPill({ category }: { category: Category | undefined }) {
  const c = category ?? "digest";
  return <Pill tone={categoryTone[c]}>{t(categoryKey[c])}</Pill>;
}

// 一則訊息的膠囊：「要你同意」回答過（或過期）之後不再掛黃色的「要你同意」，改成灰色「已處理」，
// 不然看起來像狀態沒更新（作者 10-27：M-27 回了「不要」外面還寫要你同意）。
export function NoticePill({ n }: { n: Notification }) {
  if (n.category === "approve" && n.state !== "unread" && n.state !== "read") {
    return <Pill tone="muted">{t("notice.cat.approve.handled")}</Pill>;
  }
  return <CategoryPill category={n.category} />;
}

// 詳細頁上方那一行：分類膠囊 · 編號 · 秘書 · 時間（看多台時最後再加台名標籤，site）。
export function NoticeMeta({
  n,
  agentName,
  time,
  site,
}: {
  n: Notification;
  agentName: string;
  time: string;
  site?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted">
      <NoticePill n={n} />
      <span className="font-mono">{n.id}</span>
      <span aria-hidden="true">·</span>
      <span>{agentName}</span>
      {time ? (
        <>
          <span aria-hidden="true">·</span>
          <span>{time}</span>
        </>
      ) : null}
      {site}
    </div>
  );
}
