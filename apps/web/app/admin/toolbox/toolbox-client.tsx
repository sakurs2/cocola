"use client";

import { Wrench as ToolboxIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useState, type ComponentType } from "react";
import { AdminPage, AdminPageHeader } from "@/components/admin/admin-ui";
import { SystemPromptTool } from "./system-prompt-tool";
import { MemoryTool } from "./memory-tool";

export type ToolboxToolId = "system-prompt" | "memory";

type ToolboxToolProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const TOOLBOX_ITEMS: readonly {
  id: ToolboxToolId;
  component: ComponentType<ToolboxToolProps>;
}[] = [
  { id: "system-prompt", component: SystemPromptTool },
  { id: "memory", component: MemoryTool },
];

export function ToolboxClient({ initialTool }: { initialTool: ToolboxToolId | null }) {
  const router = useRouter();
  const t = useTranslations("admin.toolboxPage");
  const [activeTool, setActiveTool] = useState<ToolboxToolId | null>(initialTool);

  const setToolOpen = (tool: ToolboxToolId, open: boolean) => {
    const nextTool = open ? tool : null;
    setActiveTool(nextTool);
    router.replace(
      nextTool ? `/admin/toolbox?tool=${encodeURIComponent(nextTool)}` : "/admin/toolbox",
      {
        scroll: false,
      },
    );
  };

  return (
    <AdminPage className="admin-theme-cyan">
      <AdminPageHeader
        icon={<ToolboxIcon className="size-[18px]" />}
        title={t("title")}
        description={t("description")}
      />

      <section className="grid items-stretch gap-4 md:grid-cols-2 xl:grid-cols-3">
        {TOOLBOX_ITEMS.map((item) => {
          const Tool = item.component;
          return (
            <Tool
              key={item.id}
              open={activeTool === item.id}
              onOpenChange={(open) => setToolOpen(item.id, open)}
            />
          );
        })}
      </section>
    </AdminPage>
  );
}
