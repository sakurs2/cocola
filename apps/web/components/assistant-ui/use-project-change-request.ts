"use client";

import { useCallback, useEffect, useState } from "react";

export type ProjectChangeRequest = {
  conversation_id: string;
  project_id: string;
  provider: "local" | "github";
  external_number?: number;
  external_url?: string;
  status: "working" | "open" | "checks_pending" | "conflict" | "merged" | "closed" | "failed";
  head_sha?: string;
  merge_sha?: string;
  error_code?: string;
  updated_at: string;
};

export const PROJECT_CHANGE_REQUEST_EVENT = "cocola:project-change-request";

export function useProjectChangeRequest(projectID: string, taskID: string, active: boolean) {
  const [value, setValue] = useState<ProjectChangeRequest | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const request = useCallback(
    async (action: "load" | "create" | "refresh" | "merge") => {
      if (!projectID || !taskID) return;
      setLoading(true);
      setError("");
      try {
        const suffix =
          action === "merge" ? "merge" : action === "refresh" ? "refresh" : "change-request";
        const response = await fetch(
          `/api/projects/${encodeURIComponent(projectID)}/tasks/${encodeURIComponent(taskID)}/${suffix}`,
          { method: action === "load" ? "GET" : "POST", cache: "no-store" },
        );
        const body = (await response.json().catch(() => ({}))) as ProjectChangeRequest & {
          error?: { message?: string };
        };
        if (response.status === 404 && action === "load") {
          setValue(null);
          return;
        }
        if (!response.ok) throw new Error(body.error?.message || "Change request action failed");
        setValue(body);
        window.dispatchEvent(
          new CustomEvent(PROJECT_CHANGE_REQUEST_EVENT, {
            detail: { projectID, taskID, changeRequest: body },
          }),
        );
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : "Change request action failed");
      } finally {
        setLoading(false);
      }
    },
    [projectID, taskID],
  );

  useEffect(() => {
    if (active && projectID) void request("load");
  }, [active, projectID, request]);

  return { changeRequest: value, error, loading, request };
}
