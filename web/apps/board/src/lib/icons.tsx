import {
  AlarmClock,
  Bell,
  Briefcase,
  CalendarCheck,
  CalendarDays,
  Circle,
  ClipboardList,
  ContactRound,
  Dumbbell,
  Folder,
  House,
  ImagePlus,
  type LucideIcon,
  MessageCircle,
  Puzzle,
  Scale,
  Settings,
  ShieldCheck,
  Shirt,
  Sun,
  Target,
  Utensils,
} from "lucide-react";

// 選單項目的圖示名稱（伺服器給 lucide 的名稱）對照；不認得就用 circle。
const icons: Record<string, LucideIcon> = {
  house: House,
  "message-circle": MessageCircle,
  bell: Bell,
  "clipboard-list": ClipboardList,
  "alarm-clock": AlarmClock,
  settings: Settings,
  "shield-check": ShieldCheck,
  sun: Sun,
  briefcase: Briefcase,
  folder: Folder,
  calendar: CalendarDays,
  "calendar-check": CalendarCheck,
  circle: Circle,
  puzzle: Puzzle,
  utensils: Utensils,
  dumbbell: Dumbbell,
  scale: Scale,
  target: Target,
  shirt: Shirt,
  "image-plus": ImagePlus,
  "contact-round": ContactRound,
};

/** 選單編輯器讓人挑的圖示名稱（就是上面對照表有的那些）。 */
export const iconNames = Object.keys(icons);

export function iconFor(name: string | undefined): LucideIcon {
  return (name && icons[name]) || Circle;
}
