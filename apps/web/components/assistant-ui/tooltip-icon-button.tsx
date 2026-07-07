"use client";

import * as React from "react";
import * as Tooltip from "@radix-ui/react-tooltip";
import { Button, type ButtonProps } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export interface TooltipIconButtonProps extends ButtonProps {
  tooltip: string;
}

export const TooltipIconButton = React.forwardRef<HTMLButtonElement, TooltipIconButtonProps>(
  ({ children, tooltip, className, variant = "ghost", size = "icon", ...rest }, ref) => {
    const button = (
      <Button
        ref={ref}
        variant={variant}
        size={size}
        aria-label={tooltip}
        className={cn("size-6 p-1", className)}
        {...rest}
      >
        {children}
        <span className="sr-only">{tooltip}</span>
      </Button>
    );

    if (rest.disabled) return button;

    return (
      <Tooltip.Provider delayDuration={220}>
        <Tooltip.Root>
          <Tooltip.Trigger asChild>{button}</Tooltip.Trigger>
          <Tooltip.Portal>
            <Tooltip.Content
              sideOffset={8}
              className="z-50 rounded-md border border-border bg-popover px-2 py-1 text-xs text-popover-foreground shadow-lg"
            >
              {tooltip}
              <Tooltip.Arrow className="fill-popover" />
            </Tooltip.Content>
          </Tooltip.Portal>
        </Tooltip.Root>
      </Tooltip.Provider>
    );
  },
);
TooltipIconButton.displayName = "TooltipIconButton";
