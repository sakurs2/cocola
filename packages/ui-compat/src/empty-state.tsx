"use client";

import { EmptyState as HeroUIEmptyState } from "@heroui/react";
import { forwardRef, type ComponentProps, type ComponentPropsWithRef, type ReactNode } from "react";

type EmptyStateSize = "sm" | "md" | "lg";

export type EmptyStateRootProps = ComponentProps<typeof HeroUIEmptyState> & {
  children: ReactNode;
  size?: EmptyStateSize;
};

export type EmptyStateHeaderProps = ComponentPropsWithRef<"div"> & {
  children: ReactNode;
};

export type EmptyStateMediaProps = ComponentPropsWithRef<"div"> & {
  children: ReactNode;
  variant?: "default" | "icon";
};

export type EmptyStateTitleProps = ComponentPropsWithRef<"h3"> & {
  children: ReactNode;
};

export type EmptyStateDescriptionProps = ComponentPropsWithRef<"p"> & {
  children: ReactNode;
};

export type EmptyStateContentProps = ComponentPropsWithRef<"div"> & {
  children: ReactNode;
};

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

export function EmptyStateRoot({
  children,
  className,
  size = "md",
  ...props
}: EmptyStateRootProps) {
  return (
    <HeroUIEmptyState
      {...props}
      className={mergeClassNames("cocola-empty-state", className)}
      data-size={size}
    >
      {children}
    </HeroUIEmptyState>
  );
}

export const EmptyStateHeader = forwardRef<HTMLDivElement, EmptyStateHeaderProps>(
  function EmptyStateHeader({ children, className, ...props }, ref) {
    return (
      <div
        {...props}
        ref={ref}
        className={mergeClassNames("cocola-empty-state__header", className)}
      >
        {children}
      </div>
    );
  },
);

export const EmptyStateMedia = forwardRef<HTMLDivElement, EmptyStateMediaProps>(
  function EmptyStateMedia({ children, className, variant = "default", ...props }, ref) {
    return (
      <div
        {...props}
        ref={ref}
        className={mergeClassNames("cocola-empty-state__media", className)}
        data-variant={variant}
      >
        {children}
      </div>
    );
  },
);

export const EmptyStateTitle = forwardRef<HTMLHeadingElement, EmptyStateTitleProps>(
  function EmptyStateTitle({ children, className, ...props }, ref) {
    return (
      <h3 {...props} ref={ref} className={mergeClassNames("cocola-empty-state__title", className)}>
        {children}
      </h3>
    );
  },
);

export const EmptyStateDescription = forwardRef<HTMLParagraphElement, EmptyStateDescriptionProps>(
  function EmptyStateDescription({ children, className, ...props }, ref) {
    return (
      <p
        {...props}
        ref={ref}
        className={mergeClassNames("cocola-empty-state__description", className)}
      >
        {children}
      </p>
    );
  },
);

export const EmptyStateContent = forwardRef<HTMLDivElement, EmptyStateContentProps>(
  function EmptyStateContent({ children, className, ...props }, ref) {
    return (
      <div
        {...props}
        ref={ref}
        className={mergeClassNames("cocola-empty-state__content", className)}
      >
        {children}
      </div>
    );
  },
);

export const EmptyState = Object.assign(EmptyStateRoot, {
  Root: EmptyStateRoot,
  Header: EmptyStateHeader,
  Media: EmptyStateMedia,
  Title: EmptyStateTitle,
  Description: EmptyStateDescription,
  Content: EmptyStateContent,
});
