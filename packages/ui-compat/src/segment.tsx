"use client";

import { ToggleButton, ToggleButtonGroup, type Key, type Selection } from "react-aria-components";
import { forwardRef, type ComponentPropsWithRef } from "react";

export type SegmentRootProps = Omit<
  ComponentPropsWithRef<typeof ToggleButtonGroup>,
  "selectionMode" | "selectedKeys" | "defaultSelectedKeys" | "onSelectionChange"
> & {
  selectedKey?: Key | null;
  defaultSelectedKey?: Key;
  onSelectionChange?: (key: Key) => void;
  size?: "sm" | "md" | "lg";
  variant?: "default" | "ghost";
};

export type SegmentItemProps = ComponentPropsWithRef<typeof ToggleButton>;
export type SegmentSeparatorProps = ComponentPropsWithRef<"span">;

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

export function SegmentRoot({
  children,
  className,
  defaultSelectedKey,
  onSelectionChange,
  selectedKey,
  size = "md",
  variant = "default",
  ...props
}: SegmentRootProps) {
  const selectionProps =
    selectedKey !== undefined
      ? { selectedKeys: selectedKey === null ? new Set<Key>() : new Set([selectedKey]) }
      : defaultSelectedKey !== undefined
        ? { defaultSelectedKeys: new Set([defaultSelectedKey]) }
        : {};

  const handleSelectionChange = (selection: Selection) => {
    if (selection === "all") return;
    const key = selection.values().next().value;
    if (key !== undefined) onSelectionChange?.(key);
  };

  return (
    <ToggleButtonGroup
      {...props}
      {...selectionProps}
      className={(renderProps) =>
        mergeClassNames(
          "segment",
          `segment--${size}`,
          variant !== "default" ? `segment--${variant}` : undefined,
          typeof className === "function" ? className(renderProps) : className,
        )
      }
      data-size={size}
      data-slot="segment"
      data-variant={variant}
      disallowEmptySelection
      selectionMode="single"
      onSelectionChange={handleSelectionChange}
    >
      {children}
    </ToggleButtonGroup>
  );
}

export const SegmentItem = forwardRef<HTMLButtonElement, SegmentItemProps>(function SegmentItem(
  { children, className, ...props },
  ref,
) {
  return (
    <ToggleButton
      {...props}
      ref={ref}
      className={(renderProps) =>
        mergeClassNames(
          "segment__item",
          typeof className === "function" ? className(renderProps) : className,
        )
      }
      data-slot="segment-item"
    >
      {(renderProps) => (
        <>
          {renderProps.isSelected ? (
            <span aria-hidden="true" className="segment__indicator" data-slot="segment-indicator" />
          ) : null}
          {typeof children === "function" ? children(renderProps) : children}
        </>
      )}
    </ToggleButton>
  );
});

export const SegmentSeparator = forwardRef<HTMLSpanElement, SegmentSeparatorProps>(
  function SegmentSeparator({ className, ...props }, ref) {
    return (
      <span
        {...props}
        ref={ref}
        aria-hidden="true"
        className={mergeClassNames("segment__separator", className)}
        data-slot="segment-separator"
      />
    );
  },
);

export const Segment = Object.assign(SegmentRoot, {
  Root: SegmentRoot,
  Item: SegmentItem,
  Separator: SegmentSeparator,
});
