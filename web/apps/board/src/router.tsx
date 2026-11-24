import { createRootRoute, createRoute, createRouter, redirect } from "@tanstack/react-router";
import { flushDeferred, installDeferredFlush } from "./data/deferred";
import { type HomeSearch, parseHomeSearch } from "./data/sites";
import type { TextKey } from "./i18n/zh-TW";
import { ChatPage } from "./pages/chat/ChatPage";
import { GoalDetailPage } from "./pages/goals/GoalDetailPage";
import { GoalsPage } from "./pages/goals/GoalsPage";
import type { GoalView } from "./pages/goals/goals";
import { MessageDetailPage } from "./pages/messages/MessageDetailPage";
import { MessagesPage, type MessagesSearch } from "./pages/messages/MessagesPage";
import { OverviewPage } from "./pages/overview/OverviewPage";
import type { TodaySearch } from "./pages/overview/WideToday";
import { PolicyPage } from "./pages/policy/PolicyPage";
import { ReminderDetailPage } from "./pages/reminders/ReminderDetailPage";
import { RemindersPage } from "./pages/reminders/RemindersPage";
import {
  AgentsSettingsPage,
  DevicesSettingsPage,
  DisplaySettingsPage,
  HealthSettingsPage,
  LogSettingsPage,
  MachineSettingsPage,
  MenuSettingsPage,
  NotifySettingsPage,
  PackagesSettingsPage,
  PersonaPage,
  PersonasPage,
  PhoneSettingsPage,
  RulesSettingsPage,
  SetupSettingsPage,
  TrustSettingsPage,
} from "./pages/settings/pages";
import { SettingsHome } from "./pages/settings/SettingsHome";
import { tabLabel } from "./pages/settings/SettingsLayout";
import { JobsPage } from "./pages/tasks/JobsPage";
import { TaskDetailPage } from "./pages/tasks/TaskDetailPage";
import { TasksPage } from "./pages/tasks/TasksPage";
import { taskSearchOf } from "./pages/tasks/taskViews";
import { PackagePage } from "./pkg/PackagePage";
import { PACKAGE_ROUTE } from "./pkg/routes";
import { goalChat } from "./shell/GoalChat";
import { NotFoundPage } from "./shell/NotFound";
import { Shell } from "./shell/Shell";
import { parseWelcomeSearch, WELCOME_PATH } from "./shell/welcomeFlow";

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
  interface StaticDataRouteOption {
    /** 分頁標題與手機頁首的標題（i18n key）。 */
    title?: TextKey;
    /** 手機頁首改寫這個（例：設定的各頁只寫「通知」，不寫「設定 · 通知」，DEV-156 U-12）；分頁標題照 title。 */
    mobileTitle?: TextKey;
    /** 星號路由（`…/$`）：星號不是空的就是找不到頁，外殼不用這條的標題（設定首頁，DEV-156 U-12）。 */
    splatNotFound?: boolean;
    /** 手機上整頁畫面（討論串）：收起外殼的標題列和底部列。 */
    hideMobileBar?: boolean;
    /** "today"：手機頁首換成早安／午安／晚安與日期。 */
    header?: "today";
    /** 手機（< 1024px）：外殼在內容上方畫秘書篩選列（今天、聊天清單、待辦、任務單、訊息）。 */
    agentFilter?: boolean;
  }
}

const root = createRootRoute({ component: Shell, notFoundComponent: NotFoundPage });

const text = (v: unknown): string | undefined =>
  typeof v === "string" && v !== "" ? v : undefined;
const oneOf = <T extends string>(v: unknown, allowed: readonly T[]): T | undefined =>
  allowed.includes(v as T) ? (v as T) : undefined;

// 看板的多台（S3）：聊天與各詳情頁的 ?home=<台名> 指出那筆資料在哪一台；沒寫＝主伺服器（網址跟 S3 之前一樣）。
// 頁面用 data/sites.ts 的 useHomeParam／useHomeSite 讀，連過去用 homeSearch(site) 帶。
const homeOf = parseHomeSearch;

const todayFilters = ["approve", "review", "ask"] as const;
const messageFilters = ["approve", "all"] as const;
const messageLanes = ["system"] as const;
const policyViewNames = ["applied", "history"] as const;
// 待辦頁的分頁是 open／waiting／done（DEV-107）；舊網址的 today、overdue 等七種照舊收，頁面把它們當成「未完成」。
const reminderViews = [
  "open",
  "waiting",
  "done",
  "today",
  "overdue",
  "this_week",
  "upcoming",
  "no_time",
  "done_yesterday",
] as const;

const overview = createRoute({
  getParentRoute: () => root,
  path: "/",
  staticData: { title: "page.overview", header: "today", agentFilter: true },
  validateSearch: (s: Record<string, unknown>): TodaySearch => ({
    sel: text(s.sel),
    filter: oneOf(s.filter, todayFilters),
  }),
  component: OverviewPage,
});

// 新家的歡迎頁（WEB-176）：名冊上還沒有秘書時外殼（Shell）整個畫面只畫它，不經過這裡的 component；
// 這條路由只負責讓網址存在、把步驟（?step=、job=、persona=）收進網址（UXC-3）。有秘書的家開到這裡會被外殼轉回今天。
const welcome = createRoute({
  getParentRoute: () => root,
  path: WELCOME_PATH,
  staticData: { title: "page.welcome" },
  validateSearch: parseWelcomeSearch,
  component: () => null,
});

const chat = createRoute({
  getParentRoute: () => root,
  path: "/chat",
  staticData: { title: "page.chat", agentFilter: true },
  component: ChatPage,
});

const chatThread = createRoute({
  getParentRoute: () => root,
  path: "/chat/$session",
  staticData: { title: "page.thread", hideMobileBar: true },
  validateSearch: homeOf,
  component: ChatPage,
});

const messages = createRoute({
  getParentRoute: () => root,
  path: "/messages",
  staticData: { title: "page.messages", agentFilter: true },
  validateSearch: (s: Record<string, unknown>): MessagesSearch => ({
    show: oneOf(s.show, messageFilters),
    lane: oneOf(s.lane, messageLanes),
  }),
  component: MessagesPage,
});

const messageDetail = createRoute({
  getParentRoute: () => root,
  path: "/messages/$id",
  staticData: { title: "page.message" },
  validateSearch: (s: Record<string, unknown>): MessagesSearch & HomeSearch => ({
    show: oneOf(s.show, messageFilters),
    lane: oneOf(s.lane, messageLanes),
    ...homeOf(s),
  }),
  component: MessageDetailPage,
});

const tasks = createRoute({
  getParentRoute: () => root,
  path: "/tasks",
  staticData: { title: "page.tasks", agentFilter: true },
  // new=true：從「今天」的「交代任務」來，開單的輸入框直接聚焦（REQ-234）。
  // view=closed／status=waiting_user,idea（DEV-107）；舊的 ?view=waiting 轉成「未結」＋「等你」篩選，舊連結不壞。
  validateSearch: (
    s: Record<string, unknown>,
  ): { view?: "closed"; status?: string; new?: boolean } => ({
    ...taskSearchOf(s.view, s.status),
    new: s.new === true || s.new === "true" ? true : undefined,
  }),
  component: TasksPage,
});

// 任務單 → 排程（REQ-18）：?job=<排程 id> 是一個排程的詳情，重新整理與上一頁回到同一個畫面（UXC-3）；
// 秘書篩選跟任務單、待辦同一個。靜態路徑 /tasks/jobs 比 /tasks/$id 優先（TanStack Router 依路徑的明確程度排序），
// 所以 jobs 不會被當成任務單編號（e2e TestJobsRouteNotTaskId 守著）。
const tasksJobs = createRoute({
  getParentRoute: () => root,
  path: "/tasks/jobs",
  staticData: { title: "page.tasks", agentFilter: true },
  validateSearch: (s: Record<string, unknown>): { job?: string } => ({ job: text(s.job) }),
  component: JobsPage,
});

const taskDetail = createRoute({
  getParentRoute: () => root,
  path: "/tasks/$id",
  staticData: { title: "page.task" },
  validateSearch: homeOf,
  component: TaskDetailPage,
});

const reminders = createRoute({
  getParentRoute: () => root,
  path: "/reminders",
  staticData: { title: "page.reminders", agentFilter: true },
  validateSearch: (s: Record<string, unknown>): { view?: (typeof reminderViews)[number] } => ({
    view: oneOf(s.view, reminderViews),
  }),
  component: RemindersPage,
});

const reminderDetail = createRoute({
  getParentRoute: () => root,
  path: "/reminders/$id",
  staticData: { title: "page.reminder" },
  validateSearch: homeOf,
  component: ReminderDetailPage,
});

const policy = createRoute({
  getParentRoute: () => root,
  path: "/policy",
  staticData: { title: "page.policy" },
  validateSearch: (s: Record<string, unknown>): { view?: (typeof policyViewNames)[number] } => ({
    view: oneOf(s.view, policyViewNames),
  }),
  component: PolicyPage,
});

// Goal（REQ-203、D-39）：?view= 是清單的分段，/goals/$id 是詳情。
const goalViewNames = ["active", "draft", "ended"] as const;
const goals = createRoute({
  getParentRoute: () => root,
  path: "/goals",
  // 標題與秘書篩選列跟任務單、排程一樣：三個分頁共用外框，切過去頂端不會跳。
  staticData: { title: "page.tasks", agentFilter: true },
  validateSearch: (s: Record<string, unknown>): { view?: GoalView } => ({
    view: oneOf(s.view, goalViewNames),
  }),
  component: GoalsPage,
});

const goalDetail = createRoute({
  getParentRoute: () => root,
  path: "/goals/$id",
  staticData: { title: "page.goal" },
  validateSearch: homeOf,
  // 旁邊的聊天由外殼交進來（Goal 頁不能 import 聊天頁，FEN-27；WEB-82）。
  component: () => <GoalDetailPage chat={goalChat} />,
});

// 套件頁面（P6-2a）：標題由 Shell 從 web.get_pages 查（staticData 只放固定字串）。
const packagePage = createRoute({
  getParentRoute: () => root,
  path: PACKAGE_ROUTE,
  staticData: { title: "page.package" },
  component: PackagePage,
});

// /settings 本身：手機是設定首頁的清單（常用／秘書／進階，DEV-156 U-12）；桌機有左邊的分頁清單，轉到第一個分頁。
// 寫成 /settings/$ 一條：TanStack 會讓 /settings 比對到星號這條（星號可以是空的），另開一條 /settings 永遠輪不到，
// 再從星號轉回 /settings 就會轉個不停（踩過）。
// 星號不是空的（/settings/打錯的字）：以前被默默轉到「通知」，現在是找不到頁（DEV-156 U-32），跟其他找不到的網址一樣畫在外殼裡。
const settings = createRoute({
  getParentRoute: () => root,
  path: "/settings/$",
  staticData: { title: "page.settings", splatNotFound: true },
  component: function SettingsIndex() {
    const { _splat } = settings.useParams();
    return _splat?.replace(/\/+$/, "") ? <NotFoundPage /> : <SettingsHome />;
  },
});

const settingsPage = (
  path:
    | "notify"
    | "trust"
    | "devices"
    | "phone"
    | "menu"
    | "packages"
    | "machine"
    | "setup"
    | "display"
    | "health"
    | "log",
  title: TextKey,
  component: () => React.JSX.Element,
) =>
  createRoute({
    getParentRoute: () => root,
    path: `/settings/${path}`,
    staticData: { title, mobileTitle: tabLabel[path] },
    component,
  });

// 舊網址：排程原本在「設定 → 排程」（F4），2026-10-26 搬到任務單。舊連結與書籤轉到 /tasks/jobs，保留 ?job=。
const settingsJobs = createRoute({
  getParentRoute: () => root,
  path: "/settings/jobs",
  validateSearch: (s: Record<string, unknown>): { job?: string } => ({ job: text(s.job) }),
  beforeLoad: ({ search }) => {
    throw redirect({ to: "/tasks/jobs", search: { job: search.job }, replace: true });
  },
});

// 職位與秘書：?claim=<職位 id> 把那張認領卡捲到畫面上（認領通知按「換一個」、SEC-302 S9）。
const settingsAgents = createRoute({
  getParentRoute: () => root,
  path: "/settings/agents",
  staticData: { title: "page.settings.agents", mobileTitle: tabLabel.agents },
  validateSearch: (s: Record<string, unknown>): { claim?: string } => ({ claim: text(s.claim) }),
  component: AgentsSettingsPage,
});

// 「角色」頁（P6-5a；DEV-117 從設定搬來，WEB-136；DEV-143 一位角色一頁）：/personas 是角色小卡的清單（?new=1 一進來就打開「新增角色」），
// /personas/<代號> 是一位角色的頁，?tab= 是分頁（沒寫＝介紹；套件掛的分頁是 `<套件>.<頁面>`）。重新整理與上一頁回到同一個畫面（UXC-3）。
// ?item= 是套件分頁裡現在打開的那一項（例：「衣服」分頁打開哪一套）：核心不解讀，原樣交給分頁的頁面；換分頁就拿掉。
// 舊的 ?card=<代號>（通知、書籤）轉到那位的頁（replace，WEB-138）。
const personas = createRoute({
  getParentRoute: () => root,
  path: "/personas",
  staticData: { title: "page.personas" },
  validateSearch: (s: Record<string, unknown>): { card?: string; new?: boolean } => ({
    card: text(s.card),
    new: s.new === true || s.new === "1" || s.new === 1 ? true : undefined,
  }),
  beforeLoad: ({ search }) => {
    if (search.card) {
      throw redirect({ to: "/personas/$slug", params: { slug: search.card }, replace: true });
    }
  },
  component: PersonasPage,
});

const persona = createRoute({
  getParentRoute: () => root,
  path: "/personas/$slug",
  staticData: { title: "page.persona" },
  validateSearch: (s: Record<string, unknown>): { tab?: string; item?: string } => ({
    tab: text(s.tab),
    item: text(s.item),
  }),
  component: PersonaPage,
});

// 舊網址：角色庫原本在「設定 → 角色庫」，2026-11-04 搬到「角色」頁。舊連結與書籤轉到 /personas，帶 ?card= 的轉到那位的頁（WEB-138）。
const settingsPersonas = createRoute({
  getParentRoute: () => root,
  path: "/settings/personas",
  validateSearch: (s: Record<string, unknown>): { card?: string } => ({ card: text(s.card) }),
  beforeLoad: ({ search }) => {
    if (search.card) {
      throw redirect({ to: "/personas/$slug", params: { slug: search.card }, replace: true });
    }
    throw redirect({ to: "/personas", replace: true });
  },
});

// 規矩（S7、D-44）：?doc=<文件 id> 選哪一份、?proposal=RP-<n> 捲到那個提案（提案通知連到這裡）。
const settingsRules = createRoute({
  getParentRoute: () => root,
  path: "/settings/rules",
  staticData: { title: "page.settings.rules", mobileTitle: tabLabel.rules },
  validateSearch: (s: Record<string, unknown>): { doc?: string; proposal?: string } => ({
    doc: text(s.doc),
    proposal: text(s.proposal),
  }),
  component: RulesSettingsPage,
});

const routeTree = root.addChildren([
  overview,
  welcome,
  chat,
  chatThread,
  messages,
  messageDetail,
  tasks,
  tasksJobs,
  taskDetail,
  reminders,
  reminderDetail,
  policy,
  goals,
  goalDetail,
  packagePage,
  personas,
  persona,
  settings,
  settingsPage("notify", "page.settings.notify", NotifySettingsPage),
  settingsPage("trust", "page.settings.trust", TrustSettingsPage),
  settingsPage("devices", "page.settings.devices", DevicesSettingsPage),
  settingsPage("phone", "page.settings.phone", PhoneSettingsPage),
  settingsPage("menu", "page.settings.menu", MenuSettingsPage),
  settingsPage("packages", "page.settings.packages", PackagesSettingsPage),
  settingsAgents,
  settingsJobs,
  settingsPersonas,
  settingsRules,
  settingsPage("machine", "page.settings.machine", MachineSettingsPage),
  settingsPage("setup", "page.settings.setup", SetupSettingsPage),
  settingsPage("display", "page.settings.display", DisplaySettingsPage),
  settingsPage("health", "page.settings.health", HealthSettingsPage),
  settingsPage("log", "page.settings.log", LogSettingsPage),
]);

export const router = createRouter({ routeTree, defaultPreload: false });

// 等著 5 秒後才送的動作（待辦的「做了」「取消」，FEN-21）：換畫面或離開頁面時立刻送出。
installDeferredFlush();
router.subscribe("onBeforeNavigate", (event) => {
  if (event.pathChanged) flushDeferred();
});
