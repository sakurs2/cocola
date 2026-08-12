"use client";

import { Check, ChevronDown } from "lucide-react";
import { Dropdown } from "@heroui/react";
import { useTranslations } from "next-intl";
import { ModelIcon } from "@/components/ui/model-icon";
import type { AgentModelOption } from "@/lib/agents";

export function HeroUIAgentModelSelect({
  isDisabled = false,
  models,
  value,
  fallbackLabel,
  onChange,
}: {
  isDisabled?: boolean;
  models: AgentModelOption[];
  value: string;
  fallbackLabel?: string;
  onChange: (modelID: string) => void;
}) {
  const t = useTranslations("agents.modelSelect");
  const model = models.find((item) => item.id === value);

  return (
    <Dropdown>
      <Dropdown.Trigger
        aria-label={t("select")}
        className="cocola-web-select-trigger border-separator bg-default hover:bg-default-hover flex h-11 w-full min-w-0 items-center gap-3 rounded-2xl border px-3 text-left text-sm outline-none transition-colors disabled:cursor-not-allowed disabled:opacity-50"
        isDisabled={isDisabled}
      >
        <span className="flex min-w-0 flex-1 items-center gap-2.5">
          <ModelIcon className="size-5 shrink-0" icon={model?.icon} bare />
          <span className="min-w-0 truncate font-medium">
            {model?.alias ?? fallbackLabel ?? value}
          </span>
        </span>
        <ChevronDown className="text-muted size-4 shrink-0" />
      </Dropdown.Trigger>
      <Dropdown.Popover className="min-w-72" placement="bottom start">
        <Dropdown.Menu aria-label={t("models")} onAction={(key) => onChange(String(key))}>
          {models.map((item) => (
            <Dropdown.Item key={item.id} id={item.id} textValue={item.alias}>
              <span className="flex min-w-0 items-center gap-2.5">
                <ModelIcon className="size-5 shrink-0" icon={item.icon} bare />
                <span className="min-w-0 flex-1 truncate text-sm font-medium">{item.alias}</span>
                {item.id === value ? <Check className="text-accent size-4" /> : null}
              </span>
            </Dropdown.Item>
          ))}
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}
