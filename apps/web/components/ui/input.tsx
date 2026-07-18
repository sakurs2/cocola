import * as React from "react";
import { cn } from "@/lib/utils";

// shadcn-style Input, skinned for the cocola user UI (large radius, soft shadow,
// subtle blue focus ring). Authored against the design tokens in app/globals.css.

const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, type, ...props }, ref) => (
    <input
      type={type}
      ref={ref}
      className={cn(
        "flex h-9 w-full rounded-xl border border-input bg-background px-3 text-sm shadow-xs outline-none transition-colors",
        "placeholder:text-muted-foreground",
        "focus-visible:border-foreground/30 focus-visible:ring-2 focus-visible:ring-blue-500/20",
        "disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  ),
);
Input.displayName = "Input";

export { Input };
