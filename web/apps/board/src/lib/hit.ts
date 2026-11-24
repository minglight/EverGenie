// 視覺上比較小的按鈕／連結：用看不見的 ::after 把可點範圍補到高 44（FEN-7、UXC-5；DEV-156 U-11）。
// 上下置中、高固定 44，不管本體多高（24、28、36…）都補得到；本體超過 44 就沒有作用。
// 元素本身要能當定位基準，所以帶 relative；已經是 absolute／fixed 的不要用這個（自己寫 after:）。
export const HIT_44 =
  "relative after:absolute after:inset-x-0 after:top-1/2 after:h-11 after:-translate-y-1/2 after:content-['']";
