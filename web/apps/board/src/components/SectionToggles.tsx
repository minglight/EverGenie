import { api, type Package } from "@evergenie/sdk-web";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { SlidersHorizontal } from "lucide-react";
import { useId, useState } from "react";
import { invalidatePackageViews, packageListQuery } from "../data/queries";
import { t } from "../i18n/zh-TW";
import { reasonOf } from "../lib/reason";
import { packagePageRoute } from "../pkg/routes";
import { Toggle } from "./Toggle";

/** 選單項目宣告在這個 section 的套件（含停用的，才開得回來），依選單順序（REQ-236）。 */
export function sectionPackages(all: readonly Package[], section: string): Package[] {
  const order = (p: Package) =>
    Math.min(...(p.menu ?? []).filter((m) => m.section === section).map((m) => m.order));
  return all
    .filter((p) => (p.menu ?? []).some((m) => m.section === section))
    .sort((a, b) => order(a) - order(b));
}

/**
 * 開關之後要不要換頁：關掉的是正在看的那個套件，就換到還開著的第一個套件分頁；沒有就回同 section 的核心頁（`home`，WEB-137），
 * 再沒有回今天。其他情況（打開、關掉的不是正在看的、在核心頁上）不換頁，回 undefined。
 */
export function afterToggle(
  pkgs: readonly Package[],
  x: { pkg: string; on: boolean; current: string; section: string; home?: string },
): string | undefined {
  if (x.on || !x.current || x.pkg !== x.current) return undefined;
  const next = pkgs.find((p) => p.id !== x.pkg && p.enabled);
  const page = next?.menu?.find((m) => m.section === x.section)?.page;
  return next?.id && page ? packagePageRoute(next.id, page) : x.home || "/";
}

// 「健康」這類合併頁上的「項目」開關（REQ-236）：每個分頁背後是一個套件，關掉就是停用那個套件
// （分頁不見、秘書不記，資料留著）。關掉的是正在看的那頁，就換到還開著的第一頁；沒有就回同 section 的核心頁（`home`，WEB-137），再沒有回今天。
// 只列套件：核心頁（例「介紹」）也是分頁，但不能關。
export function SectionToggles({
  section,
  title,
  current,
  home,
}: {
  section: string;
  title: string;
  /** 正在看的套件 id；在核心頁上是空字串。 */
  current: string;
  /** 同 section 的核心頁網址（有的話）。 */
  home?: string;
}) {
  const [open, setOpen] = useState(false);
  const panelId = useId();
  return (
    <div className="relative">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((v) => !v)}
        data-testid="section-toggles"
        className="flex min-h-11 items-center gap-1.5 rounded-lg px-2.5 text-[13px] font-semibold text-muted hover:bg-hover hover:text-fg focus-visible:outline-2 focus-visible:outline-focus"
      >
        <SlidersHorizontal className="size-4" strokeWidth={1.8} aria-hidden="true" />
        {t("sectionToggles.button")}
      </button>
      {open ? (
        <Panel
          id={panelId}
          section={section}
          title={title}
          current={current}
          home={home}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </div>
  );
}

function Panel({
  id,
  section,
  title,
  current,
  home,
  onClose,
}: {
  id: string;
  section: string;
  title: string;
  current: string;
  home?: string;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const list = useQuery(packageListQuery());
  const [error, setError] = useState("");
  const pkgs = sectionPackages(list.data?.packages ?? [], section);
  const toggle = useMutation({
    mutationFn: ({ pkg, on }: { pkg: string; on: boolean }) =>
      api.package.setEnabled({ id: pkg, enabled: on }),
    onMutate: () => setError(""),
    onError: (err) => setError(t("sectionToggles.failed", { reason: reasonOf(err) })),
    onSuccess: (_res, { pkg, on }) => {
      const to = afterToggle(pkgs, { pkg, on, current, section, home });
      if (to) void navigate({ to: to as "/", replace: true });
    },
    onSettled: () => invalidatePackageViews(client),
  });
  return (
    <div
      id={id}
      data-testid="section-toggles-panel"
      className="absolute top-full right-0 z-20 mt-1 flex w-[min(320px,calc(100vw-32px))] flex-col gap-2 rounded-[18px] border border-border bg-surface p-4 shadow-[var(--shadow-card)]"
    >
      <p className="text-sm font-semibold">{t("sectionToggles.title", { title })}</p>
      <ul className="m-0 flex list-none flex-col p-0">
        {pkgs.map((p) => {
          const name = p.menu?.find((m) => m.section === section)?.title || p.name || p.id || "";
          const checked =
            toggle.isPending && toggle.variables?.pkg === p.id
              ? toggle.variables.on
              : Boolean(p.enabled);
          return (
            <li key={p.id} className="flex items-center justify-between gap-3">
              <span className="text-[15px]">{name}</span>
              <Toggle
                checked={checked}
                disabled={toggle.isPending}
                label={t("sectionToggles.toggle", { name })}
                onChange={(on) => toggle.mutate({ pkg: p.id ?? "", on })}
              />
            </li>
          );
        })}
      </ul>
      <p className="text-xs leading-snug text-muted">{t("sectionToggles.hint")}</p>
      {error ? (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      ) : null}
      <button
        type="button"
        onClick={onClose}
        className="min-h-10 self-end rounded-lg px-3 text-[13px] font-semibold text-muted hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
      >
        {t("sectionToggles.close")}
      </button>
    </div>
  );
}
