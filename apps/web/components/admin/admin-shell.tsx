"use client";

import { AppLayout } from "@cocola/ui-compat/app-layout";
import { usePathname, useRouter } from "next/navigation";
import { useCallback, type ReactNode } from "react";
import { AdminSidebar } from "@/components/admin/admin-sidebar";
import {
  getAdminSectionForPathname,
  getAdminThemeStyle,
} from "@/components/admin/admin-navigation";
import { WorkspaceThemeToggle } from "@/components/assistant-ui/workspace-theme-toggle";

export function AdminShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const section = getAdminSectionForPathname(pathname);
  const navigate = useCallback((href: string) => router.push(href), [router]);

  return (
    <AppLayout
      className="cocola-admin-ui h-svh"
      navigate={navigate}
      navbar={<AdminTopbar />}
      scrollMode="content"
      sidebar={<AdminSidebar activeSectionId={section.id} />}
      sidebarCollapsible="offcanvas"
      sidebarDefaultSize="17rem"
      style={getAdminThemeStyle(section.theme)}
    >
      {children}
    </AppLayout>
  );
}

function AdminTopbar() {
  return (
    <div className="flex h-14 w-full items-center gap-3 px-3 sm:px-5">
      <AppLayout.MenuToggle />
      <div className="min-w-0 flex-1">
        <p className="text-accent truncate text-[11px] font-semibold uppercase tracking-[0.14em]">
          Control plane
        </p>
      </div>
      <WorkspaceThemeToggle />
    </div>
  );
}
