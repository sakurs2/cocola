import * as React from "react";
import { cn } from "@/lib/utils";

// shadcn-style Label, skinned for the cocola user UI.
// Authored against the design tokens in app/globals.css.

const Label = React.forwardRef<HTMLLabelElement, React.LabelHTMLAttributes<HTMLLabelElement>>(
  ({ className, ...props }, ref) => (
    <label
      ref={ref}
      className={cn(
        "text-xs font-medium text-muted-foreground peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
        className,
      )}
      {...props}
    />
  ),
);
Label.displayName = "Label";

export { Label };
