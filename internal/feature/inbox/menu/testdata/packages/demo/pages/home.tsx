import { Button } from "@evergenie/ui";
import { useState } from "react";
import { label } from "../lib/util";

export default function Home() {
  const [n, setN] = useState(0);
  return <Button onClick={() => setN(n + 1)}>{label(n)}</Button>;
}
