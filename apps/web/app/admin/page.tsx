"use client";

import type { CSSProperties } from "react";
import { ArrowRight } from "lucide-react";
import { Card, ScrollShadow } from "@heroui/react";
import Link from "next/link";
import {
  ADMIN_GROUPS,
  getAdminSection,
  getAdminThemeStyle,
} from "@/components/admin/admin-navigation";

export default function AdminPage() {
  return (
    <ScrollShadow hideScrollBar className="h-full overflow-y-auto">
      <main className="mx-auto w-full max-w-[100rem] px-4 py-5 sm:px-6 sm:py-6">
        <div className="grid gap-4 xl:grid-cols-2">
          {ADMIN_GROUPS.map((group, groupIndex) => (
            <section
              key={group.label}
              className="admin-overview-group bg-surface-secondary rounded-3xl p-3 sm:p-4"
              style={{ "--overview-delay": `${Math.min(groupIndex * 35, 140)}ms` } as CSSProperties}
            >
              <h1 className="text-foreground mb-3 px-1 text-sm font-semibold">{group.label}</h1>
              <div className="grid gap-3 sm:grid-cols-2">
                {group.sectionIds.map((sectionId) => {
                  const section = getAdminSection(sectionId);
                  const Icon = section.icon;

                  return (
                    <Link
                      key={section.id}
                      className="group block h-full rounded-2xl no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
                      href={`/admin/${section.path}`}
                      style={getAdminThemeStyle(section.theme)}
                    >
                      <Card className="admin-overview-card h-full min-h-40 p-5">
                        <Card.Content className="grid h-full grid-cols-[auto_minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)_auto] items-start gap-x-3 gap-y-2 p-0">
                          <span className="admin-overview-icon bg-accent-soft text-accent row-span-2 flex size-11 items-center justify-center rounded-2xl">
                            <Icon className="size-5" />
                          </span>
                          <span className="text-foreground text-base font-semibold">{section.label}</span>
                          <span className="text-muted line-clamp-2 text-sm leading-5">
                            {section.description}
                          </span>
                          <span className="admin-overview-cta col-span-2 flex w-fit items-center justify-center gap-1.5 self-end rounded-full px-4 py-2 text-[13px] font-semibold text-white">
                            Open
                            <ArrowRight className="admin-overview-arrow size-3.5" />
                          </span>
                        </Card.Content>
                      </Card>
                    </Link>
                  );
                })}
              </div>
            </section>
          ))}
        </div>
      </main>
    </ScrollShadow>
  );
}
