"use client";

import { useCallback, useEffect, useRef, useState } from "react";

export type GitChange = {
  path: string;
  old_path?: string;
  status: string;
  area: "staged" | "working" | "both" | "untracked" | string;
};

export type GitCommit = {
  sha: string;
  parents?: string[];
  subject: string;
  body?: string;
  author_name: string;
  authored_at: string;
  refs?: string[];
  files_changed?: number;
  additions?: number;
  deletions?: number;
};

export type GitCommitFile = {
  path: string;
  old_path?: string;
  status: string;
  binary?: boolean;
  additions: number;
  deletions: number;
};

export type GitSnapshot = {
  branch?: string;
  base_ref?: string;
  base_sha?: string;
  head_sha?: string;
  ahead?: number;
  dirty?: boolean;
  changes?: GitChange[];
  truncated?: boolean;
  commits?: GitCommit[];
  history_truncated?: boolean;
  captured_at?: string;
};

export type GitDiff = {
  path: string;
  text: string;
  binary: boolean;
  truncated: boolean;
  commitSHA?: string;
};

export type GitCommitDetail = {
  commit: GitCommit;
  files: GitCommitFile[];
  truncated: boolean;
};

export type GitInspectOperation = "status" | "diff" | "commit";

export type GitInspectOptions = {
  path?: string;
  diffTarget?: string;
  commitSHA?: string;
};

export function useGitWorkspace(sessionID: string, active: boolean) {
  const [snapshot, setSnapshot] = useState<GitSnapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [commitDetail, setCommitDetail] = useState<GitCommitDetail | null>(null);
  const [diff, setDiff] = useState<GitDiff | null>(null);
  const [projectID, setProjectID] = useState("");
  const requestSequence = useRef(0);

  const loadStored = useCallback(async () => {
    const requestID = ++requestSequence.current;
    setLoading(true);
    setError(null);
    try {
      const response = await fetch(
        `/api/conversations/${encodeURIComponent(sessionID)}/git/status`,
        { cache: "no-store" },
      );
      if (!response.ok) throw new Error("Could not load the saved Git snapshot");
      const body = (await response.json()) as {
        workspace?: { project_id?: string; git_snapshot?: GitSnapshot; branch_name?: string };
      };
      if (requestID !== requestSequence.current) return;
      setSnapshot({
        ...(body.workspace?.git_snapshot ?? {}),
        branch: body.workspace?.git_snapshot?.branch || body.workspace?.branch_name,
      });
      setProjectID(body.workspace?.project_id || "");
    } catch (loadError) {
      if (requestID === requestSequence.current) {
        setError(loadError instanceof Error ? loadError.message : "Could not load Git status");
      }
    } finally {
      if (requestID === requestSequence.current) setLoading(false);
    }
  }, [sessionID]);

  useEffect(() => {
    if (active) void loadStored();
  }, [active, loadStored]);

  const inspect = useCallback(
    async (operation: GitInspectOperation, options: GitInspectOptions = {}) => {
      const requestID = ++requestSequence.current;
      setLoading(true);
      setError(null);
      try {
        const response = await fetch(
          `/api/conversations/${encodeURIComponent(sessionID)}/git/inspect`,
          {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({
              operation,
              path: options.path ?? "",
              diff_target: options.diffTarget ?? "working",
              commit_sha: options.commitSHA ?? "",
            }),
          },
        );
        const body = (await response.json().catch(() => ({}))) as {
          snapshot?: GitSnapshot;
          diff?: string;
          binary?: boolean;
          truncated?: boolean;
          commit?: GitCommit;
          commit_files?: GitCommitFile[];
          error?: { message?: string };
        };
        if (!response.ok) {
          throw new Error(
            response.status === 409
              ? "The Agent is running. Git status will update when it finishes."
              : body.error?.message || "Could not inspect Git workspace",
          );
        }
        if (requestID !== requestSequence.current) return;
        if (body.snapshot) setSnapshot(body.snapshot);
        if (operation === "diff") {
          setDiff({
            path: options.path ?? "",
            text: body.diff ?? "",
            binary: Boolean(body.binary),
            truncated: Boolean(body.truncated),
          });
        } else if (operation === "commit" && body.commit) {
          if (options.path) {
            setDiff({
              path: options.path,
              text: body.diff ?? "",
              binary: Boolean(body.binary),
              truncated: Boolean(body.truncated),
              commitSHA: body.commit.sha,
            });
          } else {
            setCommitDetail({
              commit: body.commit,
              files: body.commit_files ?? [],
              truncated: Boolean(body.truncated),
            });
          }
        }
      } catch (inspectError) {
        if (requestID === requestSequence.current) {
          setError(
            inspectError instanceof Error
              ? inspectError.message
              : "Could not inspect Git workspace",
          );
        }
      } finally {
        if (requestID === requestSequence.current) setLoading(false);
      }
    },
    [sessionID],
  );

  const closeDiff = useCallback(() => setDiff(null), []);
  const closeCommit = useCallback(() => setCommitDetail(null), []);

  return {
    closeCommit,
    closeDiff,
    commitDetail,
    diff,
    error,
    inspect,
    loading,
    projectID,
    snapshot,
  };
}
