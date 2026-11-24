import { Button } from "@evergenie/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFor, PRIMARY } from "../data/sites";
import { t } from "../i18n/zh-TW";

// 秘書卡在啟動對話框（第一次進資料夾的「信任這個資料夾」）：什麼都收不到，要主人回答才會動。
// 不管在哪一頁都看得到，答完就消失。讀不到就不畫，不打擾其他畫面。
const bootPromptsKey = ["web.get_boot_prompts"];

export function BootPromptBanner() {
  const client = useQueryClient();
  const { data } = useQuery({
    queryKey: bootPromptsKey,
    queryFn: async () => (await apiFor(PRIMARY).web.getBootPrompts({})).prompts ?? [],
    refetchInterval: 15_000,
    retry: false,
  });
  const answer = useMutation({
    mutationFn: (v: { session: string; id: string; choice: string }) =>
      apiFor(PRIMARY).web.answerBootPrompt(v),
    // 等畫面換掉再重抓：馬上抓可能還看到舊的框，提示又跳回來。
    onSettled: () =>
      new Promise((done) => setTimeout(done, 1_000)).then(() =>
        client.invalidateQueries({ queryKey: bootPromptsKey }),
      ),
  });
  if (!data?.length) return null;
  return (
    <div data-testid="boot-prompts" className="shrink-0">
      {data.map((p) => (
        <div
          key={p.id}
          role="alert"
          className="flex flex-wrap items-center gap-2 border-b border-border bg-attention-soft px-4 py-2 text-[13px]"
        >
          <p className="min-w-0 flex-1">
            {t("boot.title", {
              name: p.agent_name || t("boot.fallbackName"),
              title: p.title ?? "",
            })}
          </p>
          {(p.choices ?? []).map((choice, i) => (
            <Button
              key={choice}
              variant={i === 0 ? "primary" : "secondary"}
              disabled={answer.isPending}
              onClick={() => answer.mutate({ session: p.session ?? "", id: p.id ?? "", choice })}
            >
              {choice}
            </Button>
          ))}
        </div>
      ))}
      {answer.isError ? (
        <p role="status" className="px-4 py-1 text-[13px] text-danger">
          {t("boot.failed", { reason: answer.error.message })}
        </p>
      ) : null}
    </div>
  );
}
