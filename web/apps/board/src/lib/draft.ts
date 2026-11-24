import { useCallback, useState } from "react";

// 草稿存 localStorage（FEN-5 允許的方便類資料）；讀寫都包 try/catch，私密模式或額度滿了就當沒有。
function read(key: string): string {
  try {
    return window.localStorage.getItem(key) ?? "";
  } catch {
    return "";
  }
}

function write(key: string, value: string) {
  try {
    if (value === "") window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
  } catch {
    // 存不了就算了。
  }
}

export { chatDraftKey } from "@evergenie/sdk-web";

/** 先幫主人在輸入框起個頭（例：從通知去聊）；那串已經有草稿就不蓋掉。 */
export function seedDraft(key: string, text: string) {
  if (read(key) === "") write(key, text);
}

/** 一個輸入框的草稿：回來還在，送出成功就 clear（FEN-23）。呼叫端用 key={key} 換串時重掛。 */
export function useDraft(key: string): [string, (v: string) => void, () => void] {
  const [value, setValue] = useState(() => read(key));
  const update = useCallback(
    (v: string) => {
      setValue(v);
      write(key, v);
    },
    [key],
  );
  const clear = useCallback(() => {
    setValue("");
    write(key, "");
  }, [key]);
  return [value, update, clear];
}
