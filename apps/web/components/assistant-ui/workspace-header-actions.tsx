"use client";

import { Button, Tooltip } from "@heroui/react";
import { useTranslations } from "next-intl";

import { WorkspaceThemeToggle } from "@/components/assistant-ui/workspace-theme-toggle";
import { LanguageMenu } from "@/components/i18n/language-menu";

const COCOLA_GITHUB_URL = "https://github.com/sakurs2/cocola";
export function WorkspaceHeaderActions() {
  const t = useTranslations("common");
  const githubLabel = t("githubPage");

  return (
    <div className="flex shrink-0 items-center gap-1">
      <LanguageMenu />
      <Tooltip delay={0}>
        <Button
          isIconOnly
          aria-label={githubLabel}
          className="shrink-0"
          size="sm"
          variant="ghost"
          onPress={() => window.open(COCOLA_GITHUB_URL, "_blank", "noopener,noreferrer")}
        >
          <GitHubIcon className="size-4" />
        </Button>
        <Tooltip.Content placement="bottom end">{githubLabel}</Tooltip.Content>
      </Tooltip>
      <WorkspaceThemeToggle />
    </div>
  );
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg aria-hidden="true" className={className} fill="currentColor" viewBox="0 0 16 16">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82A7.68 7.68 0 0 1 8 3.75c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}
