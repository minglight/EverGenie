import _ from "lodash";

// 編不過的選單元件（import 了套件以外的模組）。
export default function BadMenu() {
  return <nav>{_.upperCase("bad menu")}</nav>;
}
