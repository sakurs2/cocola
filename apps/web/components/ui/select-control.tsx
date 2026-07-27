"use client";

import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown, ChevronUp } from "lucide-react";
import { cn } from "@/lib/utils";

export type SelectOption = {
  value: string;
  label: string;
  disabled?: boolean;
};

type SelectControlProps = {
  value: string;
  onValueChange: (value: string) => void;
  options: readonly SelectOption[];
  id?: string;
  disabled?: boolean;
  ariaLabel?: string;
  placeholder?: string;
  className?: string;
  contentClassName?: string;
};

const VALUE_PREFIX = "cocola-select:";

function toPrimitiveValue(value: string) {
  return `${VALUE_PREFIX}${value}`;
}

function fromPrimitiveValue(value: string) {
  return value.slice(VALUE_PREFIX.length);
}

export function SelectControl({
  value,
  onValueChange,
  options,
  id,
  disabled,
  ariaLabel,
  placeholder = "Select an option",
  className,
  contentClassName,
}: SelectControlProps) {
  return (
    <SelectPrimitive.Root
      value={toPrimitiveValue(value)}
      onValueChange={(nextValue) => onValueChange(fromPrimitiveValue(nextValue))}
      disabled={disabled}
    >
      <SelectPrimitive.Trigger
        id={id}
        aria-label={ariaLabel}
        className={cn(
          "flex h-10 w-full min-w-0 items-center justify-between gap-2 rounded-xl border border-input bg-background px-3 text-sm text-foreground shadow-sm outline-none transition-colors",
          "hover:bg-muted/30 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/20",
          "disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
      >
        <SelectPrimitive.Value
          placeholder={placeholder}
          className="min-w-0 flex-1 truncate text-left"
        />
        <SelectPrimitive.Icon asChild>
          <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>

      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          align="start"
          sideOffset={4}
          className={cn(
            "z-[100] max-h-[min(20rem,var(--radix-select-content-available-height))] w-[var(--radix-select-trigger-width)] overflow-hidden rounded-xl border border-border bg-popover text-popover-foreground shadow-xl",
            contentClassName,
          )}
        >
          <SelectPrimitive.ScrollUpButton className="flex h-7 cursor-default items-center justify-center">
            <ChevronUp aria-hidden="true" className="size-4" />
          </SelectPrimitive.ScrollUpButton>
          <SelectPrimitive.Viewport className="max-h-72 p-1">
            {options.map((option) => (
              <SelectPrimitive.Item
                key={option.value}
                value={toPrimitiveValue(option.value)}
                disabled={option.disabled}
                className={cn(
                  "relative flex w-full cursor-default select-none items-center rounded-lg py-2 pl-2.5 pr-8 text-sm outline-none",
                  "focus:bg-accent focus:text-accent-foreground",
                  "data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
                )}
              >
                <SelectPrimitive.ItemText className="block min-w-0 flex-1 truncate">
                  {option.label}
                </SelectPrimitive.ItemText>
                <SelectPrimitive.ItemIndicator className="absolute right-2 inline-flex items-center">
                  <Check aria-hidden="true" className="size-4" />
                </SelectPrimitive.ItemIndicator>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
          <SelectPrimitive.ScrollDownButton className="flex h-7 cursor-default items-center justify-center">
            <ChevronDown aria-hidden="true" className="size-4" />
          </SelectPrimitive.ScrollDownButton>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
}
