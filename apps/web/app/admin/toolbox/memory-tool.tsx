"use client";

import { BrainCircuit, Construction } from "lucide-react";
import { ItemCard } from "@heroui-pro/react/item-card";
import { AdminStatusBadge } from "@/components/admin/admin-ui";

export function MemoryTool() {
  return (
    <ItemCard
      className="min-h-52 w-full cursor-not-allowed opacity-70"
      aria-disabled="true"
    >
      <ItemCard.Icon className="bg-violet-100 text-violet-600 dark:bg-violet-950 dark:text-violet-300"><BrainCircuit className="size-5" /></ItemCard.Icon>
      <ItemCard.Content><ItemCard.Title>Memory</ItemCard.Title><ItemCard.Description>Memory is currently under development and is not available in this release.</ItemCard.Description><span className="mt-4 flex items-center gap-2">
        <AdminStatusBadge tone="amber">In development</AdminStatusBadge>
      </span></ItemCard.Content>
      <ItemCard.Action><span className="text-muted flex items-center gap-1 text-sm"><Construction className="size-4" />Unavailable</span></ItemCard.Action>
    </ItemCard>
  );
}
