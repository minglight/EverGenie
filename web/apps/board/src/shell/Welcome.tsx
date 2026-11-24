import type { PersonaCard } from "@evergenie/sdk-web";
import { Avatar, Spinner } from "@evergenie/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { ActionButton, actionButtonClass } from "../components/ActionButton";
import { KnowledgePicker } from "../components/KnowledgePicker";
import { LoadFailed } from "../components/StateViews";
import { enabledIds, knowledgeQuery } from "../data/knowledge";
import { packageListQuery } from "../data/queries";
import { t } from "../i18n/zh-TW";
import { reasonOf } from "../lib/reason";
import { forgetEmptyRoster, hireSecretary, personasQuery } from "../pages/settings/queries";
import { GenieMark } from "./GenieMark";
import { DevStrip, HomeLabel, homeTitle, useHome } from "./HomeMark";
import {
  parseWelcomeSearch,
  skipWelcome,
  WELCOME_PATH,
  type WelcomeJob,
  type WelcomeSearch,
  welcomeBack,
  welcomeJobs,
  welcomePersonas,
  welcomeStepNumber,
  welcomeSteps,
  welcomeView,
} from "./welcomeFlow";

// 新家第一次打開的歡迎頁（WEB-176，DEV-156 U-03，作者 2026-11-14：「獨立的歡迎頁」）：
// 名冊上一位秘書都沒有時，外殼不畫看板（沒有選單、沒有設定分頁），整個畫面只有這一頁。
// 一頁一件事：①這是什麼 ②先請秘書幫什麼（生活／健康／工作）③請哪位角色 ④請她開始上班 → 今天頁。
// 步驟寫在網址（?step=、job=、persona=），重新整理與上一頁都回到同一步（UXC-3）。
// 帳號、模型、權限一律用預設，不在這裡問（D-40）；請秘書上班用跟設定頁同一個動作（hireSecretary，WEB-47）。

const TOTAL_STEPS = welcomeSteps.length + 1;

const cardClass =
  "flex min-h-[72px] w-full items-center gap-3 rounded-[18px] border border-border bg-surface px-4 py-3.5 text-left text-fg no-underline shadow-[var(--shadow-card)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:bg-hover";

function welcomeLink(search: WelcomeSearch) {
  return { to: WELCOME_PATH, search } as const;
}

/** 外框：上面標誌與第幾步，中間內容（太長可以捲），下面是主要按鈕與上一步（手機拇指碰得到的位置）。 */
function Frame({
  step,
  number,
  title,
  lead,
  children,
  footer,
  back,
}: {
  step: string;
  number: number;
  title: string;
  lead?: string;
  children?: ReactNode;
  footer?: ReactNode;
  back: WelcomeSearch | null;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  // 換步驟時把焦點放到新標題：報讀器會念出這一步，鍵盤也從這一步的開頭開始。
  // biome-ignore lint/correctness/useExhaustiveDependencies: 只在步驟換了的時候做一次。
  useEffect(() => {
    heading.current?.focus();
  }, [step]);
  return (
    <div className="flex h-dvh flex-col bg-bg text-fg">
      <DevStrip />
      <main
        data-testid="welcome"
        data-step={step}
        className="mx-auto flex min-h-0 w-full max-w-xl flex-1 flex-col px-5 pt-[max(env(safe-area-inset-top),20px)]"
      >
        <div className="flex shrink-0 items-center justify-between gap-3">
          <span className="flex min-w-0 items-center gap-2">
            <GenieMark className="size-9 shrink-0" />
            {/* 家名徽章跟看板同一條規則（HomeLabel）：只有一個家的正式家不畫；開發家、看兩個家以上才畫。 */}
            <HomeLabel />
          </span>
          <p data-testid="welcome-progress" className="shrink-0 text-[13px] text-muted">
            {t("welcome.progress", { n: number, total: TOTAL_STEPS })}
          </p>
        </div>
        <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto py-6">
          <div className="flex flex-col gap-2">
            <h1
              ref={heading}
              tabIndex={-1}
              className="text-[26px] leading-tight font-bold break-words outline-none"
            >
              {title}
            </h1>
            {lead ? <p className="text-[16px] leading-relaxed text-muted">{lead}</p> : null}
          </div>
          {children}
        </div>
        <div className="flex shrink-0 flex-col gap-2 pt-2 pb-[max(env(safe-area-inset-bottom),16px)]">
          {footer}
          {back ? (
            <Link
              {...welcomeLink(back)}
              data-testid="welcome-back"
              className="flex min-h-11 w-fit items-center gap-1 rounded-xl pr-3 text-[15px] font-medium text-muted no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
            >
              <ChevronLeft className="size-5" aria-hidden="true" />
              {t("welcome.back")}
            </Link>
          ) : null}
        </div>
      </main>
    </div>
  );
}

const bigButton = "h-[52px] w-full text-[17px]";

/** 走不下去的時候（這個家沒有可以請的職位或角色）：說明＋一顆「先進看板」，不把主人卡在這一頁。 */
function DeadEnd({ text, to }: { text: string; to: "/settings/setup" | "/personas" }) {
  const navigate = useNavigate();
  return (
    <div data-testid="welcome-dead-end" className="flex flex-col gap-3">
      <p className="text-[15px] leading-relaxed">{text}</p>
      <ActionButton
        tone="outline"
        className="w-fit"
        onClick={() => {
          skipWelcome();
          void navigate({ to, replace: true });
        }}
      >
        {t("welcome.skip")}
      </ActionButton>
    </div>
  );
}

/** 歡迎頁第一步的一行連結（DEV-153）：想先把手機接上的人，直接進看板的「設定 → 手機連線」。 */
function PhoneLink() {
  const navigate = useNavigate();
  return (
    <button
      type="button"
      data-testid="welcome-phone"
      className="flex min-h-11 w-fit items-center gap-1 rounded-xl text-left text-[15px] font-medium text-accent underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      onClick={() => {
        skipWelcome();
        void navigate({ to: "/settings/phone", replace: true });
      }}
    >
      {t("welcome.intro.phone")}
    </button>
  );
}

function PersonaTile({ card, job }: { card: PersonaCard; job: WelcomeJob }) {
  const name = card.name || card.slug || "";
  return (
    <li>
      <Link
        {...welcomeLink({ step: "start", job: job.fn.id, persona: card.slug })}
        data-testid="welcome-persona"
        data-slug={card.slug}
        className={cardClass}
      >
        <Avatar name={name} src={card.avatar_url || undefined} size={56} />
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span
            data-testid="welcome-persona-name"
            className="text-[17px] leading-snug font-semibold break-words"
          >
            {name}
          </span>
          {card.tagline ? (
            <span className="text-[14px] leading-snug break-words text-muted">{card.tagline}</span>
          ) : null}
        </span>
        <ChevronRight className="size-5 shrink-0 text-muted" aria-hidden="true" />
      </Link>
    </li>
  );
}

function StartStep({ job, persona }: { job: WelcomeJob; persona: PersonaCard }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const name = persona.name || persona.slug || "";
  // 這個職位有可選知識就列在頭像下面（DEV-161）：同一畫面、不多一步，預設勾好；讀不到或沒有就不出現，不擋「開始上班」。
  // 還沒有秘書紀錄：沒動過的就用官方預設（default）；主人動過的放在 picked。
  const knowledge = useQuery(knowledgeQuery(job.fn.id));
  const blocks = knowledge.data ?? [];
  const [picked, setPicked] = useState<Record<string, boolean>>({});
  const checked = new Set(blocks.filter((b) => picked[b.id] ?? b.default).map((b) => b.id));
  const hire = useMutation({
    mutationFn: () =>
      hireSecretary(
        job.fn,
        persona.slug ?? "",
        blocks.length > 0
          ? enabledIds(blocks.map((b) => ({ ...b, enabled: checked.has(b.id) })))
          : undefined,
      ),
    // 請成功就直接到今天，不等、也不靠名冊重讀成功：先把「名冊是空的」那份舊答案丟掉（forgetEmptyRoster），
    // 外殼沒有名冊時照常畫看板；重讀失敗也不會因為舊的空名冊被彈回歡迎頁第一步。失敗時留在這一步，看得到原因、可以再試。
    onSuccess: async () => {
      forgetEmptyRoster(client);
      await navigate({ to: "/", replace: true });
    },
  });
  const failed = hire.isError;
  return (
    <Frame
      step="start"
      number={welcomeStepNumber("start")}
      title={t("welcome.start.title", { name, job: job.title })}
      lead={t("welcome.start.lead")}
      back={welcomeBack({ step: "start", job, persona })}
      footer={
        <>
          {failed ? (
            <p role="alert" data-testid="welcome-error" className="text-[14px] text-danger">
              {t("welcome.start.failed", { reason: reasonOf(hire.error) })}
            </p>
          ) : null}
          <ActionButton
            tone="primary"
            className={bigButton}
            data-testid="welcome-hire"
            busy={hire.isPending || hire.isSuccess}
            onClick={() => hire.mutate()}
          >
            {failed ? t("welcome.start.retry") : t("welcome.start.go", { name })}
          </ActionButton>
        </>
      }
    >
      <div className="flex flex-col items-center gap-3 py-2 text-center">
        {/* 有知識要勾的時候頭像小一點，整頁不會比以前長很多（手機寬度）。 */}
        <Avatar
          name={name}
          src={persona.avatar_url || undefined}
          size={blocks.length > 0 ? 80 : 112}
        />
        {persona.tagline ? (
          <p className="max-w-sm text-[15px] leading-relaxed text-muted">{persona.tagline}</p>
        ) : null}
      </div>
      <KnowledgePicker
        agent={job.fn.id}
        blocks={blocks}
        checked={checked}
        disabled={hire.isPending || hire.isSuccess}
        onToggle={(id, next) => setPicked((p) => ({ ...p, [id]: next }))}
      />
    </Frame>
  );
}

export function Welcome() {
  const search = parseWelcomeSearch(
    useLocation({ select: (l) => l.search }) as Record<string, unknown>,
  );
  const home = useHome();
  const packages = useQuery(packageListQuery());
  const personas = useQuery(personasQuery);
  useEffect(() => {
    document.title = homeTitle(home.dev, `${t("welcome.docTitle")} · ${t("app.title")}`);
  }, [home.dev]);

  if (!search.step) {
    return (
      <Frame
        step="intro"
        number={welcomeStepNumber("intro")}
        title={
          home.userName
            ? t("welcome.intro.titleNamed", { name: home.userName })
            : t("welcome.intro.title")
        }
        lead={t("welcome.intro.lead")}
        back={null}
        footer={
          <Link
            {...welcomeLink({ step: "job" })}
            data-testid="welcome-begin"
            className={actionButtonClass("primary", "page", `${bigButton} no-underline`)}
          >
            {t("welcome.intro.go")}
          </Link>
        }
      >
        <p className="text-[15px] leading-relaxed text-muted">{t("welcome.intro.more")}</p>
        <PhoneLink />
      </Frame>
    );
  }

  // 後面幾步要知道這個家有哪些職位與角色：還沒讀到先轉圈，讀不到就白話說明＋再試一次。
  if (packages.data === undefined || personas.data === undefined) {
    const failed = packages.isError || personas.isError;
    return (
      <Frame
        step="loading"
        number={welcomeStepNumber(search.step)}
        title={t("welcome.job.title")}
        back={{}}
      >
        {failed ? (
          <LoadFailed
            what={t("welcome.loadWhat")}
            onRetry={() => {
              void packages.refetch();
              void personas.refetch();
            }}
          />
        ) : (
          <div className="flex justify-center py-10">
            <Spinner label={t("ui.loading")} />
          </div>
        )}
      </Frame>
    );
  }

  const jobs = welcomeJobs(packages.data.packages ?? [], t);
  const view = welcomeView(search, jobs, personas.data);

  if (view.step === "start" && view.job && view.persona) {
    return <StartStep job={view.job} persona={view.persona} />;
  }

  if (view.step === "persona" && view.job) {
    const job = view.job;
    const cards = welcomePersonas(personas.data, job);
    return (
      <Frame
        step="persona"
        number={welcomeStepNumber("persona")}
        title={t("welcome.persona.title", { job: job.title })}
        lead={cards.length > 0 ? t("welcome.persona.lead") : undefined}
        back={welcomeBack(view)}
      >
        {cards.length > 0 ? (
          <ul className="m-0 flex list-none flex-col gap-3 p-0">
            {cards.map((card) => (
              <PersonaTile key={card.slug} card={card} job={job} />
            ))}
          </ul>
        ) : (
          <DeadEnd text={t("welcome.persona.none")} to="/personas" />
        )}
      </Frame>
    );
  }

  return (
    <Frame
      step="job"
      number={welcomeStepNumber("job")}
      title={t("welcome.job.title")}
      lead={jobs.length > 0 ? t("welcome.job.lead") : undefined}
      back={welcomeBack({ step: "job" })}
    >
      {jobs.length > 0 ? (
        <>
          <ul className="m-0 flex list-none flex-col gap-3 p-0">
            {jobs.map((job) => (
              <li key={job.fn.id}>
                <Link
                  {...welcomeLink({ step: "persona", job: job.fn.id })}
                  data-testid="welcome-job"
                  data-job={job.fn.id}
                  className={cardClass}
                >
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="text-[19px] leading-snug font-semibold break-words">
                      {job.title}
                    </span>
                    {job.hint ? (
                      <span className="text-[14px] leading-snug break-words text-muted">
                        {job.hint}
                      </span>
                    ) : null}
                  </span>
                  <ChevronRight className="size-5 shrink-0 text-muted" aria-hidden="true" />
                </Link>
              </li>
            ))}
          </ul>
          <p className="text-[13px] leading-relaxed text-muted">{t("welcome.job.later")}</p>
        </>
      ) : (
        <DeadEnd text={t("welcome.job.none")} to="/settings/setup" />
      )}
    </Frame>
  );
}
