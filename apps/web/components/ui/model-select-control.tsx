"use client";

import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown, ChevronUp } from "lucide-react";
import { ModelIcon } from "@/components/ui/model-icon";
import type { ModelIconConfig } from "@/lib/model-icons";
import { cn } from "@/lib/utils";

export type ModelSelectItem = {
  id: string;
  label: string;
  provider?: string;
  icon?: ModelIconConfig;
  disabled?: boolean;
  suffix?: string;
};

export type ModelSelectModel = {
  id: string;
  label: string;
  alias?: string;
  provider?: string;
  icon?: ModelIconConfig;
};

type ModelSelectControlProps = {
  value: string;
  onValueChange: (value: string) => void;
  models: readonly ModelSelectModel[];
  /**
   * Extra option prepended to the list when the currently selected model is
   * missing from `models` (e.g. removed by an admin). Purely presentational —
   * still emits `value` on selection.
   */
  fallback?: ModelSelectItem;
  id?: string;
  disabled?: boolean;
  ariaLabel?: string;
  placeholder?: string;
  className?: string;
  contentClassName?: string;
};

const VALUE_PREFIX = "cocola-model-select:";

const toPrimitive = (value: string) => `${VALUE_PREFIX}${value}`;
const fromPrimitive = (value: string) => value.slice(VALUE_PREFIX.length);

function toItem(model: ModelSelectModel): ModelSelectItem {
  return {
    id: model.id,
    label: model.label || model.alias || model.id,
    ...(model.provider ? { provider: model.provider } : {}),
    ...(model.icon ? { icon: model.icon } : {}),
  };
}

export function ModelSelectControl({
  value,
  onValueChange,
  models,
  fallback,
  id,
  disabled,
  ariaLabel,
  placeholder = "Select a model",
  className,
  contentClassName,
}: ModelSelectControlProps) {
  const items: ModelSelectItem[] = [
    ...(fallback && !models.some((model) => model.id === fallback.id) ? [fallback] : []),
    ...models.map(toItem),
  ];
  const active = items.find((item) => item.id === value);

  return (
    <SelectPrimitive.Root
      value={toPrimitive(value)}
      onValueChange={(nextValue) => onValueChange(fromPrimitive(nextValue))}
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
        <span className="flex min-w-0 flex-1 items-center gap-2">
          {active?.icon ? (
            <ModelIcon icon={active.icon} className="size-4 shrink-0" bare />
          ) : null}
          <SelectPrimitive.Value placeholder={placeholder}>
            {active ? (
              <span className="flex min-w-0 items-center gap-1.5 truncate text-left">
                <span className="truncate font-medium text-foreground">{active.label}</span>
                {active.provider ? (
                  <span className="shrink-0 text-xs text-muted-foreground">
                    · {active.provider}
                  </span>
                ) : null}
                {active.suffix ? (
                  <span className="shrink-0 text-xs text-muted-foreground">
                    · {active.suffix}
                  </span>
                ) : null}
              </span>
            ) : null}
          </SelectPrimitive.Value>
        </span>
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
            "z-[100] max-h-[min(22rem,var(--radix-select-content-available-height))] w-[var(--radix-select-trigger-width)] overflow-hidden rounded-xl border border-border bg-popover text-popover-foreground shadow-xl",
            contentClassName,
          )}
        >
          <SelectPrimitive.ScrollUpButton className="flex h-7 cursor-default items-center justify-center">
            <ChevronUp aria-hidden="true" className="size-4" />
          </SelectPrimitive.ScrollUpButton>
          <SelectPrimitive.Viewport className="max-h-80 p-1">
            {items.map((item) => (
              <SelectPrimitive.Item
                key={item.id}
                value={toPrimitive(item.id)}
                disabled={item.disabled}
                className={cn(
                  "relative flex w-full cursor-default select-none items-center gap-2.5 rounded-lg py-2 pl-2.5 pr-8 text-sm outline-none",
                  "focus:bg-accent focus:text-accent-foreground",
                  "data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
                )}
              >
                {item.icon ? (
                  <ModelIcon icon={item.icon} className="size-4 shrink-0" bare />
                ) : (
                  <span className="size-4 shrink-0" />
                )}
                <span className="flex min-w-0 flex-1 items-center gap-1.5">
                  <SelectPrimitive.ItemText asChild>
                    <span className="truncate font-medium">{item.label}</span>
                  </SelectPrimitive.ItemText>
                  {item.provider ? (
                    <span className="shrink-0 text-xs text-muted-foreground">
                      · {item.provider}
                    </span>
                  ) : null}
                  {item.suffix ? (
                    <span className="shrink-0 text-xs text-muted-foreground">
                      · {item.suffix}
                    </span>
                  ) : null}
                </span>
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
