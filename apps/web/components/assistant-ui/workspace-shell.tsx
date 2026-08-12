"use client";

import { ChevronsRight } from "@gravity-ui/icons";
import { Button, Tooltip } from "@heroui/react";
import { AppLayout } from "@cocola/ui-compat/app-layout";
import { usePathname, useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { type CSSProperties, type ReactNode, useCallback, useEffect, useState } from "react";

import { CocolaRuntimeProvider } from "@/app/runtime-provider";
import { CommandPalette } from "@/components/assistant-ui/command-palette";
import { HeroUIWorkspaceSidebar } from "@/components/assistant-ui/heroui-workspace-sidebar";
import { WorkspaceHeaderActions } from "@/components/assistant-ui/workspace-header-actions";
import { WorkspaceUnsavedChangesProvider } from "@/components/assistant-ui/workspace-unsaved-changes";
import { useWorkspaceUnsavedChanges } from "@/components/assistant-ui/workspace-unsaved-changes";
import { WorkspaceToastProvider } from "@/components/assistant-ui/workspace-toast";
import { isProjectTaskPath } from "@/lib/workspace-routes";

const IMMERSIVE_KEY = "cocola:immersive";

const WORKSPACE_PATHS = [
  "/",
  "/wiki",
  "/agents",
  "/skills",
  "/mcps",
  "/tasks",
  "/connectors",
  "/folders",
  "/projects",
  "/profile",
] as const;

const PAGE_LABEL_KEYS = {
  "/agents": "agents",
  "/connectors": "connectors",
  "/folders": "folders",
  "/mcps": "mcps",
  "/profile": "profile",
  "/projects": "projects",
  "/skills": "skills",
  "/tasks": "tasks",
  "/wiki": "wiki",
} as const;

const PAGE_ACCENTS: Record<string, { accent: string; foreground: string }> = {
  "/agents": { accent: "oklch(68% 0.14 215)", foreground: "var(--eclipse)" },
  "/connectors": { accent: "oklch(65% 0.17 160)", foreground: "var(--eclipse)" },
  "/folders": { accent: "oklch(76% 0.17 70)", foreground: "var(--eclipse)" },
  "/mcps": { accent: "oklch(67% 0.19 45)", foreground: "var(--snow)" },
  "/profile": { accent: "oklch(62.04% 0.195 253.83)", foreground: "var(--snow)" },
  "/projects": { accent: "oklch(56% 0.22 275)", foreground: "var(--snow)" },
  "/skills": { accent: "oklch(60% 0.22 292)", foreground: "var(--snow)" },
  "/tasks": { accent: "oklch(62.04% 0.195 253.83)", foreground: "var(--snow)" },
  "/wiki": { accent: "oklch(62.04% 0.195 253.83)", foreground: "var(--snow)" },
  "/": { accent: "oklch(62.04% 0.195 253.83)", foreground: "var(--snow)" },
};

function isWorkspacePath(pathname: string | null) {
  if (!pathname) return false;
  return WORKSPACE_PATHS.some((path) =>
    path === "/" ? pathname === "/" : pathname === path || pathname.startsWith(`${path}/`),
  );
}

function workspaceLabelKey(
  pathname: string,
): (typeof PAGE_LABEL_KEYS)[keyof typeof PAGE_LABEL_KEYS] | "newChat" {
  const basePath = Object.keys(PAGE_LABEL_KEYS).find(
    (path) => pathname === path || pathname.startsWith(`${path}/`),
  );
  return basePath ? PAGE_LABEL_KEYS[basePath as keyof typeof PAGE_LABEL_KEYS] : "newChat";
}

function workspaceTheme(pathname: string): CSSProperties {
  const basePath = Object.keys(PAGE_ACCENTS).find((path) =>
    path === "/" ? pathname === "/" : pathname === path || pathname.startsWith(`${path}/`),
  );
  const theme = PAGE_ACCENTS[basePath ?? "/"] ?? PAGE_ACCENTS["/"]!;
  return {
    "--accent": theme.accent,
    "--accent-foreground": theme.foreground,
    "--accent-hover": "color-mix(in oklab, var(--accent) 90%, var(--accent-foreground) 10%)",
    "--accent-soft": "color-mix(in oklab, var(--accent) 15%, transparent)",
    "--accent-soft-foreground": "var(--accent)",
    "--focus": "var(--accent)",
  } as CSSProperties;
}

export function WorkspaceShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();

  if (!isWorkspacePath(pathname)) return <>{children}</>;

  return (
    <WorkspaceUnsavedChangesProvider>
      <CocolaRuntimeProvider>
        <WorkspaceToastProvider>
          <HeroUIWorkspaceLayout pathname={pathname || "/"}>{children}</HeroUIWorkspaceLayout>
        </WorkspaceToastProvider>
      </CocolaRuntimeProvider>
    </WorkspaceUnsavedChangesProvider>
  );
}

function HeroUIWorkspaceLayout({ children, pathname }: { children: ReactNode; pathname: string }) {
  const t = useTranslations("navigation");
  const router = useRouter();
  const { runWithNavigationGuard } = useWorkspaceUnsavedChanges();
  const [immersive, setImmersive] = useState(false);
  const [peeked, setPeeked] = useState(false);
  const isChat = pathname === "/";
  const compactTopbar = isProjectTaskPath(pathname);

  useEffect(() => {
    try {
      setImmersive(window.localStorage.getItem(IMMERSIVE_KEY) === "1");
    } catch {
      // The current session can still use immersive mode without persistence.
    }
  }, []);

  const updateImmersive = useCallback((nextValue: boolean) => {
    setPeeked(false);
    setImmersive(nextValue);
    try {
      window.localStorage.setItem(IMMERSIVE_KEY, nextValue ? "1" : "0");
    } catch {
      // The current session still reflects the requested mode.
    }
  }, []);

  useEffect(() => {
    if (!immersive) return;
    const exitOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") updateImmersive(false);
    };
    window.addEventListener("keydown", exitOnEscape);
    return () => window.removeEventListener("keydown", exitOnEscape);
  }, [immersive, updateImmersive]);

  const navigate = useCallback(
    (href: string) => {
      runWithNavigationGuard(() => router.push(href));
    },
    [router, runWithNavigationGuard],
  );

  return (
    <>
      <AppLayout
        className={`cocola-user-ui cocola-web-shell h-svh ${immersive ? "cocola-web-immersive" : ""}`}
        navigate={navigate}
        navbar={
          isChat ? undefined : (
            <WorkspaceTopbar
              compact={compactTopbar}
              immersive={immersive}
              label={t(workspaceLabelKey(pathname))}
              pathname={pathname}
              onExitImmersive={() => updateImmersive(false)}
            />
          )
        }
        onSidebarOpenChange={(isOpen) => updateImmersive(!isOpen)}
        scrollMode="content"
        sidebar={
          <HeroUIWorkspaceSidebar
            immersive={immersive}
            onPeekChange={setPeeked}
            onToggleImmersive={() => updateImmersive(!immersive)}
          />
        }
        sidebarCollapsible="offcanvas"
        sidebarDefaultSize="17rem"
        sidebarOpen={!immersive || peeked}
        style={workspaceTheme(pathname)}
      >
        {children}
      </AppLayout>
      {isChat && immersive ? (
        <Tooltip delay={0}>
          <Button
            isIconOnly
            aria-label={t("exitImmersive")}
            aria-pressed="true"
            className="fixed left-1 top-2 z-50"
            size="sm"
            variant="ghost"
            onPress={() => updateImmersive(false)}
          >
            <ChevronsRight className="size-4" />
          </Button>
          <Tooltip.Content>{t("exitImmersiveHint")}</Tooltip.Content>
        </Tooltip>
      ) : null}
      {immersive && !peeked ? (
        <div
          aria-hidden="true"
          className="fixed inset-y-0 left-0 z-40 hidden w-3 md:block"
          onMouseEnter={() => setPeeked(true)}
        />
      ) : null}
      <CommandPalette />
    </>
  );
}

function WorkspaceTopbar({
  compact,
  immersive,
  label,
  pathname,
  onExitImmersive,
}: {
  compact: boolean;
  immersive: boolean;
  label: string;
  pathname: string;
  onExitImmersive: () => void;
}) {
  const t = useTranslations("navigation");
  const isChat = pathname === "/";
  return (
    <div
      className={`flex w-full items-center gap-3 ${immersive ? "px-4" : "mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"} ${compact ? "h-10" : "h-14"}`}
    >
      {immersive ? (
        <Tooltip delay={0}>
          <Button
            isIconOnly
            aria-label={t("exitImmersive")}
            aria-pressed="true"
            size="sm"
            variant="ghost"
            onPress={onExitImmersive}
          >
            <ChevronsRight className="size-4" />
          </Button>
          <Tooltip.Content>{t("exitImmersiveHint")}</Tooltip.Content>
        </Tooltip>
      ) : (
        <AppLayout.MenuToggle />
      )}
      {isChat ? (
        <div className="min-w-0 flex-1" />
      ) : (
        <div className="min-w-0 flex-1">
          {compact ? (
            <p className="text-foreground truncate text-sm font-medium">{label}</p>
          ) : (
            <>
              <p className="text-accent truncate text-[11px] font-semibold tracking-[0.14em] uppercase">
                {t("agentWorkspace")}
              </p>
              <p className="text-foreground truncate text-sm font-medium">{label}</p>
            </>
          )}
        </div>
      )}
      <WorkspaceHeaderActions />
    </div>
  );
}
