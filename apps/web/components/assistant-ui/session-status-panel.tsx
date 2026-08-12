"use client";

import { type EnvironmentComponent, type EnvironmentStatus } from "@/app/runtime-provider";
import {
  CheckCircle2 as CheckCircle,
  Plug as PlugsConnected,
  Sparkles as Sparkle,
  Loader2 as SpinnerGap,
  AlertCircle as WarningCircle,
} from "lucide-react";
import { Activity, ChevronRight, FileText, X } from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { useState, type ReactNode } from "react";

export function SessionStatusButton({
  status,
  onClick,
}: {
  status: EnvironmentStatus;
  onClick: () => void;
}) {
  const t = useTranslations("chat.sessionStatus");
  const summary = environmentSummary(status, t);

  return (
    <button
      type="button"
      title={summary}
      aria-label={t("open", { summary })}
      onClick={onClick}
      className="relative inline-flex size-8 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
    >
      <Activity className="size-4" />
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
  const t = useTranslations("chat.sessionStatus");
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
    connected > 0 ? t("readyCount", { count: connected }) : "",
    configured > 0 ? t("configuredCount", { count: configured }) : "",
    connecting > 0 ? t("connectingCount", { count: connecting }) : "",
    unavailable > 0 ? t("unavailableCount", { count: unavailable }) : "",
  ].filter(Boolean);

  return (
    <div className="flex max-h-[inherit] flex-col font-sans">
      <header className="flex min-h-14 items-center gap-3 px-4">
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium text-foreground">{t("title")}</div>
          <div className="truncate text-xs text-muted">{environmentSummary(status, t)}</div>
        </div>
        {artifactName ? (
          <button
            type="button"
            title={t("openArtifact", { name: artifactName })}
            aria-label={t("openArtifact", { name: artifactName })}
            onClick={onOpenArtifact}
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
          >
            <FileText className="size-4" />
          </button>
        ) : null}
        <button
          type="button"
          title={t("close")}
          aria-label={t("close")}
          onClick={onClose}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
        >
          <X className="size-4" />
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-5">
        {status.components.length === 0 && status.phase !== "ready" ? (
          <div className="flex items-start gap-3 text-sm text-muted">
            <EnvironmentPhaseIcon status={status} className="mt-0.5 size-4 shrink-0" />
            <div>
              <p className="font-medium text-foreground">
                {status.phase === "preparing"
                  ? t("preparing")
                  : status.phase === "degraded"
                    ? t("unavailable")
                    : t("ready")}
              </p>
              <p className="mt-1 text-xs leading-5">
                {status.phase === "preparing"
                  ? t("preparingDescription")
                  : status.phase === "degraded"
                    ? t("unavailableDescription")
                    : t("emptyDescription")}
              </p>
            </div>
          </div>
        ) : (
          <div className="divide-y divide-border/60">
            <EnvironmentGroup
              title={t("skills")}
              summary={
                skills.length > 0 ? t("skillsLoaded", { count: skills.length }) : t("noneLoaded")
              }
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
                <EnvironmentEmptyState>{t("noSkills")}</EnvironmentEmptyState>
              )}
            </EnvironmentGroup>

            <EnvironmentGroup
              title={t("mcpServers")}
              summary={statusCounts.length > 0 ? statusCounts.join(" · ") : t("noneEnabled")}
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
                <EnvironmentEmptyState>{t("noMcp")}</EnvironmentEmptyState>
              )}
            </EnvironmentGroup>
          </div>
        )}
      </div>

      {unavailable > 0 ? (
        <div className="px-4 py-3">
          <Link
            href="/mcps"
            className="text-xs font-medium text-accent transition-colors hover:text-accent/80 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
          >
            {t("openMcpSettings")}
          </Link>
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
          <span className="text-[13px] font-semibold tracking-wide text-foreground">{title}</span>
        </span>
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate text-xs text-muted/80">{summary}</span>
          <ChevronRight
            className={`size-4 shrink-0 text-muted/70 transition-transform ${open ? "rotate-90" : ""}`}
          />
        </span>
      </button>
      {open ? <div className="pb-1">{children}</div> : null}
    </section>
  );
}

function EnvironmentEmptyState({ children }: { children: ReactNode }) {
  return <p className="px-1 pb-2 text-xs leading-5 text-muted">{children}</p>;
}

function EnvironmentComponentRow({ component }: { component: EnvironmentComponent }) {
  const t = useTranslations("chat.sessionStatus");
  return (
    <div className="flex min-h-[42px] items-center gap-3 rounded-xl px-1 py-1.5 transition-colors hover:bg-surface-secondary/40">
      <span className="grid size-6 shrink-0 place-items-center text-muted">
        <ComponentStatusIcon component={component} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-3">
          <p className="truncate text-sm font-normal text-foreground">{component.label}</p>
          <span className="shrink-0 text-[11px] text-muted">
            {componentStatusLabel(component, t)}
          </span>
        </div>
        {component.error ? (
          <p className="mt-1 break-words text-xs leading-5 text-amber-700">{component.error}</p>
        ) : component.kind === "skill" && component.version ? (
          <p className="mt-1 text-xs text-muted">{t("version", { version: component.version })}</p>
        ) : component.kind === "mcp" && component.status === "connected" ? (
          <p className="mt-1 text-xs text-muted">
            {component.toolCount > 0
              ? t("toolsAvailable", { count: component.toolCount })
              : t("connectionVerified")}
          </p>
        ) : component.kind === "mcp" && component.status === "configured" ? (
          <p className="mt-1 text-xs text-muted">{t("verifyOnUse")}</p>
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
    return <PlugsConnected className="size-4 text-muted" />;
  }
  return <WarningCircle className="size-4 text-amber-600" />;
}

type SessionStatusTranslations = ReturnType<typeof useTranslations<"chat.sessionStatus">>;

function environmentSummary(status: EnvironmentStatus, t: SessionStatusTranslations): string {
  const skills = status.components.filter((component) => component.kind === "skill");
  const mcps = status.components.filter((component) => component.kind === "mcp");
  const unavailable = mcps.filter((component) =>
    ["failed", "needs-auth", "timeout", "unavailable"].includes(component.status),
  ).length;
  if (status.phase === "preparing") return t("preparingSummary");
  const parts = skills.length > 0 ? [t("summarySkills", { count: skills.length })] : [];
  if (unavailable > 0) {
    parts.push(t("summaryMcpUnavailable", { count: unavailable }));
    return parts.join(" · ");
  }
  const connected = mcps.filter((component) => component.status === "connected").length;
  if (connected > 0) parts.push(t("summaryMcpReady", { count: connected }));
  const configured = mcps.filter((component) => component.status === "configured").length;
  if (configured > 0) parts.push(t("summaryMcpConfigured", { count: configured }));
  return parts.length > 0 ? parts.join(" · ") : t("ready");
}

function componentStatusLabel(
  component: EnvironmentComponent,
  t: SessionStatusTranslations,
): string {
  return t(`status.${component.status}`);
}

function environmentDotClass(phase: EnvironmentStatus["phase"]): string {
  if (phase === "preparing") return "bg-sky-500";
  if (phase === "degraded") return "bg-amber-500";
  return "bg-emerald-500";
}
