"use client";

import { ChevronsRight } from "@gravity-ui/icons";
import { Button, Tooltip } from "@heroui/react";
import { AppLayout } from "@heroui-pro/react/app-layout";
import { usePathname, useRouter } from "next/navigation";
import { type CSSProperties, type ReactNode, useCallback, useEffect, useState } from "react";

import { CocolaRuntimeProvider } from "@/app/runtime-provider";
import { CommandPalette } from "@/components/assistant-ui/command-palette";
import { HeroUIWorkspaceSidebar } from "@/components/assistant-ui/heroui-workspace-sidebar";
import { WorkspaceThemeToggle } from "@/components/assistant-ui/workspace-theme-toggle";
import { WorkspaceUnsavedChangesProvider } from "@/components/assistant-ui/workspace-unsaved-changes";
import { useWorkspaceUnsavedChanges } from "@/components/assistant-ui/workspace-unsaved-changes";
import { WorkspaceToastProvider } from "@/components/assistant-ui/workspace-toast";

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

const PAGE_LABELS: Record<string, string> = {
  "/agents": "Agents",
  "/connectors": "Connectors",
  "/folders": "Folders",
  "/mcps": "MCP",
  "/profile": "Profile",
  "/projects": "Projects",
  "/skills": "Skills",
  "/tasks": "Tasks",
  "/wiki": "Wiki",
};

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

function workspaceLabel(pathname: string) {
  const basePath = Object.keys(PAGE_LABELS).find(
    (path) => pathname === path || pathname.startsWith(`${path}/`),
  );
  return basePath ? (PAGE_LABELS[basePath] ?? "Workspace") : "New chat";
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
  const router = useRouter();
  const { runWithNavigationGuard } = useWorkspaceUnsavedChanges();
  const [immersive, setImmersive] = useState(false);
  const [peeked, setPeeked] = useState(false);

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
          <WorkspaceTopbar
            immersive={immersive}
            label={workspaceLabel(pathname)}
            pathname={pathname}
            onExitImmersive={() => updateImmersive(false)}
          />
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
  immersive,
  label,
  pathname,
  onExitImmersive,
}: {
  immersive: boolean;
  label: string;
  pathname: string;
  onExitImmersive: () => void;
}) {
  const isChat = pathname === "/";
  return (
    <div className="mx-auto flex h-14 w-full max-w-7xl items-center gap-3 px-4 sm:px-6 lg:px-8">
      {immersive ? (
        <Tooltip delay={0}>
          <Button
            isIconOnly
            aria-label="Exit immersive mode"
            aria-pressed="true"
            size="sm"
            variant="ghost"
            onPress={onExitImmersive}
          >
            <ChevronsRight className="size-4" />
          </Button>
          <Tooltip.Content>Exit immersive mode · Esc</Tooltip.Content>
        </Tooltip>
      ) : (
        <AppLayout.MenuToggle />
      )}
      {isChat ? (
        <div className="min-w-0 flex-1" />
      ) : (
        <div className="min-w-0 flex-1">
          <p className="text-accent truncate text-[11px] font-semibold tracking-[0.14em] uppercase">
            Agent workspace
          </p>
          <p className="text-foreground truncate text-sm font-medium">{label}</p>
        </div>
      )}
      <WorkspaceThemeToggle />
    </div>
  );
}
