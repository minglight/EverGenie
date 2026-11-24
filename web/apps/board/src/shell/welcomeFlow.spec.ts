import type { Package, PersonaCard } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import { t } from "../i18n/zh-TW";
import {
  isWelcomePath,
  parseWelcomeSearch,
  WELCOME_PATH,
  welcomeBack,
  welcomeGate,
  welcomeJobs,
  welcomePersonas,
  welcomeStepNumber,
  welcomeView,
} from "./welcomeFlow";

const pkg = (id: string, agents: Package["agents"]): Package =>
  ({ id, enabled: true, state: "running", agents }) as Package;

const official: Package[] = [
  pkg("health", [{ id: "health", duty: "健康：飲食、運動、身體一起照顧" }]),
  pkg("secretaries", [
    { id: "code", duty: "寫程式：讀 code、設計" },
    { id: "life", duty: "生活：家庭、提醒、陪聊" },
    { id: "system", duty: "電腦：電腦環境" },
    { id: "work", duty: "工作：行程、會議" },
  ]),
  pkg("admin", [{ id: "art", duty: "生圖：做新衣服", optional: true }]), // 開發家才開著的開發工坊帶選用的生圖
];

const card = (slug: string, more: Partial<PersonaCard> = {}): PersonaCard => ({
  slug,
  name: slug,
  source: "official",
  status: "live",
  seats: [],
  shadowed: false,
  updated_at: "",
  ...more,
});

// Guards: WEB-176
describe("歡迎頁什麼時候出現", () => {
  test("名冊空的：任何一頁都轉到歡迎頁，已經在歡迎頁就畫它", () => {
    expect(welcomeGate(0, "/", false)).toBe("toWelcome");
    expect(welcomeGate(0, "/chat", false)).toBe("toWelcome");
    expect(welcomeGate(0, "/settings/setup", false)).toBe("toWelcome");
    expect(welcomeGate(0, WELCOME_PATH, false)).toBe("welcome");
    expect(welcomeGate(0, `${WELCOME_PATH}/`, false)).toBe("welcome");
  });

  test("名冊上有人（停用的也算）：絕不出現，停在歡迎頁的轉回今天", () => {
    expect(welcomeGate(1, "/", false)).toBe("board");
    expect(welcomeGate(3, "/settings/agents", false)).toBe("board");
    expect(welcomeGate(1, WELCOME_PATH, false)).toBe("toToday");
  });

  test("名冊還沒讀到：別頁照常畫看板（不擋人），歡迎頁先等", () => {
    expect(welcomeGate(undefined, "/", false)).toBe("board");
    expect(welcomeGate(undefined, WELCOME_PATH, false)).toBe("wait");
  });

  test("走不下去按了「先進看板」：這個分頁不再轉回歡迎頁", () => {
    expect(welcomeGate(0, "/settings/setup", true)).toBe("board");
    expect(welcomeGate(0, WELCOME_PATH, true)).toBe("toToday");
  });

  test("只有 /home/welcome 是歡迎頁", () => {
    expect(isWelcomePath("/home/welcome")).toBe(true);
    expect(isWelcomePath("/home")).toBe(false);
    expect(isWelcomePath("/home/welcome/x")).toBe(false);
  });
});

// Guards: WEB-176
describe("歡迎頁列哪些職位與角色", () => {
  test("只列生活、健康、工作（照這個順序），寫程式、電腦與選用職位不出現；文字不露職位代號", () => {
    const jobs = welcomeJobs(official, t);
    expect(jobs.map((j) => j.fn.id)).toEqual(["life", "health", "work"]);
    expect(jobs.map((j) => j.title)).toEqual(["生活", "健康", "工作"]);
    for (const j of jobs) {
      expect(j.hint).not.toBe("");
      expect(`${j.title}${j.hint}`).not.toMatch(/[a-z]/);
    }
    expect(jobs[1]?.fn.pkg).toBe("health");
  });

  test("這個家沒裝的那張不出現；停用的套件不算", () => {
    const jobs = welcomeJobs([official[1] as Package, { ...official[0], enabled: false }], t);
    expect(jobs.map((j) => j.fn.id)).toEqual(["life", "work"]);
  });

  test("三個都沒有：退一步列這個家還沒人擔任的一般職位（用樣板的名字與職責），選用的仍不列", () => {
    const jobs = welcomeJobs(
      [
        pkg("garden", [
          { id: "garden", duty: "花園：澆水、施肥" },
          { id: "painter", duty: "畫畫", optional: true },
        ]),
      ],
      t,
    );
    expect(jobs.map((j) => [j.fn.id, j.title, j.hint])).toEqual([
      ["garden", "花園", "花園：澆水、施肥"],
    ]);
    expect(welcomeJobs([], t)).toEqual([]);
  });

  test("角色只列能指派的；職位自己帶的角色排第一", () => {
    const jobs = welcomeJobs(official, t);
    const health = jobs.find((j) => j.fn.id === "health");
    if (!health) throw new Error("沒有健康");
    const cards = [
      card("athena"),
      card("draft", { status: "design" }),
      card("mine", { shadowed: true }),
      card("off", { unassignable: "套件停用" }),
      card("health.coach", { source: "package", package: "health" }),
    ];
    expect(welcomePersonas(cards, health).map((c) => c.slug)).toEqual(["health.coach", "athena"]);
  });
});

// Guards: WEB-176, UXC-3
describe("步驟寫在網址", () => {
  const jobs = welcomeJobs(official, t);
  const cards = [card("athena"), card("venus")];

  test("網址 → 步驟；每一步的上一步", () => {
    expect(parseWelcomeSearch({})).toEqual({ step: undefined, job: undefined, persona: undefined });
    expect(parseWelcomeSearch({ step: "nope", job: 3 }).step).toBeUndefined();
    const intro = welcomeView(parseWelcomeSearch({}), jobs, cards);
    expect(intro.step).toBe("intro");
    expect(welcomeBack(intro)).toBeNull();

    const job = welcomeView(parseWelcomeSearch({ step: "job" }), jobs, cards);
    expect(job.step).toBe("job");
    expect(welcomeBack(job)).toEqual({});

    const persona = welcomeView(
      parseWelcomeSearch({ step: "persona", job: "health" }),
      jobs,
      cards,
    );
    expect([persona.step, persona.job?.fn.id]).toEqual(["persona", "health"]);
    expect(welcomeBack(persona)).toEqual({ step: "job" });

    const start = welcomeView(
      parseWelcomeSearch({ step: "start", job: "health", persona: "venus" }),
      jobs,
      cards,
    );
    expect([start.step, start.job?.fn.id, start.persona?.slug]).toEqual([
      "start",
      "health",
      "venus",
    ]);
    expect(welcomeBack(start)).toEqual({ step: "persona", job: "health" });
    expect([intro, job, persona, start].map((v) => welcomeStepNumber(v.step))).toEqual([
      1, 2, 3, 4,
    ]);
  });

  test("網址寫的職位或角色不存在：退回還站得住的那一步", () => {
    expect(welcomeView({ step: "persona", job: "code" }, jobs, cards).step).toBe("job");
    expect(welcomeView({ step: "persona" }, jobs, cards).step).toBe("job");
    const noPersona = welcomeView({ step: "start", job: "life", persona: "ghost" }, jobs, cards);
    expect([noPersona.step, noPersona.job?.fn.id]).toEqual(["persona", "life"]);
    expect(welcomeView({ step: "start", job: "life" }, jobs, cards).step).toBe("persona");
  });
});
