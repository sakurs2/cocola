"use client";

import { ShieldCheck } from "lucide-react";
import { Chip } from "@heroui/react";
import { AppLayout } from "@heroui-pro/react/app-layout";
import { useSession } from "next-auth/react";
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
  const { data: session } = useSession();
  const section = getAdminSectionForPathname(pathname);
  const userLabel = session?.user?.name || session?.user?.email || "Administrator";
  const navigate = useCallback((href: string) => router.push(href), [router]);

  return (
    <AppLayout
      className="cocola-admin-ui h-svh"
      navigate={navigate}
      navbar={<AdminTopbar userLabel={userLabel} />}
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

function AdminTopbar({ userLabel }: { userLabel: string }) {
  return (
    <div className="flex h-14 w-full items-center gap-3 px-3 sm:px-5">
      <AppLayout.MenuToggle />
      <div className="min-w-0 flex-1">
        <p className="text-accent truncate text-[11px] font-semibold uppercase tracking-[0.14em]">
          Control plane
        </p>
      </div>
      <Chip className="hidden sm:flex" color="success" size="sm" variant="soft">
        Self-hosted
      </Chip>
      <Chip className="hidden max-w-48 sm:flex" size="sm" variant="soft">
        <ShieldCheck className="text-accent size-3.5" />
        {userLabel}
      </Chip>
      <WorkspaceThemeToggle />
    </div>
  );
}
