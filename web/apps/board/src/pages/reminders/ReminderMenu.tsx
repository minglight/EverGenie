import { Popover } from "@base-ui/react/popover";
import { ApiError, type Reminder } from "@evergenie/sdk-web";
import { cn, Sheet } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { Ellipsis } from "lucide-react";
import { type ReactNode, useState } from "react";
import { ActionButton, actionButtonClass } from "../../components/ActionButton";
import { AppWhenField } from "../../components/AppWhenField";
import {
  markCancelled,
  markDoing,
  snoozePresets,
  snoozeReminder,
} from "../../data/reminderActions";
import { PRIMARY, type SiteRef } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";

const itemClass =
  "flex min-h-11 w-full items-center rounded-[10px] px-3 text-left text-[15px] hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus";

/** 這個待辦現在能不能「還在做」（伺服器只准待辦或要你回答的）。 */
export function canDoing(r: Reminder): boolean {
  return r.status === "todo" || r.status === "asking";
}

/** 延後的選項與「自己選」：選單和詳細頁的「延後」共用。 */
/** 待辦在哪台（預設主伺服器；S3-7b）：動作都送那台。 */
interface OnSiteProps {
  reminder: Reminder;
  site?: SiteRef;
}

export function SnoozeChoices({
  reminder,
  onClose,
  site = PRIMARY,
}: OnSiteProps & { onClose: () => void }) {
  const client = useQueryClient();
  const [picking, setPicking] = useState(false);
  const [at, setAt] = useState<Date | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const run = async (until: string, when: Date | null) => {
    setBusy(true);
    setError("");
    try {
      await snoozeReminder(client, reminder, until, when, site);
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  if (picking) {
    return (
      <div className="flex flex-col gap-3 px-1 pb-1">
        <AppWhenField
          value={at}
          onChange={setAt}
          error={error}
          autoFocus
          onSubmit={() => at && void run(at.toISOString(), at)}
        />
        <div className="flex gap-2">
          <ActionButton
            size="card"
            tone="primary"
            busy={busy}
            disabled={at === null}
            className="flex-1"
            onClick={() => at && void run(at.toISOString(), at)}
          >
            {t("reminder.snooze.apply")}
          </ActionButton>
          <ActionButton size="card" tone="quiet" onClick={() => setPicking(false)}>
            {t("reminder.snooze.back")}
          </ActionButton>
        </div>
      </div>
    );
  }
  return (
    <div className="flex flex-col">
      {snoozePresets().map((p) => (
        <button
          key={p.key}
          type="button"
          disabled={busy}
          onClick={() => void run(p.until, p.at)}
          className={itemClass}
        >
          {p.label}
        </button>
      ))}
      <button type="button" onClick={() => setPicking(true)} className={itemClass}>
        {t("reminder.snooze.pick")}
      </button>
      {error ? (
        <p role="alert" className="px-3 pb-1 text-[13px] text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

function MenuBody({ reminder, onClose, site = PRIMARY }: OnSiteProps & { onClose: () => void }) {
  const client = useQueryClient();
  const [error, setError] = useState("");
  const heading = (text: string): ReactNode => (
    <div className="px-3 pt-2 pb-1 text-xs font-semibold text-muted">{text}</div>
  );
  return (
    <div className="flex flex-col pb-1">
      {heading(t("reminder.snooze"))}
      <SnoozeChoices reminder={reminder} site={site} onClose={onClose} />
      <div className="my-1 h-px bg-border" role="presentation" />
      {canDoing(reminder) ? (
        <button
          type="button"
          onClick={async () => {
            try {
              await markDoing(client, reminder, site);
              onClose();
            } catch (err) {
              setError(err instanceof ApiError ? err.message : String(err));
            }
          }}
          className={itemClass}
        >
          {t("reminder.doing")}
        </button>
      ) : null}
      <button
        type="button"
        onClick={() => {
          onClose();
          markCancelled(client, reminder, site);
        }}
        className={cn(itemClass, "text-danger")}
      >
        {t("reminder.cancel")}
      </button>
      {error ? (
        <p role="alert" className="px-3 pb-1 text-[13px] text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

/** 清單每一列右邊的「⋯」：延後、還在做、取消。桌機是小浮窗，手機是底部抽屜（FEN-24）。 */
export function ReminderMenu({ reminder, site = PRIMARY }: OnSiteProps) {
  const desktop = useIsDesktop();
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  const label = t("reminder.menu");

  if (!desktop) {
    return (
      <>
        <button
          type="button"
          aria-label={label}
          aria-haspopup="dialog"
          onClick={() => setOpen(true)}
          className="flex size-11 shrink-0 items-center justify-center rounded-full text-muted focus-visible:outline-2 focus-visible:outline-focus"
        >
          <Ellipsis className="size-5" aria-hidden="true" />
        </button>
        <Sheet open={open} onOpenChange={setOpen} title={reminder.content || reminder.id || label}>
          <div className="px-3 pb-2">
            <MenuBody reminder={reminder} site={site} onClose={close} />
          </div>
        </Sheet>
      </>
    );
  }
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        aria-label={label}
        className="flex size-11 shrink-0 items-center justify-center rounded-full text-muted hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
      >
        <Ellipsis className="size-5" aria-hidden="true" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner align="end" sideOffset={4} className="z-50">
          <Popover.Popup
            aria-label={label}
            className="hg-pop w-[280px] rounded-2xl border border-border bg-surface p-1.5 shadow-[var(--shadow-pop)] outline-none"
          >
            <MenuBody reminder={reminder} site={site} onClose={close} />
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}

/** 詳細頁動作列的「延後」：桌機小浮窗、手機底部抽屜，內容跟清單的「⋯」一樣。 */
export function SnoozeButton({ reminder, site = PRIMARY }: OnSiteProps) {
  const desktop = useIsDesktop();
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  const label = t("reminder.snooze");
  if (!desktop) {
    return (
      <>
        <ActionButton size="page" tone="secondary" onClick={() => setOpen(true)}>
          {label}
        </ActionButton>
        <Sheet open={open} onOpenChange={setOpen} title={label}>
          <div className="px-3 pb-2">
            <SnoozeChoices reminder={reminder} site={site} onClose={close} />
          </div>
        </Sheet>
      </>
    );
  }
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger className={actionButtonClass("secondary", "page")}>{label}</Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner align="start" sideOffset={4} className="z-50">
          <Popover.Popup
            aria-label={label}
            className="hg-pop w-[280px] rounded-2xl border border-border bg-surface p-1.5 shadow-[var(--shadow-pop)] outline-none"
          >
            <SnoozeChoices reminder={reminder} site={site} onClose={close} />
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
