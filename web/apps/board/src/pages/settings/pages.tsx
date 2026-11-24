import { useSearch } from "@tanstack/react-router";
import { CoreSectionTabs } from "../../components/SectionTabs";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import { AgentsSettings } from "./AgentsSettings";
import { DevicesSettings } from "./DevicesSettings";
import { DisplaySettings } from "./DisplaySettings";
import { HealthSettings } from "./HealthSettings";
import { LogSettings } from "./LogSettings";
import { MachineSettings } from "./MachineSettings";
import { MenuSettings } from "./MenuSettings";
import { NotifySettings } from "./NotifySettings";
import { PackagesSettings } from "./PackagesSettings";

export { PersonaPage } from "./PersonaPage";

import { PersonasSettings } from "./PersonasSettings";
import { PhoneSettings } from "./PhoneSettings";
import { RulesPage } from "./rules/RulesPage";
import { SaveStatus } from "./SaveStatus";
import { SettingsLayout } from "./SettingsLayout";
import { SetupSettings } from "./SetupSettings";
import { TrustSettings } from "./TrustSettings";
import { useAutosavedPrefs } from "./useAutosavedPrefs";
import { WorkMaxSetting } from "./WorkMax";

// 設定的十三個分頁（網址 /settings/notify|display|trust|devices|phone|menu|packages|agents|rules|machine|setup|health|log，REQ-107、REQ-111）。
// 排程不在設定：2026-10-26 搬到任務單（/tasks/jobs）；角色庫 2026-11-04 搬到「角色」頁（/personas，WEB-136）；舊網址只剩轉址（router.tsx）。
export function NotifySettingsPage() {
  const auto = useAutosavedPrefs();
  return (
    <SettingsLayout
      tab="notify"
      intro={t("settings.notify.intro")}
      status={<SaveStatus state={auto.state} onRetry={auto.retry} />}
    >
      <NotifySettings auto={auto} />
    </SettingsLayout>
  );
}

// 規矩（S7、D-44）：家裡的憲法、宗旨、法律；?doc= 選文件、?proposal= 捲到那個提案（提案通知的連結）。
export function RulesSettingsPage() {
  return (
    <SettingsLayout tab="rules" intro={t("settings.rules.intro")}>
      <RulesPage />
    </SettingsLayout>
  );
}

export function TrustSettingsPage() {
  return (
    <SettingsLayout tab="trust" intro={t("settings.trust.intro")}>
      <TrustSettings />
    </SettingsLayout>
  );
}

export function DevicesSettingsPage() {
  return (
    <SettingsLayout tab="devices" intro={t("settings.devices.intro")}>
      <DevicesSettings />
    </SettingsLayout>
  );
}

// 手機連線（DEV-153）：四步把看板接到手機。
export function PhoneSettingsPage() {
  return (
    <SettingsLayout tab="phone" intro={t("settings.phone.intro")}>
      <PhoneSettings />
    </SettingsLayout>
  );
}

export function HealthSettingsPage() {
  return (
    <SettingsLayout tab="health" intro={t("settings.health.intro")}>
      <HealthSettings />
    </SettingsLayout>
  );
}

export function MenuSettingsPage() {
  return (
    <SettingsLayout tab="menu" intro={t("settings.menu.intro")}>
      <MenuSettings />
    </SettingsLayout>
  );
}

export function PackagesSettingsPage() {
  return (
    <SettingsLayout tab="packages" intro={t("settings.packages.intro")}>
      <PackagesSettings />
    </SettingsLayout>
  );
}

export function AgentsSettingsPage() {
  // ?claim=<職位 id>：認領通知按「換一個」開過來，把那張認領卡捲到畫面上（SEC-302 S9）。
  const { claim } = useSearch({ strict: false }) as { claim?: string };
  return (
    <SettingsLayout tab="agents" intro={t("settings.agents.intro")}>
      <AgentsSettings focusClaim={claim} />
      <WorkMaxSetting />
    </SettingsLayout>
  );
}

// 「角色」頁的第一個分頁「介紹」（/personas，DEV-117、WEB-136）：核心頁，同 section 的套件頁（衣櫃、展示間）是後面的分頁。
// 程式留在設定資料夾：角色卡的純函式、查詢跟「職位與秘書」共用（FEN-27，頁面資料夾之間不互相 import）。
// 外框跟套件頁（pkg/PackagePage）一樣寬、標題與分頁在同一個位置，換分頁時頂端不跳。
export function PersonasPage() {
  const desktop = useIsDesktop();
  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 px-4 pt-3 pb-7">
      {desktop ? (
        <h1 className="pt-4 text-2xl font-bold tracking-[-0.01em]">{t("page.personas")}</h1>
      ) : null}
      <CoreSectionTabs route={PERSONAS_ROUTE} />
      <p className="px-1 text-[13px] leading-snug text-muted">{t("settings.personas.intro")}</p>
      <PersonasSettings />
    </div>
  );
}

/** 核心「介紹」的網址（跟 internal/feature/inbox/menu 的 core.personas 一樣）。 */
export const PERSONAS_ROUTE = "/personas";

export function MachineSettingsPage() {
  return (
    <SettingsLayout tab="machine" intro={t("settings.machine.introSites")}>
      <MachineSettings />
    </SettingsLayout>
  );
}

export function DisplaySettingsPage() {
  return (
    <SettingsLayout tab="display" intro={t("settings.display.intro")}>
      <DisplaySettings />
    </SettingsLayout>
  );
}

export function SetupSettingsPage() {
  return (
    <SettingsLayout tab="setup" intro={t("settings.setup.intro")}>
      <SetupSettings />
    </SettingsLayout>
  );
}

export function LogSettingsPage() {
  return (
    <SettingsLayout tab="log" intro={t("settings.log.intro")}>
      <LogSettings />
    </SettingsLayout>
  );
}
