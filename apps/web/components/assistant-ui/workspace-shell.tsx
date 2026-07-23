"use client";

import { type ReactNode, useCallback, useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import { PanelLeftOpen } from "lucide-react";
import { CocolaRuntimeProvider } from "@/app/runtime-provider";
import { AppSidebar } from "@/components/assistant-ui/app-sidebar";
import { CommandPalette } from "@/components/assistant-ui/command-palette";
import { WorkspaceToastProvider } from "@/components/assistant-ui/workspace-toast";
import { cn } from "@/lib/utils";

const IMMERSIVE_KEY = "cocola:immersive";

function isWorkspacePath(pathname: string | null) {
  return (
    pathname === "/" ||
    pathname === "/skills" ||
    pathname?.startsWith("/skills/") ||
    pathname === "/mcps" ||
    pathname?.startsWith("/mcps/") ||
    pathname === "/tasks" ||
    pathname?.startsWith("/tasks/") ||
    pathname === "/connectors" ||
    pathname?.startsWith("/connectors/") ||
    pathname === "/folders" ||
    pathname?.startsWith("/folders/") ||
    pathname === "/projects" ||
    pathname?.startsWith("/projects/")
  );
}

export function WorkspaceShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  // Immersive mode: sidebar folds away and content centers; hovering the left
  // edge slides the sidebar back over the content without shifting it.
  const [immersive, setImmersive] = useState(false);
  const [peek, setPeek] = useState(false);

  // Restore the persisted preference after mount (avoids hydration mismatch).
  useEffect(() => {
    try {
      setImmersive(window.localStorage.getItem(IMMERSIVE_KEY) === "1");
    } catch {
      /* ignore */
    }
  }, []);

  const toggleImmersive = useCallback(() => {
    setImmersive((prev) => {
      const next = !prev;
      try {
        window.localStorage.setItem(IMMERSIVE_KEY, next ? "1" : "0");
      } catch {
        /* ignore */
      }
      if (!next) setPeek(false);
      return next;
    });
  }, []);

  // Esc exits immersive mode.
  useEffect(() => {
    if (!immersive) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setPeek(false);
        setImmersive(false);
        try {
          window.localStorage.setItem(IMMERSIVE_KEY, "0");
        } catch {
          /* ignore */
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [immersive]);

  if (!isWorkspacePath(pathname)) {
    return <>{children}</>;
  }

  const sidebarVisible = !immersive || peek;
  const easing = "cubic-bezier(0.22,1,0.36,1)";

  return (
    <CocolaRuntimeProvider>
      <WorkspaceToastProvider>
        <div className="cocola-user-ui workspace-grain relative flex h-screen overflow-hidden bg-background text-foreground">
          {/* Sidebar spacer — collapses to 0 in immersive mode so the main
              content smoothly recenters. The actual panel is absolutely
              positioned on top so peeking never shifts the layout. */}
          <div
            className="relative h-full shrink-0 transition-[width] duration-500"
            style={{ width: immersive ? 0 : "17rem", transitionTimingFunction: easing }}
          >
            <div
              onMouseEnter={() => immersive && setPeek(true)}
              onMouseLeave={() => immersive && setPeek(false)}
              className={cn(
                "absolute left-0 top-0 z-40 h-full w-[17rem] transition-transform duration-500 will-change-transform",
                sidebarVisible ? "translate-x-0" : "-translate-x-full",
                immersive && peek ? "shadow-2xl shadow-black/25" : "",
              )}
              style={{ transitionTimingFunction: easing }}
            >
              <AppSidebar immersive={immersive} onToggleImmersive={toggleImmersive} />
            </div>
          </div>

          {/* Left-edge hover trigger — only in immersive mode. */}
          {immersive ? (
            <div
              aria-hidden
              onMouseEnter={() => setPeek(true)}
              className="fixed left-0 top-0 z-30 h-full w-3"
            />
          ) : null}

          {/* Floating toggle pinned to the same top-left coordinate as the
              sidebar's own toggle, shown only while the sidebar is hidden so the
              icon appears to stay in place. */}
          <button
            type="button"
            onClick={toggleImmersive}
            title="Exit immersive mode"
            aria-label="Exit immersive mode"
            aria-pressed={immersive}
            className={cn(
              "fixed left-3 top-4 z-50 grid size-8 place-items-center rounded-lg text-foreground/70 transition hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45",
              sidebarVisible ? "pointer-events-none opacity-0" : "opacity-100",
            )}
          >
            <PanelLeftOpen className="size-[18px]" />
          </button>

          <main className="min-w-0 flex-1 overflow-hidden bg-transparent">{children}</main>
          <CommandPalette />
        </div>
      </WorkspaceToastProvider>
    </CocolaRuntimeProvider>
  );
}
