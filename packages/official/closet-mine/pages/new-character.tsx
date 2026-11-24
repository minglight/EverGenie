import { NewCharacter } from "./lib/forms";
import { useSender } from "./lib/parts";

// 角色清單「新增角色」裡的「上傳半身照」（DEV-143）：掛在核心的 slot core.personas.new，標題由 package.toml 給。
// 核心給 `onDone(代號)`（建好了，開到她的頁）與 `onCancel()`；這裡只上傳半身照、寫一筆 closet-mine.request（type = character），
// 等套件的程式建好角色卡與第一套衣服（CMI-4），result 是 `<代號>-base`。
export default function NewCharacterPage({
  onDone,
  onCancel,
}: {
  onDone?: (persona?: string) => void;
  onCancel?: () => void;
}) {
  const sender = useSender(() => {});
  return (
    <NewCharacter
      sender={sender}
      onDone={(persona) => onDone?.(persona || undefined)}
      onCancel={onCancel}
    />
  );
}
