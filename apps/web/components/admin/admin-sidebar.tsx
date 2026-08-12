"use client";

import { ArrowLeft } from "lucide-react";
import { useTranslations } from "next-intl";
import { Sheet } from "@cocola/ui-compat/sheet";
import { Sidebar } from "@cocola/ui-compat/sidebar";
import { CocolaCoreLogo } from "@/components/cocola-core-logo";
import {
  ADMIN_GROUPS,
  getAdminSection,
  type AdminSection,
  type AdminSectionId,
} from "@/components/admin/admin-navigation";

export function AdminSidebar({ activeSectionId }: { activeSectionId: AdminSectionId }) {
  const t = useTranslations("admin.shell");
  return (
    <>
      <Sidebar>
        <AdminSidebarContents activeSectionId={activeSectionId} />
      </Sidebar>
      <Sidebar.Mobile>
        <Sheet.Heading className="sr-only">{t("mobileNavigation")}</Sheet.Heading>
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
  const t = useTranslations("admin");
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
              {t("shell.title")}
            </span>
            <span className="text-muted text-xs leading-tight">{t("shell.subtitle")}</span>
          </div>
        </div>
      </Sidebar.Header>

      <Sidebar.Content className="overscroll-contain pb-3 pt-1">
        <Sidebar.Group>
          <Sidebar.Menu aria-label={t("shell.overviewNavigation")}>
            <AdminSidebarItem
              activeSectionId={activeSectionId}
              idPrefix={idPrefix}
              section={overview}
            />
          </Sidebar.Menu>
        </Sidebar.Group>

        {ADMIN_GROUPS.map((group) => (
          <Sidebar.Group key={group.id}>
            <Sidebar.GroupLabel>{t(`groups.${group.id}`)}</Sidebar.GroupLabel>
            <Sidebar.Menu aria-label={t(`groups.${group.id}`)}>
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

      <Sidebar.Footer className="relative z-10 border-t border-separator bg-background">
        <Sidebar.Menu aria-label={t("shell.settingsNavigation")}>
          <AdminSidebarItem
            activeSectionId={activeSectionId}
            idPrefix={idPrefix}
            section={settings}
          />
        </Sidebar.Menu>
        <Sidebar.Menu aria-label={t("shell.workspaceNavigation")}>
          <Sidebar.MenuItem
            className="cocola-sidebar-tab"
            href="/"
            id={`${idPrefix}workspace`}
            textValue={t("shell.backToWorkspace")}
          >
            <Sidebar.MenuIcon className="cocola-sidebar-tab-icon">
              <ArrowLeft className="text-accent size-4" />
            </Sidebar.MenuIcon>
            <Sidebar.MenuLabel>{t("shell.backToWorkspace")}</Sidebar.MenuLabel>
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
  const t = useTranslations("admin");
  const Icon = section.icon;
  const href = section.id === "overview" ? "/admin" : `/admin/${section.path}`;

  return (
    <Sidebar.MenuItem
      className="cocola-sidebar-tab"
      href={href}
      id={`${idPrefix}${section.id}`}
      isCurrent={activeSectionId === section.id}
      textValue={t(`sections.${section.id}.label`)}
    >
      <Sidebar.MenuIcon className="cocola-sidebar-tab-icon">
        <Icon className={`size-4 ${section.iconClassName}`} />
      </Sidebar.MenuIcon>
      <Sidebar.MenuLabel>{t(`sections.${section.id}.label`)}</Sidebar.MenuLabel>
    </Sidebar.MenuItem>
  );
}
