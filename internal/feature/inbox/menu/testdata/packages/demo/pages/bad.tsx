import _ from "lodash";

export default function Bad() {
  return <p>{_.upperCase("bad")}</p>;
}
