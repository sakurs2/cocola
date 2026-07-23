"use client";

import { GitBranch, GitFork, HardDrive, Loader2, Plus, Search } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import type { ProjectSummary } from "@/app/runtime-provider";
import { useCocola } from "@/app/runtime-provider";
import { cn } from "@/lib/utils";

type ProviderFilter = "all" | "github" | "local";

const BRAND_GRADIENT = "linear-gradient(135deg,#2563eb,#7c3aed)";

const STATUS_META: Record<
  ProjectSummary["status"],
  { label: string; color: string }
> = {
  ready: { label: "Ready", color: "#16a34a" },
  provisioning: { label: "Provisioning", color: "#d97706" },
  failed: { label: "Failed", color: "#dc2626" },
  archived: { label: "Archived", color: "#6b7280" },
};

function initials(name: string) {
  const parts = name.replace(/[_/-]/g, " ").split(/\s+/).filter(Boolean);
  const raw =
    parts.length > 1 ? `${parts[0]![0]}${parts[1]![0]}` : name.slice(0, 2);
  return raw.toUpperCase();
}

function relativeTime(iso: string) {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const diff = Date.now() - then;
  const min = Math.round(diff / 60000);
  if (min < 1) return "just now";
  if (min < 60) return `${min}m ago`;
  const hr = Math.round(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.round(hr / 24);
  if (day < 30) return `${day}d ago`;
  const mon = Math.round(day / 30);
  if (mon < 12) return `${mon}mo ago`;
  return `${Math.round(mon / 12)}y ago`;
}

function sourceLabel(project: ProjectSummary) {
  if (project.repository_provider === "github") {
    return `${project.repository_owner}/${project.repository_name}`;
  }
  return "Local workspace";
}

export default function ProjectsPage() {
  const { projects, projectsLoaded } = useCocola();
  const [query, setQuery] = useState("");
  const [provider, setProvider] = useState<ProviderFilter>("all");

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return [...projects]
      .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
      .filter((p) => {
        if (provider === "github" && p.repository_provider !== "github") return false;
        if (provider === "local" && p.repository_provider !== "local") return false;
        if (!q) return true;
        return (
          p.name.toLowerCase().includes(q) ||
          p.description.toLowerCase().includes(q) ||
          sourceLabel(p).toLowerCase().includes(q)
        );
      });
  }, [projects, query, provider]);

  const filters: { key: ProviderFilter; label: string }[] = [
    { key: "all", label: "All" },
    { key: "github", label: "GitHub" },
    { key: "local", label: "Local" },
  ];

  // Shared column template so the header and every row line up perfectly.
  const gridCols =
    "grid-cols-[44px_1fr] sm:grid-cols-[44px_1fr_160px_120px_110px_84px]";

  return (
    <div className="h-full overflow-y-auto px-3 py-8 sm:px-5">
      <main className="mx-auto w-full max-w-5xl pb-16">
        {/* Editorial header with a strong baseline rule */}
        <header className="flex flex-wrap items-end justify-between gap-4 border-b-2 border-foreground pb-5">
          <div>
            <p className="font-mono text-[11px] uppercase tracking-[0.22em] text-muted-foreground">
              Workspaces
            </p>
            <h1 className="mt-1.5 text-3xl font-semibold tracking-tight">Projects</h1>
          </div>
          <Link
            href="/projects/new"
            className="inline-flex h-10 items-center gap-2 self-end rounded-full px-5 text-sm font-semibold text-white shadow-lg shadow-primary/20 transition-transform hover:-translate-y-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            style={{ background: BRAND_GRADIENT }}
          >
            <Plus className="size-4" />
            New project
          </Link>
        </header>

        {/* Search + provider filter rail */}
        <div className="mt-6 flex flex-wrap items-center gap-2">
          <label className="flex min-w-[220px] flex-1 items-center gap-2.5 rounded-xl border border-transparent bg-muted px-4 py-2.5 transition-colors focus-within:border-primary focus-within:bg-background">
            <Search className="size-4 text-muted-foreground" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search projects, repositories, or descriptions…"
              className="min-w-0 flex-1 border-0 bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground"
            />
          </label>
          {filters.map((f) => (
            <button
              key={f.key}
              type="button"
              onClick={() => setProvider(f.key)}
              aria-pressed={provider === f.key}
              className={cn(
                "rounded-xl border px-3.5 py-2.5 text-sm font-semibold transition-colors",
                provider === f.key
                  ? "border-primary bg-primary/10 text-primary"
                  : "border-border bg-background text-muted-foreground hover:text-foreground",
              )}
            >
              {f.label}
            </button>
          ))}
        </div>

        {/* List */}
        <section className="mt-6">
          {!projectsLoaded ? (
            <div className="grid min-h-48 place-items-center">
              <Loader2 className="size-5 animate-spin text-muted-foreground" />
            </div>
          ) : filtered.length === 0 ? (
            <div className="rounded-3xl border border-dashed border-border px-6 py-14 text-center">
              <h2 className="text-sm font-semibold">
                {projects.length === 0 ? "No projects yet" : "No matching projects"}
              </h2>
              <p className="mt-1 text-xs text-muted-foreground">
                {projects.length === 0
                  ? "Create a local workspace or connect a GitHub repository."
                  : "Try a different keyword or filter."}
              </p>
            </div>
          ) : (
            <>
              {/* Column header — labels live here once instead of on every row */}
              <div
                className={cn(
                  "hidden gap-4 px-0 pb-2 pt-1 font-mono text-[11px] uppercase tracking-[0.09em] text-muted-foreground sm:grid",
                  gridCols,
                )}
              >
                <span />
                <span>Project</span>
                <span>Source</span>
                <span>Branch</span>
                <span>Status</span>
                <span>Updated</span>
              </div>

              <div className="border-t border-border">
                {filtered.map((project) => {
                  const status = STATUS_META[project.status];
                  return (
                    <Link
                      key={project.id}
                      href={`/projects/${encodeURIComponent(project.id)}`}
                      className={cn(
                        "group relative grid items-center gap-4 border-b border-border py-3.5 pl-0 transition-[padding,background] hover:bg-muted hover:pl-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
                        gridCols,
                      )}
                    >
                      {/* gradient accent bar on hover */}
                      <span
                        className="pointer-events-none absolute inset-y-2.5 left-0 w-[3px] rounded-full opacity-0 transition-opacity group-hover:opacity-100"
                        style={{ background: BRAND_GRADIENT }}
                      />
                      {/* gradient monogram */}
                      <div
                        className="grid size-11 place-items-center rounded-xl text-base font-bold tracking-tight text-white shadow-[inset_0_-9px_18px_-12px_rgba(0,0,0,0.4)]"
                        style={{ background: BRAND_GRADIENT }}
                      >
                        {initials(project.name)}
                      </div>
                      {/* identity */}
                      <div className="min-w-0">
                        <div className="flex items-center gap-2.5">
                          <span className="truncate text-[15px] font-semibold tracking-tight group-hover:text-primary">
                            {project.name}
                          </span>
                          <span className="shrink-0 rounded-md border border-border px-1.5 py-px font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
                            {project.visibility}
                          </span>
                        </div>
                        <p
                          className={cn(
                            "mt-0.5 truncate text-[13px] text-muted-foreground",
                            project.description ? "" : "opacity-50",
                          )}
                        >
                          {project.description || "No description"}
                        </p>
                      </div>
                      {/* source */}
                      <div className="hidden min-w-0 items-center gap-1.5 text-[13px] sm:flex">
                        {project.repository_provider === "github" ? (
                          <GitFork className="size-3.5 shrink-0 text-muted-foreground" />
                        ) : (
                          <HardDrive className="size-3.5 shrink-0 text-muted-foreground" />
                        )}
                        <span className="truncate">{sourceLabel(project)}</span>
                      </div>
                      {/* branch */}
                      <div className="hidden min-w-0 items-center gap-1.5 text-[13px] sm:flex">
                        {project.default_branch ? (
                          <>
                            <GitBranch className="size-3.5 shrink-0 text-muted-foreground" />
                            <span className="truncate">{project.default_branch}</span>
                          </>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </div>
                      {/* status */}
                      <div
                        className="hidden items-center gap-1.5 text-[13px] font-semibold sm:flex"
                        style={{ color: status.color }}
                      >
                        <span
                          className="size-[7px] shrink-0 rounded-full"
                          style={{ background: status.color }}
                        />
                        {status.label}
                      </div>
                      {/* updated */}
                      <div className="hidden text-[13px] text-muted-foreground sm:block">
                        {relativeTime(project.updated_at)}
                      </div>
                    </Link>
                  );
                })}
              </div>
            </>
          )}
        </section>
      </main>
    </div>
  );
}
