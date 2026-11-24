import { Fragment } from "react";

// 長路徑換行時在「/」處斷（用 <wbr>，不改文字內容、複製貼上也乾淨），
// 不會把資料夾名字從中間切開（everge／nie）。
export function SoftPath({ text }: { text: string }) {
  const parts = text.split("/");
  return (
    <>
      {parts.map((p, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: 固定順序的字串片段
        <Fragment key={i}>
          {i > 0 ? (
            <>
              /<wbr />
            </>
          ) : null}
          {p}
        </Fragment>
      ))}
    </>
  );
}
