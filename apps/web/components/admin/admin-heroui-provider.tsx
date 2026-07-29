"use client";

import { HeroUIProvider } from "@heroui/react";
import type { ReactNode } from "react";

/**
 * Admin-scoped HeroUI provider. Wraps only the /admin subtree so HeroUI
 * context (overlays, portals, a11y, routing) is available to admin pages
 * without leaking into the user surface. `display: contents` keeps the
 * provider transparent to the existing admin layout/grid.
 */
export function AdminHeroUIProvider({ children }: { children: ReactNode }) {
  return <HeroUIProvider className="contents">{children}</HeroUIProvider>;
}
