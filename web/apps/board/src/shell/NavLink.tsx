import { Link } from "@tanstack/react-router";
import type { ComponentProps } from "react";

type Props = Omit<ComponentProps<typeof Link>, "to"> & { route: string };

// 選單項目的網址由伺服器給（字串），所以這裡放寬 Router 的型別檢查。
export function NavLink({ route, ...props }: Props) {
  return <Link to={route as "/"} {...props} />;
}
