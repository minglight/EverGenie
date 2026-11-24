import type { Goal, GoalState } from "@evergenie/sdk-web";
import type { PillTone } from "../../components/Pill";
import type { TextKey } from "../../i18n/zh-TW";

export const stateKey: Record<GoalState, TextKey> = {
  active: "goal.state.active",
  draft: "goal.state.draft",
  done: "goal.state.done",
  withdrawn: "goal.state.withdrawn",
};

export const stateTone: Record<GoalState, PillTone> = {
  active: "accent",
  draft: "attention",
  done: "ok",
  withdrawn: "muted",
};

/** 是不是主人自己開的（看板的 by 是 owner(web)）。 */
export function openedByOwner(g: Goal): boolean {
  return (g.by ?? "").startsWith("owner(");
}
