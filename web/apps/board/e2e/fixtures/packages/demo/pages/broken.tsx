import _ from "lodash";

// 編不過的一般頁：看板要顯示白話原因，不拖累別的頁面。
export default function Broken() {
  return <p>{_.upperCase("broken")}</p>;
}
