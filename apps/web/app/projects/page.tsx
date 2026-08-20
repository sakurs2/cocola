"use client";

import { Button, Card, Chip } from "@heroui/react";
import { EmptyState } from "@cocola/ui-compat/empty-state";
import { Segment } from "@cocola/ui-compat/segment";
import { Folder, FolderOpen, GitBranch, GitFork, HardDrive, Loader2, Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { useFormatter, useTranslations } from "next-intl";
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
  { color: "success" | "warning" | "danger" | "default" }
> = {
  ready: { color: "success" },
  provisioning: { color: "warning" },
  failed: { color: "danger" },
  archiving: { color: "warning" },
  archive_failed: { color: "danger" },
  archived: { color: "default" },
};

function sourceLabel(project: ProjectSummary) {
  if (project.repository_provider === "github") {
    return `${project.repository_owner}/${project.repository_name}`;
  }
  return "Cocola repository";
}

export default function ProjectsPage() {
  const t = useTranslations("projects.list");
  const format = useFormatter();
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
            {t("new")}
          </WorkspacePageAction>
        }
        description={t("description")}
        icon={<Folder className="size-5" />}
        title={t("title")}
      />

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <WorkspaceSearch placeholder={t("search")} value={query} onChange={setQuery} />
        <Segment
          aria-label={t("provider")}
          className="sm:ml-auto"
          selectedKey={provider}
          size="sm"
          onSelectionChange={(key) => setProvider(String(key) as ProviderFilter)}
        >
          <Segment.Item id="all">{t("all")}</Segment.Item>
          <Segment.Item id="github">GitHub</Segment.Item>
          <Segment.Item id="local">{t("local")}</Segment.Item>
        </Segment>
      </div>

      <WorkspaceSectionHeader
        description={t("count", { count: filtered.length })}
        title={t("allProjects")}
      />

      {!projectsLoaded ? (
        <div className="grid min-h-48 place-items-center">
          <Loader2 className="text-muted size-5 animate-spin" />
        </div>
      ) : filtered.length ? (
        <section
          aria-label={t("regionLabel")}
          className="cocola-web-catalog-grid cocola-web-project-grid cocola-resource-card-grid"
        >
          {filtered.map((project) => {
            const status = STATUS_META[project.status];
            const isGithub = project.repository_provider === "github";
            return (
              <WorkspaceCatalogCard
                key={project.id}
                description={project.description || t("noDescription")}
                footerLabel={t("open")}
                footerMeta={t("updated", {
                  time: formatProjectTime(project.updated_at, format, t("recently")),
                })}
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
                      {isGithub ? sourceLabel(project) : t("local")}
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
                    {t(`status.${project.status}`)}
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
              <EmptyState.Title>{projects.length ? t("noResults") : t("empty")}</EmptyState.Title>
              <EmptyState.Description>
                {projects.length ? t("noResultsDescription") : t("emptyDescription")}
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
                  {t("clearFilters")}
                </Button>
              </EmptyState.Content>
            ) : null}
          </EmptyState>
        </Card>
      )}
    </WorkspacePageFrame>
  );
}

function formatProjectTime(iso: string, format: ReturnType<typeof useFormatter>, fallback: string) {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return fallback;
  const minutes = Math.max(0, Math.round((Date.now() - then) / 60_000));
  if (minutes < 60) return format.relativeTime(new Date(then), { unit: "minute" });
  const hours = Math.round(minutes / 60);
  if (hours < 24) return format.relativeTime(new Date(then), { unit: "hour" });
  const days = Math.round(hours / 24);
  return days < 30
    ? format.relativeTime(new Date(then), { unit: "day" })
    : format.dateTime(new Date(then), { dateStyle: "medium" });
}
