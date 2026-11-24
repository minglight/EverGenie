import type { PersonaCard } from "@evergenie/sdk-web";
import { t } from "../../i18n/zh-TW";
import { groupPersonas } from "./personas";

/** 角色下拉的選項，依來源分組：官方、我的、各套件（來自套件的卡，代號是 <套件>.<代號>）。 */
export function PersonaOptions({ cards }: { cards: readonly PersonaCard[] }) {
  return (
    <>
      {groupPersonas(cards).map((g) => (
        <optgroup
          key={g.key}
          label={
            g.kind === "package"
              ? t("settings.personas.group.package", { id: g.pkg })
              : t(`settings.personas.group.${g.kind}`)
          }
        >
          {g.cards.map((c) => (
            <option key={c.slug} value={c.slug}>
              {c.name || c.slug}
            </option>
          ))}
        </optgroup>
      ))}
    </>
  );
}
