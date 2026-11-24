/** 清單項目沒有編號可以當 key 時，用內容當 key，內容一樣的加上第幾個（不用陣列位置，項目增減時不會讓畫面狀態錯位）。 */
export function uniqueKeys(base: string[]): string[] {
  const seen = new Map<string, number>();
  return base.map((b) => {
    const n = (seen.get(b) ?? 0) + 1;
    seen.set(b, n);
    return n === 1 ? b : `${b}#${n}`;
  });
}
