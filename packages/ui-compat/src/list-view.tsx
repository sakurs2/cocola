"use client";

import {
  GridList as GridListPrimitive,
  GridListItem as GridListItemPrimitive,
  type GridListItemProps,
  type GridListItemRenderProps,
  type GridListProps,
  type GridListRenderProps,
} from "react-aria-components";
import { forwardRef, type ComponentPropsWithRef, type ReactNode } from "react";

export type ListViewVariant = "primary" | "secondary";

export interface ListViewRootProps<T extends object> extends Omit<
  GridListProps<T>,
  "layout" | "orientation" | "renderEmptyState"
> {
  /** Visual variant. @default "primary" */
  variant?: ListViewVariant;
  /** Accepted for API compatibility. Native collection rendering remains enabled. */
  virtualized?: boolean;
  /** Estimated row height used by virtualized consumers. @default 48 */
  rowHeight?: number;
  /** Render function for the empty state. */
  renderEmptyState?: () => ReactNode;
}

export interface ListViewItemProps<T extends object> extends Omit<
  GridListItemProps<T>,
  "children"
> {
  children: ReactNode;
}

export interface ListViewItemContentProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
}

export interface ListViewTitleProps extends ComponentPropsWithRef<"span"> {
  children: ReactNode;
}

export interface ListViewDescriptionProps extends ComponentPropsWithRef<"span"> {
  children: ReactNode;
}

export interface ListViewItemActionProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
}

type RenderClassName<T> = string | ((values: T) => string) | undefined;

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

function mergeRenderClassName<T>(base: string, className: RenderClassName<T>) {
  if (typeof className === "function") {
    return (values: T) => mergeClassNames(base, className(values));
  }
  return mergeClassNames(base, className);
}

export function ListViewRoot<T extends object>({
  children,
  className,
  renderEmptyState,
  rowHeight = 48,
  selectionMode,
  variant = "primary",
  virtualized = false,
  ...props
}: ListViewRootProps<T>) {
  return (
    <GridListPrimitive<T>
      {...props}
      className={mergeRenderClassName<GridListRenderProps>(
        mergeClassNames("list-view", `list-view--${variant}`),
        className as RenderClassName<GridListRenderProps>,
      )}
      data-row-height={rowHeight}
      data-slot="list-view"
      data-variant={variant}
      data-virtualized={virtualized || undefined}
      renderEmptyState={
        renderEmptyState
          ? () => (
              <div className="list-view__empty-state" data-slot="list-view-empty-state">
                {renderEmptyState()}
              </div>
            )
          : undefined
      }
      selectionMode={selectionMode}
    >
      {children}
    </GridListPrimitive>
  );
}

export function ListViewItem<T extends object>({
  children,
  className,
  ...props
}: ListViewItemProps<T>) {
  return (
    <GridListItemPrimitive<T>
      {...props}
      className={mergeRenderClassName<GridListItemRenderProps>(
        "list-view__item",
        className as RenderClassName<GridListItemRenderProps>,
      )}
      data-slot="list-view-item"
    >
      {children}
    </GridListItemPrimitive>
  );
}

export const ListViewItemContent = forwardRef<HTMLDivElement, ListViewItemContentProps>(
  function ListViewItemContent({ children, className, ...props }, ref) {
    return (
      <div
        {...props}
        ref={ref}
        className={mergeClassNames("list-view__item-content", className)}
        data-slot="list-view-item-content"
      >
        {children}
      </div>
    );
  },
);

export const ListViewTitle = forwardRef<HTMLSpanElement, ListViewTitleProps>(function ListViewTitle(
  { children, className, ...props },
  ref,
) {
  return (
    <span
      {...props}
      ref={ref}
      className={mergeClassNames("list-view__title", className)}
      data-slot="list-view-title"
    >
      {children}
    </span>
  );
});

export const ListViewDescription = forwardRef<HTMLSpanElement, ListViewDescriptionProps>(
  function ListViewDescription({ children, className, ...props }, ref) {
    return (
      <span
        {...props}
        ref={ref}
        className={mergeClassNames("list-view__description", className)}
        data-slot="list-view-description"
      >
        {children}
      </span>
    );
  },
);

export const ListViewItemAction = forwardRef<HTMLDivElement, ListViewItemActionProps>(
  function ListViewItemAction({ children, className, ...props }, ref) {
    return (
      <div
        {...props}
        ref={ref}
        className={mergeClassNames("list-view__item-action", className)}
        data-slot="list-view-item-action"
      >
        {children}
      </div>
    );
  },
);

export const ListView = Object.assign(ListViewRoot, {
  Root: ListViewRoot,
  Item: ListViewItem,
  ItemContent: ListViewItemContent,
  Title: ListViewTitle,
  Description: ListViewDescription,
  ItemAction: ListViewItemAction,
});
