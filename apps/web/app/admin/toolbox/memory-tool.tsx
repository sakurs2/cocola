"use client";

import { BrainCircuit, Construction } from "lucide-react";
import { AdminStatusBadge } from "@/components/admin/admin-ui";

export function MemoryTool() {
  return (
    <div
      className="admin-module-card w-full cursor-not-allowed text-left opacity-80"
      aria-disabled="true"
    >
      <span className="admin-module-head">
        <span className="admin-module-icon">
          <BrainCircuit className="size-6" strokeWidth={2} />
        </span>
        <span className="admin-module-title">Memory</span>
      </span>
      <span className="admin-module-summary">
        Memory is currently under development and is not available in this release.
      </span>
      <span className="flex items-center gap-2">
        <AdminStatusBadge tone="amber">In development</AdminStatusBadge>
      </span>
      <span className="admin-module-cta text-muted-foreground">
        <Construction className="size-3.5" />
        Unavailable
      </span>
    </div>
  );
}
