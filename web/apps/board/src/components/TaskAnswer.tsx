import type { Notification, Task } from "@evergenie/sdk-web";
import { cn } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
import type { SettleContext } from "../data/answers";
import { notificationsQuery } from "../data/queries";
import { PRIMARY, type SiteRef } from "../data/sites";
import { itemKey } from "../data/waiting";
import { isAsking } from "../lib/taskText";
import { AnswerButtons, AnswerTyped, isPending, optionsOf, type PaneActions } from "./AnswerPanel";

/**
 * 這張單上還等你回答的問句（DEV-156 U-05）：綁這張單、問句就是單上那句（task.ask）、還沒回的通知——
 * TSK-6 進等你時推的那則（只收打字，TSK-27）。回答了單自己回到進行中（DEV-26）。
 * - 只拿單上那句：秘書另外綁單問、內容不同的在今天頁本來就是自己一張卡、在訊息頁回（TSK-26）；
 *   這裡再畫一次就是同一個問題兩處、答了一邊另一邊不收。
 * - 同一句有好幾則（舊資料以前催一次就多一則、秘書另外發的同一句）只拿一則：先挑只收打字的（tasks 推的那則，
 *   回答會轉回秘書，NTF-48），再挑最新的；回了它，後端把同一句的其他則一起收掉（TSK-30）。
 * 沒有要你回答就不讀（空陣列）。跟訊息頁同一份 notificationsQuery 快取。
 */
export function useTaskAsks(task: Task | undefined, site: SiteRef = PRIMARY): Notification[] {
  const asking = task !== undefined && isAsking(task);
  const { data } = useQuery({ ...notificationsQuery(site), enabled: asking });
  if (!task || !asking) return [];
  const id = task.id ?? "";
  const ask = task.ask ?? "";
  let best: Notification | undefined;
  for (const n of data ?? []) {
    if (!isPending(n) || n.inline || n.task !== id || (n.todo ?? "") !== ask) continue;
    if (!best || better(n, best)) best = n;
  }
  return best ? [best] : [];
}

// 只收打字的優先（回答一定轉回秘書），一樣的話新的優先。
function better(n: Notification, than: Notification): boolean {
  if (!!n.typed !== !!than.typed) return !!n.typed;
  return (n.created_at ?? "") > (than.created_at ?? "");
}

interface TaskAnswerProps extends Pick<SettleContext, "item" | "index"> {
  task: Task;
  asks: Notification[];
  size: "card" | "page" | "pane";
  site?: SiteRef;
  /** 桌機右欄：A＝第一個選項（只收打字的就把游標放進輸入框）、R＝第二個選項。 */
  bind?: (actions: PaneActions | null) => void;
  /** 送出成功之後（桌機右欄用來跳到下一件）。 */
  onAnswered?: (text: string) => void;
  className?: string;
}

// 秘書在問你：問句下面直接回答（DEV-156 U-05，跟通知的「要你同意」同一條路 answerNotification）。
// 秘書給了選項就是按鈕（按一個就送出，一次只選一個）；只收打字的（TSK-27）是一個輸入框＋送出。
// 回答後收成綠色一列記在這張單上（settleAs），今天頁的卡也跟著收起來。「去聊」另外在 TaskActions，是次要的。
export function TaskAnswer({
  task,
  asks,
  size,
  site = PRIMARY,
  item,
  index,
  bind,
  onAnswered,
  className,
}: TaskAnswerProps) {
  const id = task.id ?? "";
  const settleAs = useMemo(() => ({ key: itemKey(site, id), id }), [site, id]);
  if (asks.length === 0) return null;
  return (
    <div data-testid="task-answer" className={cn("flex flex-col gap-3", className)}>
      {asks.map((n, i) => (
        <TaskAsk
          // 用單號＋問句當 key：催的時候換成新的那則，打到一半的字與游標還在（DEV-156 review）。
          key={`${id}:${n.todo ?? ""}`}
          notification={n}
          size={size}
          site={site}
          item={item}
          index={index}
          settleAs={settleAs}
          bind={i === 0 ? bind : undefined}
          onAnswered={onAnswered}
        />
      ))}
    </div>
  );
}

function TaskAsk({
  notification: n,
  size,
  bind,
  ...ctx
}: SettleContext & {
  notification: Notification;
  size: "card" | "page" | "pane";
  bind?: (actions: PaneActions | null) => void;
  onAnswered?: (text: string) => void;
}) {
  const options = optionsOf(n);
  const box = useRef<HTMLDivElement>(null);
  // 只收打字的：A 把游標放進輸入框（有選項的交給 AnswerButtons 自己綁）。
  useEffect(() => {
    if (!bind || options.length > 0) return;
    bind({
      primary: () => box.current?.querySelector("input")?.focus(),
      secondary: () => {},
    });
    return () => bind(null);
  }, [bind, options.length]);
  return (
    <div ref={box} className="flex flex-col gap-2">
      {/* 只收打字的沒有按鈕：AnswerButtons 這時只顯示送不出去的原因。 */}
      <AnswerButtons
        notification={n}
        size={size}
        keycaps={size === "pane" && options.length > 0}
        bind={options.length > 0 ? bind : undefined}
        {...ctx}
      />
      {/* 只收打字的給輸入框。有選項的：卡片上只給按鈕；詳情頁只在綁了對話時另給輸入框——沒綁對話、帶選項的那則，
          打的字不會轉回秘書（NTF-48），給了框主人以為說了，秘書其實讀不到；要多說就「去聊」。 */}
      {options.length === 0 || (size !== "card" && n.room) ? (
        <AnswerTyped notification={n} {...ctx} />
      ) : null}
    </div>
  );
}
