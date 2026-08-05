"use client";

import { ArrowLeft } from "lucide-react";
import { Sheet } from "@heroui-pro/react/sheet";
import { Sidebar } from "@heroui-pro/react/sidebar";
import { CocolaCoreLogo } from "@/components/cocola-core-logo";
import {
  ADMIN_GROUPS,
  getAdminSection,
  type AdminSection,
  type AdminSectionId,
} from "@/components/admin/admin-navigation";

export function AdminSidebar({ activeSectionId }: { activeSectionId: AdminSectionId }) {
  return (
    <>
      <Sidebar>
        <AdminSidebarContents activeSectionId={activeSectionId} />
      </Sidebar>
      <Sidebar.Mobile>
        <Sheet.Heading className="sr-only">Cocola admin navigation</Sheet.Heading>
        <AdminSidebarContents activeSectionId={activeSectionId} idPrefix="mobile-" />
      </Sidebar.Mobile>
    </>
  );
}

function AdminSidebarContents({
  activeSectionId,
  idPrefix = "",
}: {
  activeSectionId: AdminSectionId;
  idPrefix?: string;
}) {
  const overview = getAdminSection("overview");
  const settings = getAdminSection("settings");

  return (
    <>
      <Sidebar.Header>
        <div className="grid grid-cols-[1.25rem_minmax(0,1fr)] items-center gap-3 px-1 py-1.5">
          <span className="flex size-5 shrink-0 items-center justify-center">
            <CocolaCoreLogo className="size-10 max-w-none shrink-0" />
          </span>
          <div className="flex min-w-0 flex-col" data-sidebar="label">
            <span className="text-foreground text-sm font-semibold leading-tight">
              cocola admin
            </span>
            <span className="text-muted text-xs leading-tight">control plane</span>
          </div>
        </div>
      </Sidebar.Header>

      <Sidebar.Content>
        <Sidebar.Group>
          <Sidebar.Menu aria-label="Admin overview">
            <AdminSidebarItem
              activeSectionId={activeSectionId}
              idPrefix={idPrefix}
              section={overview}
            />
          </Sidebar.Menu>
        </Sidebar.Group>

        {ADMIN_GROUPS.map((group) => (
          <Sidebar.Group key={group.label}>
            <Sidebar.GroupLabel>{group.label}</Sidebar.GroupLabel>
            <Sidebar.Menu aria-label={`${group.label} navigation`}>
              {group.sectionIds.map((sectionId) => (
                <AdminSidebarItem
                  key={sectionId}
                  activeSectionId={activeSectionId}
                  idPrefix={idPrefix}
                  section={getAdminSection(sectionId)}
                />
              ))}
            </Sidebar.Menu>
          </Sidebar.Group>
        ))}
      </Sidebar.Content>

      <Sidebar.Footer>
        <Sidebar.Menu aria-label="Admin settings">
          <AdminSidebarItem
            activeSectionId={activeSectionId}
            idPrefix={idPrefix}
            section={settings}
          />
        </Sidebar.Menu>
        <Sidebar.Menu aria-label="Workspace navigation">
          <Sidebar.MenuItem
            className="cocola-sidebar-tab"
            href="/"
            id={`${idPrefix}workspace`}
            textValue="Back to workspace"
          >
            <Sidebar.MenuIcon className="cocola-sidebar-tab-icon">
              <ArrowLeft className="text-accent size-4" />
            </Sidebar.MenuIcon>
            <Sidebar.MenuLabel>Back to workspace</Sidebar.MenuLabel>
          </Sidebar.MenuItem>
        </Sidebar.Menu>
      </Sidebar.Footer>
    </>
  );
}

function AdminSidebarItem({
  activeSectionId,
  idPrefix,
  section,
}: {
  activeSectionId: AdminSectionId;
  idPrefix: string;
  section: AdminSection;
}) {
  const Icon = section.icon;
  const href = section.id === "overview" ? "/admin" : `/admin/${section.path}`;

  return (
    <Sidebar.MenuItem
      className="cocola-sidebar-tab"
      href={href}
      id={`${idPrefix}${section.id}`}
      isCurrent={activeSectionId === section.id}
      textValue={section.label}
    >
      <Sidebar.MenuIcon className="cocola-sidebar-tab-icon">
        <Icon className={`size-4 ${section.iconClassName}`} />
      </Sidebar.MenuIcon>
      <Sidebar.MenuLabel>{section.label}</Sidebar.MenuLabel>
    </Sidebar.MenuItem>
  );
}
