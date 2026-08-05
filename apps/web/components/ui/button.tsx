"use client";

import * as React from "react";
import { Button as HeroButton } from "@heroui/react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center whitespace-nowrap rounded-xl text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-accent text-accent-foreground hover:bg-accent-hover",
        destructive: "bg-danger text-danger-foreground hover:bg-danger-hover",
        outline: "border border-separator bg-transparent text-foreground hover:bg-default-hover",
        secondary: "bg-default text-default-foreground hover:bg-default-hover",
        ghost: "text-foreground hover:bg-default-hover",
        link: "text-accent underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-4 py-2",
        sm: "h-9 rounded-xl px-3",
        lg: "h-11 rounded-xl px-8",
        icon: "h-10 w-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, disabled, onClick, children, value, ...props }, ref) => {
    const heroProps = {
      ...props,
      ref,
      className: cn(buttonVariants({ variant, size, className })),
      isDisabled: disabled,
      isIconOnly: size === "icon",
      value: typeof value === "string" ? value : value == null ? undefined : String(value),
      size: size === "sm" ? "sm" : size === "lg" ? "lg" : "md",
      variant:
        variant === "destructive"
          ? "danger"
          : variant === "outline"
            ? "outline"
            : variant === "secondary"
              ? "secondary"
              : variant === "ghost" || variant === "link"
                ? "ghost"
                : "primary",
      onPress: (event: unknown) =>
        onClick?.(event as React.MouseEvent<HTMLButtonElement>),
    } as unknown as React.ComponentProps<typeof HeroButton>;
    return <HeroButton {...heroProps}>{children}</HeroButton>;
  },
);
Button.displayName = "Button";

export { Button, buttonVariants };
