"use client";

import {
  Wrench as ToolboxIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { useState, type ComponentType } from "react";
import { AdminPage, AdminPageHeader, AdminPanel } from "@/components/admin/admin-ui";
import { SystemPromptTool } from "./system-prompt-tool";

export type ToolboxToolId = "system-prompt";

type ToolboxToolProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const TOOLBOX_ITEMS: readonly {
  id: ToolboxToolId;
  component: ComponentType<ToolboxToolProps>;
}[] = [{ id: "system-prompt", component: SystemPromptTool }];

export function ToolboxClient({ initialTool }: { initialTool: ToolboxToolId | null }) {
  const router = useRouter();
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
    <AdminPage width="standard">
      <AdminPageHeader
        eyebrow="Configuration"
        icon={<ToolboxIcon className="size-[18px]" />}
        title="Toolbox"
        description="Open lightweight controls that shape how Cocola operates."
      />

      <AdminPanel
        title="Available tools"
        description="Small, independent controls for administrators."
        contentClassName="grid gap-3 sm:grid-cols-2 xl:grid-cols-3"
      >
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
      </AdminPanel>
    </AdminPage>
  );
}
