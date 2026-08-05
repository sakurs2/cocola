"use client";

import { Chip } from "@heroui/react";
import { ItemCard } from "@heroui-pro/react/item-card";
import { ItemCardGroup } from "@heroui-pro/react/item-card-group";
import { Database } from "lucide-react";

export function MemoryPanel() {
  return (
    <ItemCardGroup>
      <ItemCardGroup.Header>
        <ItemCardGroup.Title>Memory</ItemCardGroup.Title>
      </ItemCardGroup.Header>
      <ItemCard>
        <ItemCard.Icon className="bg-violet-100 text-violet-600 dark:bg-violet-950 dark:text-violet-300">
          <Database className="size-4" />
        </ItemCard.Icon>
        <ItemCard.Content>
          <ItemCard.Title>Personal memory</ItemCard.Title>
          <ItemCard.Description>Memory management is under development.</ItemCard.Description>
        </ItemCard.Content>
        <ItemCard.Action>
          <Chip size="sm" variant="soft">
            Coming soon
          </Chip>
        </ItemCard.Action>
      </ItemCard>
    </ItemCardGroup>
  );
}
