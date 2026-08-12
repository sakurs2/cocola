"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { ChevronLeft, GitBranch, GitMerge } from "lucide-react";
import { Button, Chip } from "@heroui/react";
import { useCocola } from "@/app/runtime-provider";
import {
  ProjectBranchBadge,
  ProjectComposerBranchProvider,
} from "@/components/assistant-ui/project-branch-control";
import Home from "@/app/page";
import {
  PROJECT_CHANGE_REQUEST_EVENT,
  type ProjectChangeRequest,
} from "@/components/assistant-ui/use-project-change-request";

type ProjectWorkspace = {
  branch_name: string;
  base_ref: string;
  base_sha: string;
};

export default function ProjectTaskPage() {
  const t = useTranslations("projects.taskBar");
  const params = useParams<{ id: string; conversationId: string }>();
  const router = useRouter();
  const { activeSessionId, loadConversation, projects } = useCocola();
  const project = projects.find((item) => item.id === params.id);
  const [projectName, setProjectName] = useState("");
  const [workspace, setWorkspace] = useState<ProjectWorkspace | null>(null);
  const [merged, setMerged] = useState(false);

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

  useEffect(() => {
    let cancelled = false;
    setMerged(false);
    void fetch(
      `/api/projects/${encodeURIComponent(params.id)}/tasks/${encodeURIComponent(params.conversationId)}/change-request`,
      { cache: "no-store" },
    )
      .then(async (response) => {
        if (!response.ok) return;
        const body = (await response.json()) as { status?: string };
        if (!cancelled) setMerged(body.status === "merged");
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [params.conversationId, params.id]);

  useEffect(() => {
    const handleChangeRequest = (event: Event) => {
      const detail = (
        event as CustomEvent<{
          projectID: string;
          taskID: string;
          changeRequest: ProjectChangeRequest;
        }>
      ).detail;
      if (detail.projectID === params.id && detail.taskID === params.conversationId) {
        setMerged(detail.changeRequest.status === "merged");
      }
    };
    window.addEventListener(PROJECT_CHANGE_REQUEST_EVENT, handleChangeRequest);
    return () => window.removeEventListener(PROJECT_CHANGE_REQUEST_EVENT, handleChangeRequest);
  }, [params.conversationId, params.id]);

  const fallbackBranch = `cocola/task-${params.conversationId.replaceAll("-", "").slice(0, 12)}`;
  const branchName = workspace?.branch_name || fallbackBranch;

  return (
    <div className="user-theme-indigo flex h-full min-h-0 flex-1 flex-col">
      <div className="border-separator bg-background flex h-10 shrink-0 items-center gap-2 border-b px-3 text-xs">
        <Button
          className="h-8 px-2 font-semibold"
          size="sm"
          variant="ghost"
          onPress={() => router.push(`/projects/${encodeURIComponent(params.id)}`)}
        >
          <ChevronLeft className="size-3.5" />
          {project?.name || projectName || t("project")}
        </Button>
        {merged ? (
          <Chip className="ml-auto" color="success" size="sm" variant="soft">
            <GitMerge className="size-3.5" />
            {t("mergedReadOnly")}
          </Chip>
        ) : null}
        <Chip
          className={`${merged ? "" : "ml-auto"} max-w-[min(24rem,48vw)]`}
          color="accent"
          size="sm"
          title={branchName}
          variant="soft"
        >
          <GitBranch className="size-3.5" />
          <span className="truncate">{branchName}</span>
        </Chip>
      </div>
      <div className="min-h-0 flex-1">
        <ProjectComposerBranchProvider
          readOnly={merged}
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
