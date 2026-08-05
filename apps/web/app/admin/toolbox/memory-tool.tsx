"use client";

import { BrainCircuit } from "lucide-react";
import { ToolboxCard } from "./toolbox-card";

export function MemoryTool() {
  return (
    <ToolboxCard
      actionLabel="Unavailable"
      disabled
      icon={BrainCircuit}
      iconClassName="bg-violet-100 text-violet-600 dark:bg-violet-950 dark:text-violet-300"
      status="In development"
      summary="Configure retention and memory extraction policies."
      title="Memory"
    />
  );
}
