import { useQuery } from "@tanstack/react-query";
import { chatDisplayQuery } from "../data/queries";

/** 對話要不要露工具細節：只有工程師模式才要；還沒讀到先不露，不讓工具字眼閃一下（WEB-160）。 */
export function useToolDetails(): boolean {
  const { data } = useQuery(chatDisplayQuery());
  return data === "engineer";
}
