import { Download } from "lucide-react";
import { actionButtonClass } from "../../components/ActionButton";
import { type TextKey, t } from "../../i18n/zh-TW";
import { SettingGroup } from "./SettingRow";

// iPhone 與 Apple Watch 語音傳話（WEB-170、CHT-9）：下載這個家的「跟秘書說」捷徑（核心 /voice-shortcut 現場簽名），
// 安裝到 iPhone、放到手錶的步驟，和作者 2026-11-08 實際遇到的問題。

/** 核心送捷徑檔的網址（internal/base/app/board/voiceshortcut.go）。 */
export const VOICE_SHORTCUT_URL = "/voice-shortcut";

const iphoneSteps: TextKey[] = [
  "settings.voice.iphone.step1",
  "settings.voice.iphone.step2",
  "settings.voice.iphone.step3",
  "settings.voice.iphone.step4",
];

const watchSteps: TextKey[] = [
  "settings.voice.watch.step1",
  "settings.voice.watch.step2",
  "settings.voice.watch.step3",
  "settings.voice.watch.step4",
];

const faqs: [TextKey, TextKey][] = [
  ["settings.voice.faq.siri.q", "settings.voice.faq.siri.a"],
  ["settings.voice.faq.english.q", "settings.voice.faq.english.a"],
  ["settings.voice.faq.music.q", "settings.voice.faq.music.a"],
  ["settings.voice.faq.name.q", "settings.voice.faq.name.a"],
  ["settings.voice.faq.offline.q", "settings.voice.faq.offline.a"],
  ["settings.voice.faq.who.q", "settings.voice.faq.who.a"],
  ["settings.voice.faq.ask.q", "settings.voice.faq.ask.a"],
  ["settings.voice.faq.typo.q", "settings.voice.faq.typo.a"],
  ["settings.voice.faq.homes.q", "settings.voice.faq.homes.a"],
];

function Steps({ title, steps }: { title: TextKey; steps: TextKey[] }) {
  return (
    <SettingGroup title={t(title)}>
      <ol className="flex flex-col gap-3 px-4 py-3.5">
        {steps.map((key, i) => (
          <li key={key} className="flex items-start gap-3">
            <span
              aria-hidden="true"
              className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[13px] font-bold text-accent"
            >
              {i + 1}
            </span>
            <span className="pt-px text-[14px] leading-snug">{t(key)}</span>
          </li>
        ))}
      </ol>
    </SettingGroup>
  );
}

export function VoiceShortcut() {
  return (
    <section className="flex flex-col gap-5" data-testid="voice-shortcut">
      <SettingGroup title={t("settings.voice.title")}>
        <div className="flex flex-col gap-3 px-4 py-3.5">
          <p className="text-[14px] leading-snug">{t("settings.voice.intro")}</p>
          <div>
            <a
              href={VOICE_SHORTCUT_URL}
              data-testid="voice-shortcut-download"
              className={actionButtonClass("primary", "page")}
            >
              <Download aria-hidden="true" className="size-4" />
              {t("settings.voice.download")}
            </a>
          </div>
          <p className="text-[13px] leading-snug text-muted">{t("settings.voice.download.hint")}</p>
        </div>
      </SettingGroup>
      <Steps title="settings.voice.iphone.title" steps={iphoneSteps} />
      <Steps title="settings.voice.watch.title" steps={watchSteps} />
      <SettingGroup title={t("settings.voice.faq.title")}>
        <div className="flex flex-col px-4 py-1.5">
          {faqs.map(([q, a]) => (
            <details key={q} className="border-b border-border py-1 last:border-b-0">
              <summary className="flex min-h-11 cursor-pointer items-center text-[14px] font-semibold">
                {t(q)}
              </summary>
              <p className="pb-3 text-[14px] leading-snug text-muted">{t(a)}</p>
            </details>
          ))}
        </div>
      </SettingGroup>
    </section>
  );
}
