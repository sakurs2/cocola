"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { ChevronLeft, GitBranch } from "lucide-react";
import { Button, Chip } from "@heroui/react";
import { useCocola } from "@/app/runtime-provider";
import {
  ProjectBranchBadge,
  ProjectComposerBranchProvider,
} from "@/components/assistant-ui/project-branch-control";
import Home from "@/app/page";

type ProjectWorkspace = {
  branch_name: string;
  base_ref: string;
  base_sha: string;
};

export default function ProjectTaskPage() {
  const params = useParams<{ id: string; conversationId: string }>();
  const router = useRouter();
  const { activeSessionId, conversations, loadConversation, projects } = useCocola();
  const project = projects.find((item) => item.id === params.id);
  const [projectName, setProjectName] = useState("");
  const [workspace, setWorkspace] = useState<ProjectWorkspace | null>(null);
  const conversation = conversations.find((item) => item.id === params.conversationId);

  useEffect(() => {
    if (activeSessionId === params.conversationId) return;
    void loadConversation(params.conversationId);
  }, [activeSessionId, loadConversation, params.conversationId]);

  useEffect(() => {
    if (project?.name) {
      setProjectName(project.name);
      return;
    }
    void fetch(`/api/projects/${encodeURIComponent(params.id)}`, { cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) return;
        const value = (await response.json()) as { name?: string };
        if (value.name) setProjectName(value.name);
      })
      .catch(() => {});
  }, [params.id, project?.name]);

  useEffect(() => {
    let cancelled = false;
    setWorkspace(null);
    void fetch(`/api/conversations/${encodeURIComponent(params.conversationId)}/git/status`, {
      cache: "no-store",
    })
      .then(async (response) => {
        if (!response.ok) return;
        const body = (await response.json()) as { workspace?: ProjectWorkspace };
        if (!cancelled && body.workspace) setWorkspace(body.workspace);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [params.conversationId]);

  const fallbackBranch =
    project?.repository_provider === "local"
      ? "main"
      : `cocola/task-${params.conversationId.replaceAll("-", "").slice(0, 12)}`;
  const branchName = workspace?.branch_name || fallbackBranch;

  return (
    <div className="user-theme-indigo flex h-full min-h-0 flex-1 flex-col">
      <div className="border-separator bg-background flex h-12 shrink-0 items-center gap-2 border-b px-3 text-xs">
        <Button
          size="sm"
          variant="ghost"
          onPress={() => router.push(`/projects/${encodeURIComponent(params.id)}`)}
        >
          <ChevronLeft className="size-3.5" />
          {project?.name || projectName || "Project"}
        </Button>
        <span className="text-muted">/</span>
        <span className="max-w-64 truncate text-foreground">{conversation?.title || "Task"}</span>
        <Chip className="ml-auto" color="accent" size="sm" variant="soft">
          <GitBranch className="size-3.5" />
          {branchName}
        </Chip>
      </div>
      <div className="min-h-0 flex-1">
        <ProjectComposerBranchProvider
          control={
            <ProjectBranchBadge
              branch={branchName}
              baseRef={workspace?.base_ref}
              baseSHA={workspace?.base_sha}
            />
          }
        >
          <Home />
        </ProjectComposerBranchProvider>
      </div>
    </div>
  );
}
