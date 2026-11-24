// 編得過、但執行時丟錯的選單元件：error boundary 要換回預設選單。
export default function CrashMenu(): never {
  throw new Error("故意壞掉的選單");
}
