"use client";

import { ArrowRight, type LucideIcon } from "lucide-react";
import { Button, Chip } from "@heroui/react";

export function ToolboxCard({
  actionLabel = "Configure",
  disabled = false,
  icon: Icon,
  iconClassName,
  onPress,
  status,
  summary,
  title,
}: {
  actionLabel?: string;
  disabled?: boolean;
  icon: LucideIcon;
  iconClassName: string;
  onPress?: () => void;
  status: string;
  summary: string;
  title: string;
}) {
  return (
    <Button
      className="admin-toolbox-card h-auto min-h-44 w-full min-w-0 justify-start overflow-hidden whitespace-normal rounded-2xl bg-surface p-0 text-left shadow-surface"
      isDisabled={disabled}
      onPress={onPress}
    >
      <span className="flex h-full w-full min-w-0 flex-col p-5">
        <span className="flex items-start justify-between gap-3">
          <span
            className={`admin-toolbox-card-icon flex size-10 shrink-0 items-center justify-center rounded-2xl ${iconClassName}`}
          >
            <Icon className="size-5" />
          </span>
          <Chip size="sm" variant="soft">
            {status}
          </Chip>
        </span>
        <span className="mt-4 block min-w-0">
          <span className="block truncate font-semibold text-foreground">{title}</span>
          <span className="mt-1 block line-clamp-2 text-sm leading-5 text-muted">{summary}</span>
        </span>
        <span className="mt-auto flex items-center gap-1 pt-4 text-sm font-medium text-accent">
          {actionLabel}
          <ArrowRight className="admin-toolbox-card-arrow size-4" />
        </span>
      </span>
    </Button>
  );
}
