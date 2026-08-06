"use client";

import { forwardRef, type ComponentPropsWithRef, type CSSProperties, type ReactNode } from "react";

type ItemCardGroupVariant = "default" | "outline" | "secondary" | "tertiary" | "transparent";

export type ItemCardGroupRootProps = ComponentPropsWithRef<"div"> & {
  children: ReactNode;
  columns?: 2 | 3;
  layout?: "grid" | "list";
  variant?: ItemCardGroupVariant;
};

export type ItemCardGroupHeaderProps = ComponentPropsWithRef<"div"> & {
  children: ReactNode;
};
export type ItemCardGroupTitleProps = ComponentPropsWithRef<"h3"> & {
  children: ReactNode;
};
export type ItemCardGroupDescriptionProps = ComponentPropsWithRef<"p"> & {
  children: ReactNode;
};

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

export const ItemCardGroupRoot = forwardRef<HTMLDivElement, ItemCardGroupRootProps>(
  function ItemCardGroupRoot(
    { children, className, columns = 2, layout = "list", style, variant = "default", ...props },
    ref,
  ) {
    return (
      <div
        {...props}
        ref={ref}
        className={mergeClassNames(
          "item-card-group",
          `item-card-group--${layout}`,
          `item-card-group--${variant}`,
          className,
        )}
        data-layout={layout}
        data-slot="item-card-group"
        data-variant={variant}
        role={props.role ?? "group"}
        style={
          {
            ...style,
            "--item-card-group-columns": columns,
          } as CSSProperties
        }
      >
        {children}
      </div>
    );
  },
);

export const ItemCardGroupHeader = forwardRef<HTMLDivElement, ItemCardGroupHeaderProps>(
  function ItemCardGroupHeader({ className, ...props }, ref) {
    return (
      <div
        {...props}
        ref={ref}
        className={mergeClassNames("item-card-group__header", className)}
        data-slot="item-card-group-header"
      />
    );
  },
);

export const ItemCardGroupTitle = forwardRef<HTMLHeadingElement, ItemCardGroupTitleProps>(
  function ItemCardGroupTitle({ className, ...props }, ref) {
    return (
      <h3
        {...props}
        ref={ref}
        className={mergeClassNames("item-card-group__title", className)}
        data-slot="item-card-group-title"
      />
    );
  },
);

export const ItemCardGroupDescription = forwardRef<
  HTMLParagraphElement,
  ItemCardGroupDescriptionProps
>(function ItemCardGroupDescription({ className, ...props }, ref) {
  return (
    <p
      {...props}
      ref={ref}
      className={mergeClassNames("item-card-group__description", className)}
      data-slot="item-card-group-description"
    />
  );
});

export const ItemCardGroup = Object.assign(ItemCardGroupRoot, {
  Root: ItemCardGroupRoot,
  Header: ItemCardGroupHeader,
  Title: ItemCardGroupTitle,
  Description: ItemCardGroupDescription,
});
