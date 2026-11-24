import { ApiError, type Task } from "@evergenie/sdk-web";
import { cn, Field, showToast } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { MessageCircle } from "lucide-react";
import { type FormEvent, type ReactNode, useCallback, useEffect, useState } from "react";
import { primaryOf } from "../data/agents";
import {
  acceptTask,
  openTaskRoom,
  rejectTask,
  reportFailure,
  type SettleContext,
  useFailures,
  useSettled,
} from "../data/answers";
import { refreshAll } from "../data/live";
import { threadsQuery } from "../data/queries";
import { homeSearch, PRIMARY, type SiteRef, useSite, useSiteApi } from "../data/sites";
import { itemKey } from "../data/waiting";
import { t } from "../i18n/zh-TW";
import { chatDraftKey, seedDraft } from "../lib/draft";
import { isAsking, isDelivery } from "../lib/taskText";
import { ActionButton } from "./ActionButton";
import type { PaneActions } from "./AnswerPanel";
import { siteName } from "./SiteTag";

export const CLOSED_STATUSES = ["done", "cancelled"] as const;

export function isTaskOpen(task: Task): boolean {
  return !(CLOSED_STATUSES as readonly string[]).includes(task.status ?? "");
}

type Size = "card" | "page" | "pane";

interface CommonProps extends SettleContext {
  task: Task;
  size: Size;
  /** 桌機右欄：按鈕旁的鍵帽，並把 A／R 交給外面的鍵盤處理。 */
  keycaps?: boolean;
  bind?: (actions: PaneActions | null) => void;
  /** 動作成功之後（右欄用來跳到下一件）。 */
  onDone?: (kind: "accept" | "reject" | "chat" | "delete") => void;
  /** 秘書在問你、問句下面已經能就地回答（TaskAnswer，DEV-156 U-05）：「去聊」退成次要。 */
  answering?: boolean;
}

/** 去這張單的專屬對話（沒有就先開，REQ-56）。 */
export function OpenRoomButton({
  task,
  size,
  tone,
  keycap,
  label,
  icon,
  onOpened,
  bind,
  site = PRIMARY,
}: {
  task: Task;
  size: Size;
  tone: "primary" | "secondary" | "outline" | "quiet";
  keycap?: string;
  label: string;
  /** 文字前面的圖示（要 aria-hidden）。 */
  icon?: ReactNode;
  onOpened?: () => void;
  /** 桌機右欄：A 鍵＝開這個對話。 */
  bind?: (actions: PaneActions | null) => void;
  /** 這張單在哪一台（預設主伺服器）：對話開在那台、網址帶那台的 ?home=。 */
  site?: SiteRef;
}) {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const siteApi = useSiteApi(site.key);
  const found = useSite(site.key);
  const open = useCallback(async () => {
    if (!siteApi) {
      setError(t("sitePart.unavailable", { site: found ? siteName(found) : site.key }));
      return;
    }
    setBusy(true);
    setError("");
    try {
      const session = await openTaskRoom(task, site);
      onOpened?.();
      void navigate({
        to: "/chat/$session",
        params: { session },
        search: homeSearch(site),
      });
    } catch (err) {
      setError(t("answer.failed", { reason: err instanceof ApiError ? err.message : String(err) }));
    } finally {
      setBusy(false);
    }
  }, [task, site, siteApi, found, navigate, onOpened]);
  useEffect(() => {
    if (!bind) return;
    bind({ primary: () => void open(), secondary: () => {} });
    return () => bind(null);
  }, [bind, open]);
  return (
    <>
      <ActionButton size={size} tone={tone} keycap={keycap} busy={busy} onClick={() => void open()}>
        {icon}
        {label}
      </ActionButton>
      {error ? (
        <p role="alert" className="basis-full text-[13px] text-danger">
          {error}
        </p>
      ) : null}
    </>
  );
}

/**
 * 秘書在問你時的「去聊」（DEV-156 U-05）：只開對話，不替主人送任何話。這張單的對話開著就去那串；
 * 沒有就到負責秘書的主要討論串，輸入框先起個頭「關於「標題」：」（已經有草稿就不蓋，跟訊息頁「跟 X 聊這則」同一條路）。
 * 不叫 session.open：沒有對話時它會先把整張單當第一句送出去，看起來像主人自己說的。
 */
function GoChatButton({
  task,
  size,
  tone,
  keycap,
  bind,
  onOpened,
  site = PRIMARY,
}: {
  task: Task;
  size: Size;
  tone: "primary" | "secondary";
  keycap?: string;
  bind?: (actions: PaneActions | null) => void;
  onOpened?: () => void;
  site?: SiteRef;
}) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const threads = useQuery(threadsQuery(site)).data;
  const go = useCallback(async () => {
    // 對話清單還沒讀到就先讀一次：不然這張單明明有開著的工作對話，也會落到主要討論串。讀不到才當沒有。
    const list = threads ?? (await client.fetchQuery(threadsQuery(site)).catch(() => undefined));
    const work = list?.find((th) => th.task === task.id && th.kind === "work")?.name;
    const session = work || primaryOf(task.agent ?? "", site.key);
    if (!work) {
      seedDraft(
        chatDraftKey(site.key === "" ? session : `${site.key}/${session}`),
        t("message.chatDraft", { title: task.title ?? "" }),
      );
    }
    onOpened?.();
    void navigate({ to: "/chat/$session", params: { session }, search: homeSearch(site) });
  }, [threads, client, task, site, navigate, onOpened]);
  useEffect(() => {
    if (!bind) return;
    bind({ primary: () => void go(), secondary: () => {} });
    return () => bind(null);
  }, [bind, go]);
  return (
    <ActionButton
      size={size}
      tone={tone}
      keycap={keycap}
      onClick={() => void go()}
      data-testid="task-go-chat"
      data-tone={tone}
    >
      <MessageCircle className="size-[18px]" aria-hidden="true" />
      {t("task.goChat")}
    </ActionButton>
  );
}

// 要你驗收：「驗收」實心、「退回」次要；退回就地展開必填的理由（會通知秘書，所以不給復原）。
function ReviewActions({
  task,
  size,
  keycaps,
  bind,
  onDone,
  item,
  index,
  site = PRIMARY,
  extra,
}: CommonProps & { extra?: ReactNode }) {
  const client = useQueryClient();
  const key = itemKey(site, task.id ?? "");
  const settled = useSettled().get(key);
  const failure = useFailures().get(key);
  const [rejecting, setRejecting] = useState(false);
  const [note, setNote] = useState("");
  // 那台現在不是 ok 就不送（不退回主伺服器，FEN-28），說一句連不上。
  const siteApi = useSiteApi(site.key);
  const found = useSite(site.key);
  const usable = useCallback(() => {
    if (siteApi) return true;
    reportFailure(key, t("sitePart.unavailable", { site: found ? siteName(found) : site.key }));
    return false;
  }, [siteApi, found, key, site.key]);

  const accept = useCallback(async () => {
    if (!usable()) return;
    if (await acceptTask(client, task, { item, index, site })) onDone?.("accept");
  }, [client, task, item, index, site, usable, onDone]);

  const startReject = useCallback(() => setRejecting(true), []);

  useEffect(() => {
    if (!bind) return;
    bind({ primary: () => void accept(), secondary: startReject });
    return () => bind(null);
  }, [bind, accept, startReject]);

  if (settled) return null;

  const submitReject = async (e: FormEvent) => {
    e.preventDefault();
    const why = note.trim();
    if (why === "") return;
    if (!usable()) return;
    if (await rejectTask(client, task, why, { item, index, site })) onDone?.("reject");
  };

  return (
    <div className="flex w-full flex-col gap-2.5">
      {/* 驗收、退回、開專屬對話同一列；手機放不下就換行，換行後左緣跟「驗收」切齊。 */}
      <div className={cn("flex flex-wrap gap-2", size === "card" && "[&>button]:flex-1")}>
        <ActionButton
          size={size}
          tone="primary"
          keycap={keycaps ? "A" : undefined}
          onClick={() => void accept()}
        >
          {t("task.accept")}
        </ActionButton>
        <ActionButton
          size={size}
          tone="secondary"
          keycap={keycaps ? "R" : undefined}
          aria-expanded={rejecting}
          onClick={startReject}
        >
          {t("task.reject")}
        </ActionButton>
        {extra}
      </div>
      {rejecting ? (
        <form onSubmit={submitReject} className="hg-rise flex flex-col gap-2.5">
          <Field label={t("task.reject.reason")} hint={t("task.reject.hint")}>
            {(control) => (
              <textarea
                {...control}
                value={note}
                onChange={(e) => setNote(e.target.value)}
                rows={3}
                placeholder={t("task.reject.example")}
                // biome-ignore lint/a11y/noAutofocus: 按了「退回」才展開，直接輸入理由
                autoFocus
                className="w-full resize-none rounded-xl border border-border-strong bg-bg px-3 py-2.5 text-base leading-snug text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus"
              />
            )}
          </Field>
          <div className="flex gap-2">
            <ActionButton
              type="submit"
              size={size === "pane" ? "pane" : "card"}
              tone="danger"
              disabled={note.trim() === ""}
              className={size === "card" ? "flex-1" : undefined}
            >
              {t("task.reject.send")}
            </ActionButton>
            <ActionButton
              size={size === "pane" ? "pane" : "card"}
              tone="quiet"
              onClick={() => setRejecting(false)}
            >
              {t("ui.cancel")}
            </ActionButton>
          </div>
        </form>
      ) : null}
      {failure ? (
        <p role="alert" className="text-[13px] text-danger">
          {failure}
        </p>
      ) : null}
    </div>
  );
}

/** 待做的單開工（DEV-124）：改成進行中，伺服器開工作 session 並把單送給秘書（SES-27）；待做的單不留工作 session（SES-28）。 */
function StartTaskButton({
  task,
  size,
  site = PRIMARY,
}: {
  task: Task;
  size: Size;
  site?: SiteRef;
}) {
  const client = useQueryClient();
  const siteApi = useSiteApi(site.key);
  const found = useSite(site.key);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const start = async () => {
    if (!siteApi) {
      setError(t("sitePart.unavailable", { site: found ? siteName(found) : site.key }));
      return;
    }
    setBusy(true);
    setError("");
    try {
      await siteApi.tasks.update({ id: task.id ?? "", status: "doing" });
      showToast(t("task.start.toast", { id: task.id ?? "" }));
      await refreshAll(client, site);
    } catch (err) {
      setError(t("answer.failed", { reason: err instanceof ApiError ? err.message : String(err) }));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <ActionButton size={size} tone="primary" busy={busy} onClick={() => void start()}>
        {t("task.start")}
      </ActionButton>
      {error ? (
        <p role="alert" className="basis-full text-[13px] text-danger">
          {error}
        </p>
      ) : null}
    </>
  );
}

// 要你回答：去聊（問句下面能就地回答時是次要的，DEV-156 U-05）；待做：開始做；其他未結的單：開專屬對話
// （沒有指派秘書的待做單沒有按鈕）。
export function TaskActions(props: CommonProps) {
  const { task, size, keycaps, bind, onDone, site, answering = false } = props;
  const delivery = isDelivery(task);

  if (!isTaskOpen(task)) return null;
  const hasAgent = Boolean(task.agent);

  if (delivery) {
    return (
      <ReviewActions
        {...props}
        extra={
          hasAgent ? (
            <OpenRoomButton
              task={task}
              site={site}
              size={size}
              tone="secondary"
              label={t("task.openRoom")}
              icon={<MessageCircle className="size-[18px]" aria-hidden="true" />}
              onOpened={() => onDone?.("chat")}
            />
          ) : null
        }
      />
    );
  }
  if (!hasAgent) return null;
  if (task.status === "idea") {
    return (
      <div className={cn("flex flex-wrap gap-2", size === "card" && "[&>button]:flex-1")}>
        <StartTaskButton task={task} size={size} site={site} />
      </div>
    );
  }
  if (isAsking(task)) {
    return (
      <div className={cn("flex flex-wrap gap-2", size === "card" && "[&>button]:flex-1")}>
        <GoChatButton
          task={task}
          site={site}
          size={size}
          tone={answering ? "secondary" : "primary"}
          keycap={keycaps && !answering ? "A" : undefined}
          bind={answering ? undefined : bind}
          onOpened={() => onDone?.("chat")}
        />
      </div>
    );
  }
  return (
    <div className={cn("flex flex-wrap gap-2", size === "card" && "[&>button]:flex-1")}>
      <OpenRoomButton
        task={task}
        site={site}
        size={size}
        tone="secondary"
        label={t("task.openRoom")}
        onOpened={() => onDone?.("chat")}
      />
    </div>
  );
}
