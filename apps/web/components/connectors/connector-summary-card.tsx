"use client";

import { Button, Card } from "@heroui/react";
import type { ReactNode } from "react";

export type ConnectorSummaryStatus = {
  label: string;
  dotClassName: string;
  textClassName: string;
  checking?: boolean;
};

export type ConnectorSummaryAction = {
  label: string;
  icon?: ReactNode;
  disabled?: boolean;
  outline?: boolean;
  onPress?: () => void;
};

export function ConnectorSummaryCard({
  provider,
  title,
  description,
  icon,
  status,
  action,
}: {
  provider: "github" | "feishu";
  title: string;
  description: string;
  icon: ReactNode;
  status: ConnectorSummaryStatus;
  action: ConnectorSummaryAction;
}) {
  const brandAction = provider === "feishu" && !action.outline;

  return (
    <Card
      className="cocola-web-connector-card h-full w-full max-w-[300px] p-4"
      data-provider={provider}
    >
      <Card.Content className="flex h-full min-w-0 flex-col p-0">
        <div className="flex min-w-0 items-center gap-3">
          <span
            className={`cocola-web-connector-icon flex size-12 shrink-0 items-center justify-center rounded-2xl ${
              provider === "feishu"
                ? "bg-[#EEF3FF] dark:bg-[#16264F]"
                : "bg-zinc-950 text-white dark:bg-white dark:text-zinc-950"
            }`}
          >
            {icon}
          </span>
          <span className="min-w-0">
            <span className="text-foreground block font-semibold">{title}</span>
            <span className="text-muted mt-1 block truncate text-sm" title={description}>
              {description}
            </span>
          </span>
        </div>
        <div className="mt-4 flex items-center gap-2 text-xs font-medium">
          <span
            className={`size-2 rounded-full ${status.dotClassName} ${
              status.checking ? "animate-pulse motion-reduce:animate-none" : ""
            }`}
          />
          <span className={status.textClassName}>{status.label}</span>
        </div>
        <Button
          fullWidth
          className={`cocola-web-connector-action mt-5 ${
            brandAction
              ? "bg-[#3370FF] text-white hover:bg-[#2B60E8]"
              : action.outline
                ? ""
                : "bg-zinc-950 text-white dark:bg-white dark:text-zinc-950"
          }`}
          isDisabled={action.disabled}
          variant={action.outline ? "outline" : "primary"}
          onPress={action.onPress}
        >
          {action.icon}
          {action.label}
        </Button>
      </Card.Content>
    </Card>
  );
}
