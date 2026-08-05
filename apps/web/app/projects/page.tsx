"use client";

import { Button, Card, Chip } from "@heroui/react";
import { EmptyState } from "@heroui-pro/react/empty-state";
import { Segment } from "@heroui-pro/react/segment";
import { Folder, FolderOpen, GitBranch, GitFork, HardDrive, Loader2, Plus } from "lucide-react";
import { useMemo, useState } from "react";
import type { ProjectSummary } from "@/app/runtime-provider";
import { useCocola } from "@/app/runtime-provider";
import {
  WorkspaceCatalogCard,
  WorkspacePageAction,
  WorkspacePageFrame,
  WorkspacePageHeader,
  WorkspaceSearch,
  WorkspaceSectionHeader,
} from "@/components/heroui-workspace/workspace-ui";

type ProviderFilter = "all" | "github" | "local";

const STATUS_META: Record<
  ProjectSummary["status"],
  { color: "success" | "warning" | "danger" | "default"; label: string }
> = {
  ready: { color: "success", label: "Ready" },
  provisioning: { color: "warning", label: "Provisioning" },
  failed: { color: "danger", label: "Failed" },
  archived: { color: "default", label: "Archived" },
};

function sourceLabel(project: ProjectSummary) {
  if (project.repository_provider === "github") {
    return `${project.repository_owner}/${project.repository_name}`;
  }
  return "Local workspace";
}

function relativeTime(iso: string) {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "recently";
  const minutes = Math.max(0, Math.round((Date.now() - then) / 60_000));
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return days < 30 ? `${days}d ago` : new Date(then).toLocaleDateString();
}

export default function ProjectsPage() {
  const { projects, projectsLoaded } = useCocola();
  const [query, setQuery] = useState("");
  const [provider, setProvider] = useState<ProviderFilter>("all");

  const filtered = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return [...projects]
      .sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at))
      .filter((project) => {
        if (provider !== "all" && project.repository_provider !== provider) return false;
        if (!normalizedQuery) return true;
        return `${project.name} ${project.description} ${sourceLabel(project)}`
          .toLowerCase()
          .includes(normalizedQuery);
      });
  }, [projects, provider, query]);

  return (
    <WorkspacePageFrame>
      <WorkspacePageHeader
        action={
          <WorkspacePageAction href="/projects/new">
            <Plus className="size-4" />
            New project
          </WorkspacePageAction>
        }
        description="Long-lived workspaces backed by local or connected repositories."
        icon={<Folder className="size-5" />}
        title="Projects"
      />

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <WorkspaceSearch placeholder="Search projects" value={query} onChange={setQuery} />
        <Segment
          aria-label="Project provider"
          className="sm:ml-auto"
          selectedKey={provider}
          size="sm"
          onSelectionChange={(key) => setProvider(String(key) as ProviderFilter)}
        >
          <Segment.Item id="all">All</Segment.Item>
          <Segment.Item id="github">GitHub</Segment.Item>
          <Segment.Item id="local">Local</Segment.Item>
        </Segment>
      </div>

      <WorkspaceSectionHeader
        description={`${filtered.length} project${filtered.length === 1 ? "" : "s"}`}
        title="All projects"
      />

      {!projectsLoaded ? (
        <div className="grid min-h-48 place-items-center">
          <Loader2 className="text-muted size-5 animate-spin" />
        </div>
      ) : filtered.length ? (
        <section
          aria-label="Projects"
          className="cocola-web-catalog-grid cocola-web-project-grid grid items-stretch gap-3 md:grid-cols-2 xl:grid-cols-3"
        >
          {filtered.map((project) => {
            const status = STATUS_META[project.status];
            const isGithub = project.repository_provider === "github";
            return (
              <WorkspaceCatalogCard
                key={project.id}
                description={project.description || "No description"}
                footerLabel="Open project"
                footerMeta={`Updated ${relativeTime(project.updated_at)}`}
                href={`/projects/${encodeURIComponent(project.id)}`}
                icon={isGithub ? <GitFork className="size-5" /> : <FolderOpen className="size-5" />}
                iconClassName={
                  isGithub
                    ? "bg-zinc-950 text-white dark:bg-white dark:text-zinc-950"
                    : "bg-indigo-500/15 text-indigo-600 dark:text-indigo-300"
                }
                metadata={
                  <>
                    <Chip size="sm" variant="soft">
                      {isGithub ? (
                        <GitFork className="size-3.5" />
                      ) : (
                        <HardDrive className="size-3.5" />
                      )}
                      {isGithub ? sourceLabel(project) : "Local"}
                    </Chip>
                    {project.default_branch ? (
                      <Chip size="sm" variant="soft">
                        <GitBranch className="size-3.5" />
                        {project.default_branch}
                      </Chip>
                    ) : null}
                  </>
                }
                status={
                  <Chip color={status.color} size="sm" variant="soft">
                    {status.label}
                  </Chip>
                }
                title={project.name}
              />
            );
          })}
        </section>
      ) : (
        <Card className="p-5">
          <EmptyState size="sm">
            <EmptyState.Header>
              <EmptyState.Media variant="icon">
                <FolderOpen className="text-indigo-500" />
              </EmptyState.Media>
              <EmptyState.Title>
                {projects.length ? "No projects found" : "No projects yet"}
              </EmptyState.Title>
              <EmptyState.Description>
                {projects.length
                  ? "Try another search or provider."
                  : "Create a local workspace or connect a GitHub repository."}
              </EmptyState.Description>
            </EmptyState.Header>
            {projects.length ? (
              <EmptyState.Content>
                <Button
                  size="sm"
                  variant="outline"
                  onPress={() => {
                    setQuery("");
                    setProvider("all");
                  }}
                >
                  Clear filters
                </Button>
              </EmptyState.Content>
            ) : null}
          </EmptyState>
        </Card>
      )}
    </WorkspacePageFrame>
  );
}
