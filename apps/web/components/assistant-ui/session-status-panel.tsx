"use client";

import { type EnvironmentComponent, type EnvironmentStatus } from "@/app/runtime-provider";
import {
  CheckCircle2 as CheckCircle,
  Plug as PlugsConnected,
  Sparkles as Sparkle,
  Loader2 as SpinnerGap,
  AlertCircle as WarningCircle,
} from "lucide-react";
import { ChevronRight, FileText, Info, X } from "lucide-react";
import { useState, type ReactNode } from "react";

export function SessionStatusButton({
  status,
  onClick,
}: {
  status: EnvironmentStatus;
  onClick: () => void;
}) {
  const summary = environmentSummary(status);

  return (
    <button
      type="button"
      title={summary}
      aria-label={`Open session status: ${summary}`}
      onClick={onClick}
      className="relative inline-flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
    >
      <Info className="size-4" />
      <span
        className={`absolute right-1 top-1 size-1.5 rounded-full ${environmentDotClass(status.phase)}`}
      />
    </button>
  );
}

export function SessionStatusPanel({
  status,
  artifactName,
  onOpenArtifact,
  onClose,
}: {
  status: EnvironmentStatus;
  artifactName?: string;
  onOpenArtifact: () => void;
  onClose: () => void;
}) {
  const [skillsOpen, setSkillsOpen] = useState(false);
  const [mcpsOpen, setMcpsOpen] = useState(false);
  const skills = status.components.filter((component) => component.kind === "skill");
  const mcps = status.components.filter((component) => component.kind === "mcp");
  const connected = mcps.filter((component) => component.status === "connected").length;
  const configured = mcps.filter((component) => component.status === "configured").length;
  const connecting = mcps.filter((component) => component.status === "pending").length;
  const unavailable = mcps.filter((component) =>
    ["failed", "needs-auth", "timeout", "unavailable"].includes(component.status),
  ).length;
  const statusCounts = [
    connected > 0 ? `${connected} ready` : "",
    configured > 0 ? `${configured} configured` : "",
    connecting > 0 ? `${connecting} connecting` : "",
    unavailable > 0 ? `${unavailable} unavailable` : "",
  ].filter(Boolean);

  return (
    <div className="flex h-full flex-col font-system">
      <header className="flex min-h-14 items-center gap-3 px-4">
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium text-foreground">Session status</div>
          <div className="truncate text-xs text-muted-foreground">{environmentSummary(status)}</div>
        </div>
        {artifactName ? (
          <button
            type="button"
            title={`Open ${artifactName}`}
            aria-label={`Open artifact ${artifactName}`}
            onClick={onOpenArtifact}
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            <FileText className="size-4" />
          </button>
        ) : null}
        <button
          type="button"
          title="Close status"
          aria-label="Close session status"
          onClick={onClose}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <X className="size-4" />
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-auto px-4 pb-5">
        {status.components.length === 0 && status.phase !== "ready" ? (
          <div className="flex items-start gap-3 text-sm text-muted-foreground">
            <EnvironmentPhaseIcon status={status} className="mt-0.5 size-4 shrink-0" />
            <div>
              <p className="font-medium text-foreground">
                {status.phase === "preparing"
                  ? "Preparing environment"
                  : status.phase === "degraded"
                    ? "Environment unavailable"
                    : "Environment ready"}
              </p>
              <p className="mt-1 text-xs leading-5">
                {status.phase === "preparing"
                  ? "Checking the connections available to this turn."
                  : status.phase === "degraded"
                    ? "The environment check did not complete for this turn."
                    : "No environment capabilities were reported for this session."}
              </p>
            </div>
          </div>
        ) : (
          <div className="divide-y divide-border/60">
            <EnvironmentGroup
              title="Skills"
              summary={skills.length > 0 ? `${skills.length} loaded` : "None loaded"}
              icon={<Sparkle className="size-4 text-violet-500" />}
              open={skillsOpen}
              onToggle={() => setSkillsOpen((open) => !open)}
            >
              {skills.length > 0 ? (
                skills.map((component) => (
                  <EnvironmentComponentRow
                    key={`${component.kind}:${component.id}`}
                    component={component}
                  />
                ))
              ) : (
                <EnvironmentEmptyState>
                  No skills are loaded for this session.
                </EnvironmentEmptyState>
              )}
            </EnvironmentGroup>

            <EnvironmentGroup
              title="MCP servers"
              summary={statusCounts.length > 0 ? statusCounts.join(" · ") : "None enabled"}
              icon={<PlugsConnected className="size-4 text-sky-500" />}
              open={mcpsOpen}
              onToggle={() => setMcpsOpen((open) => !open)}
            >
              {mcps.length > 0 ? (
                mcps.map((component) => (
                  <EnvironmentComponentRow
                    key={`${component.kind}:${component.id}`}
                    component={component}
                  />
                ))
              ) : (
                <EnvironmentEmptyState>
                  No MCP servers are enabled for this session.
                </EnvironmentEmptyState>
              )}
            </EnvironmentGroup>
          </div>
        )}
      </div>

      {unavailable > 0 ? (
        <div className="px-4 py-3">
          <a
            href="/mcps"
            className="text-xs font-medium text-primary transition-colors hover:text-primary/80 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            Open MCP settings
          </a>
        </div>
      ) : null}
    </div>
  );
}

function EnvironmentGroup({
  title,
  summary,
  icon,
  open,
  onToggle,
  children,
}: {
  title: string;
  summary: string;
  icon: ReactNode;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  return (
    <section className="py-1">
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full items-center justify-between gap-3 py-2 text-left focus-visible:outline-none"
      >
        <span className="flex min-w-0 items-center gap-2">
          <span className="shrink-0">{icon}</span>
          <span className="text-[13px] font-semibold tracking-wide text-foreground">
            {title}
          </span>
        </span>
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate text-xs text-muted-foreground/80">{summary}</span>
          <ChevronRight
            className={`size-4 shrink-0 text-muted-foreground/70 transition-transform ${open ? "rotate-90" : ""}`}
          />
        </span>
      </button>
      {open ? <div className="pb-1">{children}</div> : null}
    </section>
  );
}

function EnvironmentEmptyState({ children }: { children: ReactNode }) {
  return <p className="px-1 pb-2 text-xs leading-5 text-muted-foreground">{children}</p>;
}

function EnvironmentComponentRow({ component }: { component: EnvironmentComponent }) {
  return (
    <div className="flex min-h-[42px] items-center gap-3 rounded-xl px-1 py-1.5 transition-colors hover:bg-muted/40">
      <span className="grid size-6 shrink-0 place-items-center text-muted-foreground">
        <ComponentStatusIcon component={component} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-3">
          <p className="truncate text-sm font-normal text-foreground">{component.label}</p>
          <span className="shrink-0 text-[11px] text-muted-foreground">
            {componentStatusLabel(component)}
          </span>
        </div>
        {component.error ? (
          <p className="mt-1 break-words text-xs leading-5 text-amber-700">{component.error}</p>
        ) : component.kind === "skill" && component.version ? (
          <p className="mt-1 text-xs text-muted-foreground">Version {component.version}</p>
        ) : component.kind === "mcp" && component.status === "connected" ? (
          <p className="mt-1 text-xs text-muted-foreground">
            {component.toolCount > 0
              ? `${component.toolCount} tool${component.toolCount === 1 ? "" : "s"} available`
              : "Connection verified"}
          </p>
        ) : component.kind === "mcp" && component.status === "configured" ? (
          <p className="mt-1 text-xs text-muted-foreground">
            Connection will be verified on first use
          </p>
        ) : null}
      </div>
    </div>
  );
}

function EnvironmentPhaseIcon({
  status,
  className,
}: {
  status: EnvironmentStatus;
  className?: string;
}) {
  if (status.phase === "preparing") {
    return <SpinnerGap className={`${className ?? ""} animate-spin text-sky-600`} />;
  }
  if (status.phase === "degraded") {
    return <WarningCircle className={`${className ?? ""} text-amber-600`} />;
  }
  return <CheckCircle className={`${className ?? ""} text-emerald-600`} />;
}

function ComponentStatusIcon({ component }: { component: EnvironmentComponent }) {
  if (component.status === "pending") {
    return <SpinnerGap className="size-4 animate-spin text-sky-600" />;
  }
  if (component.status === "connected" || component.status === "loaded") {
    return <CheckCircle className="size-4 text-emerald-600" />;
  }
  if (component.status === "configured") {
    return <PlugsConnected className="size-4 text-muted-foreground" />;
  }
  return <WarningCircle className="size-4 text-amber-600" />;
}

function environmentSummary(status: EnvironmentStatus): string {
  const skills = status.components.filter((component) => component.kind === "skill");
  const mcps = status.components.filter((component) => component.kind === "mcp");
  const unavailable = mcps.filter((component) =>
    ["failed", "needs-auth", "timeout", "unavailable"].includes(component.status),
  ).length;
  if (status.phase === "preparing") return "Preparing environment…";
  const parts =
    skills.length > 0 ? [`${skills.length} skill${skills.length === 1 ? "" : "s"}`] : [];
  if (unavailable > 0) {
    parts.push(`${unavailable} MCP unavailable`);
    return parts.join(" · ");
  }
  const connected = mcps.filter((component) => component.status === "connected").length;
  if (connected > 0) parts.push(`${connected} MCP ready`);
  const configured = mcps.filter((component) => component.status === "configured").length;
  if (configured > 0) parts.push(`${configured} MCP configured`);
  return parts.length > 0 ? parts.join(" · ") : "Environment ready";
}

function componentStatusLabel(component: EnvironmentComponent): string {
  switch (component.status) {
    case "loaded":
      return "Loaded";
    case "connected":
      return "Connected";
    case "configured":
      return "Configured";
    case "needs-auth":
      return "Needs auth";
    case "timeout":
      return "Timed out";
    case "unavailable":
      return "Unavailable";
    case "disabled":
      return "Disabled";
    case "failed":
      return "Failed";
    default:
      return "Connecting";
  }
}

function environmentDotClass(phase: EnvironmentStatus["phase"]): string {
  if (phase === "preparing") return "bg-sky-500";
  if (phase === "degraded") return "bg-amber-500";
  return "bg-emerald-500";
}
