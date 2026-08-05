"use client";

import { Dropdown } from "@heroui/react";
import { Check, ChevronDown } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export type SelectOption = { value: string; label: string; disabled?: boolean; icon?: ReactNode };

export function SelectControl({ value, onValueChange, options, id, disabled, ariaLabel, placeholder = "Select an option", className, contentClassName }: { value: string; onValueChange: (value: string) => void; options: readonly SelectOption[]; id?: string; disabled?: boolean; ariaLabel?: string; placeholder?: string; className?: string; contentClassName?: string }) {
  const selected = options.find((option) => option.value === value);
  return <Dropdown><Dropdown.Trigger id={id} aria-label={ariaLabel || placeholder} className={cn("cocola-web-select-trigger border-separator bg-default hover:bg-default-hover flex h-11 w-full min-w-0 items-center justify-between gap-2 rounded-2xl border px-3 text-sm outline-none transition-colors", className)} isDisabled={disabled}><span className="flex min-w-0 flex-1 items-center gap-2 text-left">{selected?.icon ? <span className="shrink-0">{selected.icon}</span> : null}<span className="truncate">{selected?.label || placeholder}</span></span><ChevronDown className="text-muted size-4 shrink-0" /></Dropdown.Trigger><Dropdown.Popover className={contentClassName} placement="bottom start"><Dropdown.Menu aria-label={ariaLabel || placeholder} onAction={(key) => onValueChange(String(key))}>{options.map((option) => <Dropdown.Item key={option.value} id={option.value} isDisabled={option.disabled} textValue={option.label}><span className="flex min-w-0 items-center gap-2">{option.icon ? <span className="shrink-0">{option.icon}</span> : null}<span className="min-w-0 flex-1 truncate">{option.label}</span>{option.value === value ? <Check className="text-accent size-4" /> : null}</span></Dropdown.Item>)}</Dropdown.Menu></Dropdown.Popover></Dropdown>;
}
