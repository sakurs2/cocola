"use client";

import { GitBranch, GitFork, HardDrive, Loader2, Plus, Search } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import type { ProjectSummary } from "@/app/runtime-provider";
import { useCocola } from "@/app/runtime-provider";
import { cn } from "@/lib/utils";

type ProviderFilter = "all" | "github" | "local";

const STATUS_META: Record<ProjectSummary["status"], { label: string; color: string }> = {
  ready: { label: "Ready", color: "#16a34a" },
  provisioning: { label: "Provisioning", color: "#d97706" },
  failed: { label: "Failed", color: "#dc2626" },
  archived: { label: "Archived", color: "#6b7280" },
};

function initials(name: string) {
  const parts = name.replace(/[_/-]/g, " ").split(/\s+/).filter(Boolean);
  const raw = parts.length > 1 ? `${parts[0]![0]}${parts[1]![0]}` : name.slice(0, 2);
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

  return (
    <main className="user-canvas user-page user-theme-indigo h-full min-w-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6 sm:py-7">
      <div className="mx-auto w-full max-w-7xl pb-16">
        {/* Header */}
        <header className="flex flex-wrap items-center gap-3.5">
          <div className="user-page-icon">
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M4 20V6a2 2 0 0 1 2-2h5l2 2h5a2 2 0 0 1 2 2v3" />
              <circle cx="12" cy="15" r="3" />
              <path d="M12 18v3M9.5 15H6M18 15h-3.5" />
            </svg>
          </div>
          <div className="min-w-0 flex-1">
            <div className="user-eyebrow">Workspaces</div>
            <h1 className="mt-1 text-[28px] font-semibold tracking-tight">Projects</h1>
          </div>
          <Link
            href="/projects/new"
            className="user-accent-btn inline-flex h-10 items-center gap-2 rounded-xl px-[18px] text-[13.5px] font-semibold"
          >
            <Plus className="size-4" />
            New project
          </Link>
        </header>

        {/* Search + provider filter rail */}
        <div className="mt-[22px] flex flex-wrap items-center gap-2.5">
          <label className="user-search-input flex min-w-[220px] flex-1 items-center gap-2.5 rounded-xl px-3.5 py-2.5">
            <Search className="size-4 text-[#94a3b8]" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search projects, repositories, or descriptions…"
              className="min-w-0 flex-1 border-0 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
          </label>
          {filters.map((f) => (
            <button
              key={f.key}
              type="button"
              onClick={() => setProvider(f.key)}
              aria-pressed={provider === f.key}
              className={cn(
                "user-tbtn px-3.5",
                provider === f.key ? "user-tbtn--fill" : "user-tbtn--ghost",
              )}
            >
              {f.label}
            </button>
          ))}
        </div>

        {/* Section title */}
        <div className="mb-3 mt-[22px] flex items-center gap-2">
          <span className="user-section-title">All projects</span>
          <span className="user-count-badge">{filtered.length}</span>
        </div>

        {/* List */}
        {!projectsLoaded ? (
          <div className="grid min-h-48 place-items-center">
            <Loader2 className="size-5 animate-spin text-muted-foreground" />
          </div>
        ) : filtered.length === 0 ? (
          <div className="user-empty">
            <h2 className="text-sm font-semibold">
              {projects.length === 0 ? "No projects yet" : "No matching projects"}
            </h2>
            <p className="text-xs text-muted-foreground">
              {projects.length === 0
                ? "Create a local workspace or connect a GitHub repository."
                : "Try a different keyword or filter."}
            </p>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {filtered.map((project) => {
              const status = STATUS_META[project.status];
              return (
                <Link
                  key={project.id}
                  href={`/projects/${encodeURIComponent(project.id)}`}
                  className="user-card user-card--hover group row gap-4"
                >
                  {/* monogram */}
                  <div className="proj-mono size-11 text-base">{initials(project.name)}</div>
                  {/* identity */}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="user-card-name truncate group-hover:text-[color:var(--page-accent)]">
                        {project.name}
                      </span>
                    </div>
                    <p
                      className={cn(
                        "user-card-desc mt-0.5 truncate",
                        project.description ? "" : "opacity-50",
                      )}
                    >
                      {project.description || "No description"}
                    </p>
                  </div>
                  {/* source */}
                  <div className="hidden w-[170px] shrink-0 items-center justify-center gap-1.5 text-[13px] text-muted-foreground sm:flex">
                    {project.repository_provider === "github" ? (
                      <GitFork className="size-3.5 shrink-0" />
                    ) : (
                      <HardDrive className="size-3.5 shrink-0" />
                    )}
                    <span className="truncate">{sourceLabel(project)}</span>
                  </div>
                  {/* branch */}
                  <div className="hidden w-[100px] shrink-0 items-center justify-center gap-1.5 text-[13px] text-muted-foreground sm:flex">
                    {project.default_branch ? (
                      <>
                        <GitBranch className="size-3.5 shrink-0" />
                        <span className="truncate">{project.default_branch}</span>
                      </>
                    ) : (
                      <span>—</span>
                    )}
                  </div>
                  {/* status */}
                  <span
                    className="hidden w-[116px] shrink-0 items-center justify-center gap-1.5 text-[13px] font-medium sm:inline-flex"
                    style={{ color: status.color }}
                  >
                    <span
                      className="size-1.5 shrink-0 rounded-full"
                      style={{ background: status.color }}
                    />
                    {status.label}
                  </span>
                  {/* updated */}
                  <span className="hidden w-16 shrink-0 text-center font-mono text-xs text-muted-foreground sm:block">
                    {relativeTime(project.updated_at)}
                  </span>
                </Link>
              );
            })}
          </div>
        )}
      </div>
    </main>
  );
}
