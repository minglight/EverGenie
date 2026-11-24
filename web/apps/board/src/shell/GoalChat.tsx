import type { Goal } from "@evergenie/sdk-web";
import { Avatar } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { MessageCircle } from "lucide-react";
import { useState } from "react";
import { ActionButton } from "../components/ActionButton";
import { siteName } from "../components/SiteTag";
import { useAgents } from "../data/agents";
import { threadsQuery } from "../data/queries";
import { homeSearch, type Site, useSiteApi } from "../data/sites";
import { t } from "../i18n/zh-TW";
import { reasonOf } from "../lib/reason";
import { ThreadView } from "../pages/chat/ThreadView";
import type { GoalChatSlot } from "../pages/goals/GoalDetailPage";
import { goalRoomName, goalRoomQuery, openGoalRoom } from "../pages/goals/goals";

// 跟負責的秘書聊這個 Goal（WEB-82）：一個 Goal 一個專屬對話（`<秘書>-<Goal 編號>`），跟平常的聊天分開，
// 之後回來接著聊（停了的對話也接回，不重送第一句）。Goal 頁不能 import 聊天頁（FEN-27），
// 所以畫面組在外殼這裡、由 router 交給 Goal 頁：寬螢幕是右邊一欄，窄螢幕是一顆按鈕開到聊天頁。
// 對話開在 Goal 所在的那台（`site`）：讀、開、送話、連結的 ?home= 都對那台；那台寫入前不能用就擋下，不退回主伺服器（FEN-28）。

function useGoalRoom(goal: Goal, site: Site) {
  const client = useQueryClient();
  const name = goalRoomName(goal);
  const room = useQuery(goalRoomQuery(name, site));
  const siteApi = useSiteApi(site.key);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const open = async (): Promise<string> => {
    if (!siteApi) {
      setError(t("sitePart.unavailable", { site: siteName(site) }));
      return "";
    }
    setBusy(true);
    setError("");
    try {
      const session = await openGoalRoom(goal, site);
      await Promise.all([
        client.invalidateQueries({ queryKey: threadsQuery(site).queryKey }),
        client.invalidateQueries({ queryKey: goalRoomQuery(name, site).queryKey }),
      ]);
      return session;
    } catch (err) {
      setError(t("goal.chat.failed", { reason: reasonOf(err) }));
      return "";
    } finally {
      setBusy(false);
    }
  };
  return { name, room, busy, error, open };
}

function GoalChatAside({ goal, site }: { goal: Goal; site: Site }) {
  const { view } = useAgents(site);
  const { data: threads } = useQuery(threadsQuery(site));
  const a = view(goal.agent ?? "");
  const { name, room, busy, error, open } = useGoalRoom(goal, site);
  const [started, setStarted] = useState(false);
  // 這個對話開過（停了也算）就直接顯示記錄；送話時聊天元件自己會叫醒它。
  const live = Boolean(room.data) || started;
  const summary = threads?.find((th) => th.name === name);

  return (
    <aside
      aria-label={t("goal.chat.title", { name: a.name })}
      data-testid="goal-chat"
      className="flex min-h-0 w-[420px] shrink-0 flex-col border-l border-border bg-surface"
    >
      <div className="flex min-h-[52px] items-center gap-2.5 border-b border-border px-4 py-2">
        <Avatar name={a.name} color={a.color} colorDark={a.colorDark} src={a.avatarUrl} size={28} />
        <p className="min-w-0 flex-1 truncate text-sm font-semibold">
          {t("goal.chat.title", { name: a.name })}
        </p>
        {live ? (
          <Link
            to="/chat/$session"
            params={{ session: name }}
            search={homeSearch(site)}
            className="flex min-h-10 shrink-0 items-center rounded-lg px-2 text-[13px] font-semibold text-accent hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
          >
            {t("goal.chat.open")}
          </Link>
        ) : null}
      </div>
      {live ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <ThreadView
            key={`${site.key}/${name}`}
            name={name}
            home={site}
            summary={summary}
            showBack={false}
            bare
          />
        </div>
      ) : room.isPending ? null : (
        <div className="flex flex-col items-start gap-3 px-5 py-6 text-sm text-muted">
          <p>{t("goal.chat.hint")}</p>
          <ActionButton
            size="card"
            tone="primary"
            busy={busy}
            data-testid="goal-chat-start"
            onClick={() => void open().then((s) => setStarted(Boolean(s)))}
          >
            {t("goal.chat.start", { name: a.name })}
          </ActionButton>
          {error ? (
            <p role="alert" className="text-danger">
              {error}
            </p>
          ) : null}
        </div>
      )}
    </aside>
  );
}

function GoalChatButton({ goal, site }: { goal: Goal; site: Site }) {
  const navigate = useNavigate();
  const { view } = useAgents(site);
  const { busy, error, open } = useGoalRoom(goal, site);
  const go = async () => {
    const session = await open();
    if (session) {
      void navigate({ to: "/chat/$session", params: { session }, search: homeSearch(site) });
    }
  };
  return (
    <div className="flex flex-col gap-1.5">
      <div>
        <ActionButton
          tone="secondary"
          busy={busy}
          data-testid="goal-chat-start"
          onClick={() => void go()}
        >
          <MessageCircle className="size-4" aria-hidden="true" />
          {t("goal.chat.title", { name: view(goal.agent ?? "").name })}
        </ActionButton>
      </div>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

export const goalChat: GoalChatSlot = { Aside: GoalChatAside, Button: GoalChatButton };
