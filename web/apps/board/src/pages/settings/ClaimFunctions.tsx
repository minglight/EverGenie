import type { PersonaCard } from "@evergenie/sdk-web";
import { Badge } from "@evergenie/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { packageListQuery, useShowCodes } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import {
  functionTitle,
  suggestPersona,
  type UnclaimedFunction,
  unclaimedFunctions,
} from "./functions";
import { PersonaOptions } from "./PersonaOptions";
import { assignablePersonas } from "./personas";
import { agentSettingsQuery, hireSecretary, invalidateHired, personasQuery } from "./queries";
import { SettingGroup } from "./SettingRow";

const selectClass =
  "min-h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-[15px] text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus";

// 一個還沒人負責的職位：選一個角色，按「請〈名字〉上班」就建好這位秘書（套件樣板的職責）並啟用（REQ-238）。
// 鈕上寫選的那位角色的名字，不寫「她」（男角色也有，DEV-156 U-24）。
function ClaimRow({
  fn,
  personas,
  suggested,
  focused = false,
}: {
  fn: UnclaimedFunction;
  personas: PersonaCard[];
  /** 預設選的角色（優先職位自己帶的，SEC-302 S3）。 */
  suggested: string;
  /** 認領通知按「換一個」開過來的那張：捲到畫面上並標出來（SEC-302 S9）。 */
  focused?: boolean;
}) {
  const client = useQueryClient();
  const showCode = useShowCodes();
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (focused) ref.current?.scrollIntoView?.({ block: "center" });
  }, [focused]);
  const [persona, setPersona] = useState("");
  const [error, setError] = useState("");
  const title = functionTitle(fn.duty, fn.id);
  const chosen = persona || suggested || personas[0]?.slug || "";
  const chosenName = personas.find((c) => c.slug === chosen)?.name || "";
  const claim = useMutation({
    mutationFn: () => hireSecretary(fn, chosen),
    onMutate: () => setError(""),
    onError: (err) => setError(t("settings.agents.failed", { reason: reasonOf(err) })),
    onSettled: () => invalidateHired(client),
  });
  return (
    <div
      ref={ref}
      id={`claim-${fn.id}`}
      data-testid="function-unclaimed"
      data-agent={fn.id}
      data-optional={fn.optional ? "true" : undefined}
      data-focused={focused ? "true" : undefined}
      className={
        focused
          ? "flex flex-col gap-3 rounded-xl bg-attention-soft px-4 py-3.5 outline-2 outline-focus"
          : "flex flex-col gap-3 px-4 py-3.5"
      }
    >
      <div className="min-w-0">
        <h3 className="m-0 flex flex-wrap items-center gap-x-2 gap-y-1 text-[16px] leading-snug font-semibold">
          {title}
          {fn.optional ? (
            <Badge tone="muted" className="font-semibold">
              <span data-testid="function-optional-badge">{t("settings.claim.optionalBadge")}</span>
            </Badge>
          ) : null}
          {showCode ? (
            <span className="font-mono text-xs font-normal text-muted">
              {t("settings.agents.fn", { id: fn.id })}
            </span>
          ) : null}
        </h3>
        {fn.duty && fn.duty !== title ? (
          <p className="mt-0.5 text-[13px] leading-snug text-muted">{fn.duty}</p>
        ) : null}
      </div>
      {personas.length === 0 ? (
        <p className="text-[13px] leading-snug text-muted">{t("settings.claim.noPersona")}</p>
      ) : (
        <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
          <label className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="text-xs font-semibold text-muted">{t("settings.agents.persona")}</span>
            <select
              className={selectClass}
              value={chosen}
              data-testid="claim-persona"
              aria-label={t("settings.agents.persona.aria", { name: title })}
              onChange={(e) => setPersona(e.target.value)}
            >
              <PersonaOptions cards={personas} />
            </select>
          </label>
          <ActionButton
            tone="primary"
            busy={claim.isPending}
            disabled={!chosen}
            onClick={() => claim.mutate()}
          >
            {chosenName
              ? t("settings.claim.start", { name: chosenName })
              : t("settings.claim.start.plain")}
          </ActionButton>
        </div>
      )}
      {error ? (
        <p role="alert" className="text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

// 套件帶來、名冊上還沒有的職位（「職位與秘書」頁與設定精靈共用）。沒有就不顯示。
// 選用職位（DEV-157）不跟著「還沒有人擔任」一起列：withOptional（「職位與秘書」頁）才在下面另一組列出、標「選用」；
// 設定精靈不給 withOptional，新使用者不主動去加就不會看到它。
export function ClaimFunctions({
  title,
  focus,
  withOptional = false,
}: {
  title?: string;
  focus?: string;
  withOptional?: boolean;
}) {
  const packages = useQuery(packageListQuery());
  const agents = useQuery(agentSettingsQuery);
  const personas = useQuery(personasQuery);
  if (packages.data === undefined || agents.data === undefined) return null;
  const taken = new Set((agents.data.agents ?? []).map((a) => a.id ?? ""));
  const all = unclaimedFunctions(packages.data.packages ?? [], taken);
  const list = all.filter((fn) => !fn.optional);
  const optional = withOptional ? all.filter((fn) => fn.optional) : [];
  if (list.length + optional.length === 0) return null;
  // 只列能指派的卡（已上線、沒被官方蓋住、所屬套件有效；D-31），依來源分組；預設選職位自己帶的角色。
  const live = assignablePersonas(personas.data ?? []);
  const rows = (fns: UnclaimedFunction[]) =>
    fns.map((fn) => (
      <ClaimRow
        key={`${fn.pkg}/${fn.id}`}
        fn={fn}
        personas={live}
        suggested={suggestPersona(live, fn.pkg)}
        focused={focus === fn.id}
      />
    ));
  return (
    <>
      {list.length > 0 ? (
        <SettingGroup title={title ?? t("settings.claim.title")}>{rows(list)}</SettingGroup>
      ) : null}
      {optional.length > 0 ? (
        <div data-testid="functions-optional" className="flex flex-col gap-2">
          <SettingGroup title={t("settings.claim.optionalTitle")}>{rows(optional)}</SettingGroup>
          <p className="px-1 text-[13px] leading-snug text-muted">
            {t("settings.claim.optionalHint")}
          </p>
        </div>
      ) : null}
    </>
  );
}
