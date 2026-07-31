"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  Check,
  FolderGit2,
  GitFork,
  GitFork as GitHubIcon,
  Loader2,
  Lock,
  Plus,
  RefreshCw,
  Search,
} from "lucide-react";
import { useCocola } from "@/app/runtime-provider";
import { SelectControl } from "@/components/ui/select-control";
import { cn } from "@/lib/utils";
import { nextProjectCreateIntent } from "@/lib/project-task-intent.mjs";

type Mode = "empty" | "github_create" | "github_import";

type Connection = {
  enabled: boolean;
  status: string;
  external_login?: string;
};

type Repository = {
  id: number;
  name: string;
  full_name: string;
  default_branch: string;
  private: boolean;
  size_kb: number;
};

export default function NewProjectPage() {
  const router = useRouter();
  const {
    runtimes,
    refreshProjects,
    defaultAgentRuntimeID,
    runtimePickerEnabled,
    runtimeConfigError,
  } = useCocola();
  const [connection, setConnection] = useState<Connection | null>(null);
  const [mode, setMode] = useState<Mode>("empty");
  const [name, setName] = useState("");
  const [repositoryName, setRepositoryName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<"private" | "public">("private");
  const [runtimeID, setRuntimeID] = useState("");
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [selectedRepositoryID, setSelectedRepositoryID] = useState<number | null>(null);
  const [filter, setFilter] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const createIntent = useRef<{ fingerprint: string; requestID: string } | null>(null);

  const loadConnection = useCallback(async () => {
    try {
      const response = await fetch("/api/connectors/github", { cache: "no-store" });
      if (!response.ok) throw new Error("Could not check GitHub connection");
      setConnection((await response.json()) as Connection);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  useEffect(() => {
    void loadConnection();
  }, [loadConnection]);

  useEffect(() => {
    if (runtimeID || !defaultAgentRuntimeID) return;
    setRuntimeID(defaultAgentRuntimeID);
  }, [defaultAgentRuntimeID, runtimeID]);

  const loadRepositories = useCallback(async (cursor = "") => {
    setBusy(true);
    setError("");
    try {
      const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
      const response = await fetch(`/api/scm/github/repositories${query}`, {
        cache: "no-store",
      });
      if (!response.ok) throw new Error("Could not list installed repositories");
      const page = (await response.json()) as {
        repositories?: Repository[];
        next_cursor?: string;
      };
      setRepositories((current) =>
        cursor
          ? [
              ...current,
              ...(page.repositories ?? []).filter(
                (repository) => !current.some((item) => item.id === repository.id),
              ),
            ]
          : (page.repositories ?? []),
      );
      setNextCursor(page.next_cursor ?? "");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    if (mode === "github_import" && connection?.status === "ready") {
      void loadRepositories();
    }
  }, [connection?.status, loadRepositories, mode]);

  const selectedRepository = repositories.find(
    (repository) => repository.id === selectedRepositoryID,
  );
  const visibleRepositories = useMemo(() => {
    const query = filter.trim().toLowerCase();
    return query
      ? repositories.filter((repository) => repository.full_name.toLowerCase().includes(query))
      : repositories;
  }, [filter, repositories]);

  const submit = async () => {
    const projectName = name.trim() || selectedRepository?.name || "";
    if (!projectName || !runtimeID) {
      setError(runtimeConfigError || "Project name and Agent Runtime are required.");
      return;
    }
    if (mode === "github_create" && !repositoryName.trim()) {
      setError("Repository name is required.");
      return;
    }
    if (mode === "github_import" && !selectedRepository) {
      setError("Choose a repository to import.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const source =
        mode === "empty"
          ? { type: "empty" as const }
          : mode === "github_create"
            ? {
                type: "github_create" as const,
                repository_name: repositoryName.trim(),
                visibility,
              }
            : {
                type: "github_import" as const,
                repository_name: selectedRepository!.name,
                repository_id: selectedRepository!.id,
                visibility: selectedRepository!.private ? "private" : "public",
              };
      const payload = {
        name: projectName,
        description: description.trim(),
        runtime_id: runtimeID,
        source,
      };
      const intent = nextProjectCreateIntent(createIntent.current, payload, () =>
        crypto.randomUUID(),
      );
      createIntent.current = intent;
      const response = await fetch("/api/projects", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ client_request_id: intent.requestID, ...payload }),
      });
      const body = (await response.json().catch(() => ({}))) as {
        id?: string;
        error?: { message?: string };
      };
      if (!response.ok || !body.id) {
        throw new Error(body.error?.message || "Could not create project");
      }
      createIntent.current = null;
      refreshProjects();
      router.push(`/projects/${encodeURIComponent(body.id)}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const githubReady = connection?.status === "ready";

  return (
    <main className="user-canvas user-page user-theme-indigo h-full min-w-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6 sm:py-7">
      <div className="mx-auto w-full max-w-3xl pb-16">
        <button
          type="button"
          onClick={() => router.back()}
          className="group inline-flex items-center gap-1.5 rounded-full border border-black/[0.06] bg-white px-4 py-2 text-[13px] font-medium text-muted-foreground shadow-[0_1px_2px_0_rgb(15_23_42/0.06),0_6px_16px_-10px_rgb(15_23_42/0.25)] transition-all duration-200 hover:-translate-y-0.5 hover:text-foreground hover:shadow-[0_2px_4px_0_rgb(15_23_42/0.08),0_12px_24px_-12px_rgb(15_23_42/0.35)] active:translate-y-0 active:shadow-[0_1px_2px_0_rgb(15_23_42/0.06)]"
        >
          <ArrowLeft className="size-4 transition-transform duration-200 group-hover:-translate-x-0.5" />
          Back
        </button>

        <section className="mt-8 space-y-6">
          <div className="grid gap-3 sm:grid-cols-3">
            <SourceCard
              active={mode === "empty"}
              icon={FolderGit2}
              title="Empty Project"
              detail="Local workspace on main"
              onClick={() => setMode("empty")}
            />
            <SourceCard
              active={mode === "github_create"}
              icon={GitHubIcon}
              title="Create on GitHub"
              detail={
                githubReady ? `Connected as ${connection.external_login}` : "Connector required"
              }
              onClick={() => setMode("github_create")}
            />
            <SourceCard
              active={mode === "github_import"}
              icon={GitFork}
              title="Import GitHub"
              detail={githubReady ? "Choose an installed repository" : "Connector required"}
              onClick={() => setMode("github_import")}
            />
          </div>

          {mode !== "empty" && !githubReady ? (
            <div className="group rounded-2xl border border-amber-500/25 bg-amber-500/5 p-5 transition-all duration-300 hover:border-amber-500/40 hover:bg-amber-500/10 hover:shadow-[0_10px_30px_-18px_rgba(217,119,6,0.6)]">
              <h2 className="text-sm font-semibold">Connect your personal GitHub App first</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                Empty Projects remain available without GitHub. GitHub create and import use your
                own private App.
              </p>
              <Link
                href="/connectors"
                className="mt-4 inline-flex h-9 items-center rounded-xl bg-foreground px-4 text-sm font-medium text-background transition-transform duration-200 hover:-translate-y-0.5 active:translate-y-0"
              >
                Open Connectors
              </Link>
            </div>
          ) : null}

          {mode === "github_import" && githubReady ? (
            <RepositoryPicker
              filter={filter}
              onFilter={setFilter}
              repositories={visibleRepositories}
              selectedID={selectedRepositoryID}
              onSelect={(repository) => {
                setSelectedRepositoryID(repository.id);
                if (!name) setName(repository.name);
              }}
              nextCursor={nextCursor}
              busy={busy}
              onLoadMore={() => void loadRepositories(nextCursor)}
            />
          ) : null}

          {(mode === "empty" || githubReady) && mode !== "github_import" ? (
            <div className="user-panel grid gap-4 p-5 sm:grid-cols-2">
              <Field
                label="Project name"
                value={name}
                onChange={setName}
                placeholder="My project"
              />
              {mode === "github_create" ? (
                <Field
                  label="Repository name"
                  value={repositoryName}
                  onChange={(value) => {
                    setRepositoryName(value);
                    if (!name) setName(value);
                  }}
                  placeholder="my-project"
                />
              ) : null}
              <Field
                label="Description"
                value={description}
                onChange={setDescription}
                placeholder="Optional"
                wide
              />
              {mode === "github_create" ? (
                <label className="space-y-1.5">
                  <span className="user-section-title text-sm font-medium">Visibility</span>
                  <SelectControl
                    value={visibility}
                    onValueChange={(value) => setVisibility(value as "private" | "public")}
                    options={[
                      { value: "private", label: "Private (recommended)" },
                      { value: "public", label: "Public" },
                    ]}
                    contentClassName="cocola-user-ui"
                  />
                </label>
              ) : null}
            </div>
          ) : null}

          {mode === "github_import" && githubReady ? (
            <div className="user-panel grid gap-4 p-5 sm:grid-cols-2">
              <Field
                label="Project name"
                value={name}
                onChange={setName}
                placeholder={selectedRepository?.name || "Project name"}
              />
              <Field
                label="Description"
                value={description}
                onChange={setDescription}
                placeholder="Optional"
              />
            </div>
          ) : null}

          {mode === "empty" || githubReady ? (
            <>
              {runtimePickerEnabled ? (
                <label className="block space-y-1.5">
                  <span className="user-section-title text-sm font-medium">
                    Default Agent Runtime
                  </span>
                  <SelectControl
                    value={runtimeID}
                    onValueChange={setRuntimeID}
                    options={runtimes.map((runtime) => ({
                      value: runtime.id,
                      label: runtime.label,
                    }))}
                    contentClassName="cocola-user-ui"
                  />
                </label>
              ) : null}
              {error ? (
                <p
                  role="alert"
                  className="rounded-xl bg-destructive/10 px-3 py-2 text-sm text-destructive"
                >
                  {error}
                </p>
              ) : null}
              <button
                type="button"
                disabled={busy || !runtimeID}
                onClick={() => void submit()}
                className="user-accent-btn inline-flex h-10 items-center gap-2 rounded-xl px-4 text-sm font-semibold disabled:opacity-50"
              >
                {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
                Create project
              </button>
            </>
          ) : null}
        </section>
      </div>
    </main>
  );
}

function SourceCard({
  active,
  icon: Icon,
  title,
  detail,
  onClick,
}: {
  active: boolean;
  icon: typeof GitFork;
  title: string;
  detail: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className="user-card user-card--hover group relative text-left"
      style={
        active
          ? {
              borderWidth: 2,
              borderStyle: "solid",
              borderColor: "var(--page-accent)",
              background: "var(--page-accent-soft)",
            }
          : undefined
      }
    >
      <span className="user-card-glyph size-[38px]">
        <Icon className="size-[18px] transition-transform duration-300 group-hover:scale-110" />
      </span>
      <span className="user-card-name mt-2.5 block text-sm font-semibold">{title}</span>
      <span className="user-card-desc mt-1 block text-xs">{detail}</span>
      {active ? (
        <Check className="absolute right-3.5 top-3.5 size-4 text-[color:var(--page-accent)]" />
      ) : null}
    </button>
  );
}

function RepositoryPicker({
  filter,
  onFilter,
  repositories,
  selectedID,
  onSelect,
  nextCursor,
  busy,
  onLoadMore,
}: {
  filter: string;
  onFilter: (value: string) => void;
  repositories: Repository[];
  selectedID: number | null;
  onSelect: (repository: Repository) => void;
  nextCursor: string;
  busy: boolean;
  onLoadMore: () => void;
}) {
  return (
    <div className="user-panel p-4">
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <input
          value={filter}
          onChange={(event) => onFilter(event.target.value)}
          placeholder="Search repositories"
          className="user-search-input user-field-input h-10 w-full rounded-xl pl-9 pr-3 text-sm"
        />
      </div>
      <div className="mt-3 max-h-72 space-y-1.5 overflow-y-auto">
        {repositories.map((repository) => (
          <button
            type="button"
            key={repository.id}
            onClick={() => onSelect(repository)}
            aria-pressed={selectedID === repository.id}
            className={cn(
              "user-card row group w-full text-left",
              selectedID === repository.id && "is-active",
            )}
          >
            <span className="user-card-glyph">
              <Lock className="size-3.5 transition-transform duration-300 group-hover:scale-110" />
            </span>
            <span className="min-w-0 flex-1">
              <span className="user-card-name block truncate text-sm font-medium">
                {repository.full_name}
              </span>
              <span className="user-card-desc text-xs">
                {repository.default_branch} · {Math.ceil(repository.size_kb / 1024)} MB
              </span>
            </span>
            {selectedID === repository.id ? (
              <Check className="size-4 text-[color:var(--page-accent)]" />
            ) : null}
          </button>
        ))}
      </div>
      {nextCursor ? (
        <button
          type="button"
          disabled={busy}
          onClick={onLoadMore}
          className="user-tbtn user-tbtn--ghost mt-3 disabled:opacity-50"
        >
          <RefreshCw className={cn("size-3.5", busy && "animate-spin")} /> Load more repositories
        </button>
      ) : null}
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  wide = false,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  wide?: boolean;
}) {
  return (
    <label className={cn("space-y-1.5", wide && "sm:col-span-2")}>
      <span className="user-section-title text-sm font-medium">{label}</span>
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className="user-search-input user-field-input h-10 w-full rounded-xl px-3 text-sm"
      />
    </label>
  );
}
