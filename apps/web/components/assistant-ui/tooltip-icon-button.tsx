"use client";

import * as React from "react";
import { Button, type ButtonProps } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// Lightweight icon button with a native tooltip (title attribute). The official
// assistant-ui version wraps Radix Tooltip; we keep it dependency-light since
// the surrounding chrome is minimal and a native tooltip reads fine here.
export interface TooltipIconButtonProps extends ButtonProps {
  tooltip: string;
}

export const TooltipIconButton = React.forwardRef<HTMLButtonElement, TooltipIconButtonProps>(
  ({ children, tooltip, className, variant = "ghost", size = "icon", ...rest }, ref) => (
    <Button
      ref={ref}
      variant={variant}
      size={size}
      title={tooltip}
      aria-label={tooltip}
      className={cn("size-6 p-1", className)}
      {...rest}
    >
      {children}
      <span className="sr-only">{tooltip}</span>
    </Button>
  ),
);
TooltipIconButton.displayName = "TooltipIconButton";
