"use client";

import { FolderGit2, GitBranch, GitFork, Loader2, Plus } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";
import { useCocola } from "@/app/runtime-provider";

export default function ProjectsPage() {
  const { projects, projectsLoaded } = useCocola();
  const sortedProjects = useMemo(
    () => [...projects].sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at)),
    [projects],
  );

  return (
    <div className="h-full overflow-y-auto px-5 py-8 sm:px-8 lg:px-12">
      <main className="mx-auto w-full max-w-4xl pb-16">
        <header className="flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-4">
            <div className="grid size-11 shrink-0 place-items-center rounded-2xl bg-indigo-500/10 text-indigo-600">
              <FolderGit2 className="size-5" />
            </div>
            <div>
              <h1 className="text-2xl font-semibold tracking-tight">Projects</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                Open a project or start a new workspace.
              </p>
            </div>
          </div>
          <Link
            href="/projects/new"
            className="inline-flex h-10 items-center justify-center gap-2 self-start rounded-xl bg-primary px-4 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 sm:self-auto"
          >
            <Plus className="size-4" />
            New project
          </Link>
        </header>

        <section className="mt-8">
          {!projectsLoaded ? (
            <div className="grid min-h-48 place-items-center">
              <Loader2 className="size-5 animate-spin text-muted-foreground" />
            </div>
          ) : sortedProjects.length === 0 ? (
            <div className="rounded-3xl border border-dashed border-border px-6 py-14 text-center">
              <FolderGit2 className="mx-auto size-8 text-muted-foreground/70" />
              <h2 className="mt-3 text-sm font-semibold">No projects yet</h2>
              <p className="mt-1 text-xs text-muted-foreground">
                Create a local workspace or connect a GitHub repository.
              </p>
            </div>
          ) : (
            <div className="grid gap-3 sm:grid-cols-2">
              {sortedProjects.map((project) => (
                <Link
                  key={project.id}
                  href={`/projects/${encodeURIComponent(project.id)}`}
                  className="group rounded-2xl border border-border bg-card p-5 transition-colors hover:border-indigo-500/30 hover:bg-muted/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                >
                  <div className="flex items-start gap-3.5">
                    <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-indigo-500/10 text-indigo-600">
                      <FolderGit2 className="size-5" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <h2 className="truncate font-semibold group-hover:text-indigo-600">
                        {project.name}
                      </h2>
                      <p className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground">
                        {project.description ||
                          (project.repository_provider === "github"
                            ? `${project.repository_owner}/${project.repository_name}`
                            : "Local workspace")}
                      </p>
                    </div>
                  </div>
                  <div className="mt-5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                    <span className="inline-flex items-center gap-1.5">
                      <GitFork className="size-3.5" />
                      {project.repository_provider === "github" ? "GitHub" : "Local"}
                    </span>
                    <span className="inline-flex items-center gap-1.5">
                      <GitBranch className="size-3.5" />
                      {project.default_branch || "Preparing"}
                    </span>
                    {project.status !== "ready" ? (
                      <span className="capitalize text-amber-600">{project.status}</span>
                    ) : null}
                  </div>
                </Link>
              ))}
            </div>
          )}
        </section>
      </main>
    </div>
  );
}
