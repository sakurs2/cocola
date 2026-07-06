"use client";

import { type ReactNode } from "react";
import { usePathname } from "next/navigation";
import { CocolaRuntimeProvider } from "@/app/runtime-provider";
import { AppSidebar } from "@/components/assistant-ui/app-sidebar";

function isWorkspacePath(pathname: string | null) {
  return (
    pathname === "/" ||
    pathname === "/skills" ||
    pathname?.startsWith("/skills/") ||
    pathname === "/mcps" ||
    pathname?.startsWith("/mcps/")
  );
}

export function WorkspaceShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();

  if (!isWorkspacePath(pathname)) {
    return <>{children}</>;
  }

  return (
    <CocolaRuntimeProvider>
      <div className="flex h-screen bg-background text-foreground">
        <AppSidebar />
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </CocolaRuntimeProvider>
  );
}
