import type { Package, PersonaCard } from "@evergenie/sdk-web";
import type { TextKey } from "../i18n/zh-TW";
import {
  functionTitle,
  suggestPersona,
  type UnclaimedFunction,
  unclaimedFunctions,
} from "../pages/settings/functions";
import { assignablePersonas } from "../pages/settings/personas";

// 新家第一次打開的歡迎頁（WEB-176，DEV-156 U-03）的純邏輯：什麼時候出現、步驟怎麼寫在網址、列哪些職位與角色。
// 畫面在 Welcome.tsx，由外殼（Shell.tsx）決定畫它還是畫看板。

/** 歡迎頁的網址。第一段 `home` 本來就是核心保留的（pkg.CoreRouteSegments），不會撞到套件頁。 */
export const WELCOME_PATH = "/home/welcome";

export function isWelcomePath(pathname: string): boolean {
  return pathname.replace(/\/+$/, "") === WELCOME_PATH;
}

/**
 * 外殼要畫什麼：
 * - `board`：照常畫看板（名冊上有人，或名冊還沒讀到——讀不到不能把主人擋在外面）。
 * - `welcome`：名冊是空的，而且網址就是歡迎頁 → 全螢幕歡迎頁。
 * - `toWelcome`：名冊是空的，網址是別頁 → 轉到歡迎頁。
 * - `toToday`：名冊上有人了卻停在歡迎頁（剛請完、或舊書籤）→ 轉到今天。
 * - `wait`：停在歡迎頁、名冊還沒讀到 → 先畫載入畫面，不閃一下看板。
 * 名冊＝web.get_agents 的全部秘書（含停用）：升級上來的舊家就算把秘書都停用了，也不會被當成新家。
 * skipped＝主人在歡迎頁走不下去時按了「先進看板」（只記在這個分頁）。
 */
export type WelcomeGate = "board" | "welcome" | "toWelcome" | "toToday" | "wait";

export function welcomeGate(
  agentCount: number | undefined,
  pathname: string,
  skipped: boolean,
): WelcomeGate {
  const here = isWelcomePath(pathname);
  if (agentCount === undefined) return here ? "wait" : "board";
  if (agentCount > 0) return here ? "toToday" : "board";
  if (skipped) return here ? "toToday" : "board";
  return here ? "welcome" : "toWelcome";
}

const SKIP_KEY = "eg.welcome.skip.v1";

export function welcomeSkipped(): boolean {
  try {
    return sessionStorage.getItem(SKIP_KEY) === "1";
  } catch {
    return false;
  }
}

export function skipWelcome() {
  try {
    sessionStorage.setItem(SKIP_KEY, "1");
  } catch {
    // 存不了（無痕模式）就算了：下一次換頁會再回到歡迎頁。
  }
}

// ---- 步驟（寫在網址，UXC-3）----

export const welcomeSteps = ["job", "persona", "start"] as const;
export type WelcomeStep = "intro" | (typeof welcomeSteps)[number];

export interface WelcomeSearch {
  step?: (typeof welcomeSteps)[number];
  job?: string;
  persona?: string;
}

const text = (v: unknown): string | undefined =>
  typeof v === "string" && v !== "" ? v : undefined;

export function parseWelcomeSearch(s: Record<string, unknown>): WelcomeSearch {
  const step = welcomeSteps.find((x) => x === s.step);
  return { step, job: text(s.job), persona: text(s.persona) };
}

/** 歡迎頁先問的三個職位（照這個順序）；寫程式、電腦之後在設定加。 */
export const WELCOME_JOBS = ["life", "health", "work"] as const;

const jobText: Record<string, { title: TextKey; hint: TextKey }> = {
  life: { title: "welcome.job.life", hint: "welcome.job.life.hint" },
  health: { title: "welcome.job.health", hint: "welcome.job.health.hint" },
  work: { title: "welcome.job.work", hint: "welcome.job.work.hint" },
};

export interface WelcomeJob {
  fn: UnclaimedFunction;
  /** 卡片標題與一句說明：三個預設職位用歡迎頁自己的白話，其他的（退路）用樣板的職責。 */
  title: string;
  hint: string;
}

/**
 * 這個家可以先請的職位：生活／健康／工作裡這個家有裝的（沒裝的那張不出現）。
 * 三個都沒有（家裡只有自己的套件）就退一步列所有還沒人擔任的一般職位，免得歡迎頁走不下去；選用職位一律不列。
 * 畫面上只有名字與說明，不露職位代號（UXC-2）。
 */
export function welcomeJobs(
  packages: readonly Package[],
  t: (key: TextKey) => string,
): WelcomeJob[] {
  const all = unclaimedFunctions(packages, new Set()).filter((fn) => !fn.optional);
  const preset = WELCOME_JOBS.flatMap((id) => {
    const fn = all.find((f) => f.id === id);
    const keys = jobText[id];
    return fn && keys ? [{ fn, title: t(keys.title), hint: t(keys.hint) }] : [];
  });
  if (preset.length > 0) return preset;
  return all.map((fn) => {
    const title = functionTitle(fn.duty, fn.id);
    return { fn, title, hint: fn.duty && fn.duty !== title ? fn.duty : "" };
  });
}

/** 可以選的角色：能指派的公開角色；這個職位自己帶的（或還沒擔任職位的）排第一。 */
export function welcomePersonas(cards: readonly PersonaCard[], job: WelcomeJob): PersonaCard[] {
  const live = assignablePersonas(cards);
  const first = suggestPersona(live, job.fn.pkg);
  return [...live.filter((c) => c.slug === first), ...live.filter((c) => c.slug !== first)];
}

export interface WelcomeView {
  step: WelcomeStep;
  job?: WelcomeJob;
  persona?: PersonaCard;
}

/**
 * 網址 → 現在該畫哪一步。網址寫的職位或角色不存在（手改網址、舊書籤）就退回還站得住的那一步，不畫壞掉的畫面。
 */
export function welcomeView(
  search: WelcomeSearch,
  jobs: readonly WelcomeJob[],
  cards: readonly PersonaCard[],
): WelcomeView {
  if (!search.step) return { step: "intro" };
  const job = jobs.find((j) => j.fn.id === search.job);
  if (search.step === "job" || !job) return { step: "job" };
  const persona = welcomePersonas(cards, job).find((c) => c.slug === search.persona);
  if (search.step === "persona" || !persona) return { step: "persona", job };
  return { step: "start", job, persona };
}

/** 每一步的上一步（「上一步」按鈕；瀏覽器的上一頁走歷史，結果一樣）。 */
export function welcomeBack(view: WelcomeView): WelcomeSearch | null {
  if (view.step === "job") return {};
  if (view.step === "persona") return { step: "job" };
  if (view.step === "start") return { step: "persona", job: view.job?.fn.id };
  return null;
}

/** 第幾步（1 起算，共 4 步）。 */
export function welcomeStepNumber(step: WelcomeStep): number {
  return step === "intro" ? 1 : welcomeSteps.indexOf(step) + 2;
}
