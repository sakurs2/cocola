"use client";

import { Fragment, type ReactNode } from "react";

/**
 * HeroUI v3 no longer needs a provider. Keep this boundary in place so the
 * admin layout does not need to change while its components migrate.
 */
export function AdminHeroUIProvider({ children }: { children: ReactNode }) {
  return <Fragment>{children}</Fragment>;
}
