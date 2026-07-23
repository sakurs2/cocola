"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { ChevronLeft, GitBranch } from "lucide-react";
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
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <div className="flex h-10 shrink-0 items-center gap-2 border-b border-border px-3 text-xs text-muted-foreground">
        <button
          type="button"
          onClick={() => router.push(`/projects/${encodeURIComponent(params.id)}`)}
          className="inline-flex items-center gap-1 rounded-md px-1.5 py-1 hover:bg-muted hover:text-foreground"
        >
          <ChevronLeft className="size-3.5" />
          {project?.name || projectName || "Project"}
        </button>
        <span>/</span>
        <span className="max-w-64 truncate text-foreground/80">
          {conversation?.title || "Task"}
        </span>
        <span className="ml-auto inline-flex items-center gap-1">
          <GitBranch className="size-3.5" />
          {branchName}
        </span>
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
