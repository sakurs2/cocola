"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  Check,
  ExternalLink,
  GitFork,
  Loader2,
  Lock,
  Plus,
  RefreshCw,
  Search,
} from "lucide-react";
import { useCocola } from "@/app/runtime-provider";
import { cn } from "@/lib/utils";
import { nextProjectCreateIntent } from "@/lib/project-task-intent.mjs";

type Connection = {
  enabled: boolean;
  status:
    | "disabled"
    | "disconnected"
    | "installation_required"
    | "ready"
    | "reauthorization_required";
  external_login?: string;
  installation_url?: string;
};

type Repository = {
  id: number;
  owner: string;
  name: string;
  full_name: string;
  html_url: string;
  default_branch: string;
  private: boolean;
  size_kb: number;
};

export default function NewProjectPage() {
  const router = useRouter();
  const { runtimes, refreshProjects } = useCocola();
  const [connection, setConnection] = useState<Connection | null>(null);
  const [mode, setMode] = useState<"create" | "import">("create");
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
  const [error, setError] = useState<string | null>(null);
  const createIntent = useRef<{ fingerprint: string; requestID: string } | null>(null);

  const loadConnection = useCallback(async () => {
    try {
      const response = await fetch("/api/scm/github/connection", { cache: "no-store" });
      if (!response.ok) throw new Error("Could not check GitHub connection");
      setConnection((await response.json()) as Connection);
    } catch (loadError) {
      setError(
        loadError instanceof Error ? loadError.message : "Could not check GitHub connection",
      );
    }
  }, []);

  useEffect(() => {
    void loadConnection();
  }, [loadConnection]);

  useEffect(() => {
    if (runtimeID || runtimes.length === 0) return;
    setRuntimeID(runtimes.find((runtime) => runtime.is_default)?.id ?? runtimes[0]?.id ?? "");
  }, [runtimeID, runtimes]);

  useEffect(() => {
    const search = new URLSearchParams(window.location.search);
    const oauthError = search.get("error");
    const code = search.get("code");
    const state = search.get("state");
    if (oauthError) {
      window.history.replaceState({}, "", "/projects/new");
      setError("GitHub authorization was cancelled or denied.");
      return;
    }
    if (!code || !state) return;
    window.history.replaceState({}, "", "/projects/new");
    setBusy(true);
    void fetch("/api/scm/github/oauth/callback", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ code, state }),
    })
      .then(async (response) => {
        if (!response.ok) throw new Error("GitHub authorization could not be completed");
        await loadConnection();
      })
      .catch((callbackError) =>
        setError(
          callbackError instanceof Error ? callbackError.message : "GitHub authorization failed",
        ),
      )
      .finally(() => setBusy(false));
  }, [loadConnection]);

  useEffect(() => {
    if (connection?.status !== "ready" || mode !== "import") return;
    setBusy(true);
    setRepositories([]);
    setNextCursor("");
    void fetch("/api/scm/github/repositories", { cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error("Could not list installed repositories");
        const page = (await response.json()) as {
          repositories?: Repository[];
          next_cursor?: string;
        };
        setRepositories(page.repositories ?? []);
        setNextCursor(page.next_cursor ?? "");
      })
      .catch((loadError) =>
        setError(loadError instanceof Error ? loadError.message : "Could not load repositories"),
      )
      .finally(() => setBusy(false));
  }, [connection?.status, mode]);

  const loadMoreRepositories = async () => {
    if (!nextCursor) return;
    setBusy(true);
    setError(null);
    try {
      const response = await fetch(
        `/api/scm/github/repositories?cursor=${encodeURIComponent(nextCursor)}`,
        { cache: "no-store" },
      );
      if (!response.ok) throw new Error("Could not load more repositories");
      const page = (await response.json()) as {
        repositories?: Repository[];
        next_cursor?: string;
      };
      setRepositories((current) => [
        ...current,
        ...(page.repositories ?? []).filter(
          (repository) => !current.some((item) => item.id === repository.id),
        ),
      ]);
      setNextCursor(page.next_cursor ?? "");
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "Could not load repositories");
    } finally {
      setBusy(false);
    }
  };

  const connect = async () => {
    setBusy(true);
    setError(null);
    try {
      const response = await fetch("/api/scm/github/oauth/start", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ return_to: "/projects/new" }),
      });
      if (!response.ok) throw new Error("Could not start GitHub authorization");
      const body = (await response.json()) as { authorization_url?: string };
      if (!body.authorization_url) throw new Error("GitHub authorization URL is missing");
      window.location.assign(body.authorization_url);
    } catch (connectError) {
      setError(connectError instanceof Error ? connectError.message : "Could not connect GitHub");
      setBusy(false);
    }
  };

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
    const repoName = mode === "create" ? repositoryName.trim() : selectedRepository?.name || "";
    if (!projectName || !repoName || !runtimeID || (mode === "import" && !selectedRepository)) {
      setError("Complete the required project and repository fields.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const payload = {
        name: projectName,
        description: description.trim(),
        runtime_id: runtimeID,
        mode,
        repository_name: repoName,
        repository_id: mode === "import" ? selectedRepository?.id : undefined,
        visibility:
          mode === "import" ? (selectedRepository?.private ? "private" : "public") : visibility,
      };
      const intent = nextProjectCreateIntent(createIntent.current, payload, () =>
        crypto.randomUUID(),
      );
      createIntent.current = intent;
      const response = await fetch("/api/projects", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          client_request_id: intent.requestID,
          ...payload,
        }),
      });
      const body = (await response.json().catch(() => ({}))) as {
        id?: string;
        error?: { message?: string };
      };
      if (!response.ok || !body.id)
        throw new Error(body.error?.message || "Could not create project");
      createIntent.current = null;
      refreshProjects();
      router.push(`/projects/${encodeURIComponent(body.id)}`);
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "Could not create project");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="h-full overflow-y-auto px-5 py-8 sm:px-8">
      <main className="mx-auto w-full max-w-3xl pb-16">
        <button
          type="button"
          onClick={() => router.back()}
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-4" /> Back
        </button>
        <div className="mt-7 flex items-start gap-4">
          <div className="grid size-11 shrink-0 place-items-center rounded-2xl bg-foreground text-background">
            <GitFork className="size-5" />
          </div>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">Create a project</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              Start a private GitHub repository or import one installed for your personal account.
            </p>
          </div>
        </div>

        {!connection ? (
          <div className="mt-10 flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Checking GitHub connection…
          </div>
        ) : null}
        {connection?.status === "disabled" ? (
          <Notice
            title="GitHub Projects are disabled"
            detail="Ask an administrator to configure the Cocola GitHub App."
          />
        ) : null}
        {connection?.status === "disconnected" ||
        connection?.status === "reauthorization_required" ? (
          <section className="mt-10 rounded-2xl border border-border bg-card p-6">
            <h2 className="font-semibold">Connect your GitHub account</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Cocola requests access through its GitHub App and only accepts a personal
              installation.
            </p>
            <button
              type="button"
              disabled={busy}
              onClick={() => void connect()}
              className="mt-5 inline-flex items-center gap-2 rounded-xl bg-foreground px-4 py-2 text-sm font-medium text-background disabled:opacity-50"
            >
              <GitFork className="size-4" /> Connect GitHub
            </button>
          </section>
        ) : null}
        {connection?.status === "installation_required" ? (
          <section className="mt-10 rounded-2xl border border-amber-500/25 bg-amber-500/5 p-6">
            <h2 className="font-semibold">Install the GitHub App</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Authorize it on your personal account, then return here and refresh.
            </p>
            <div className="mt-5 flex gap-2">
              <a
                href={connection.installation_url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 rounded-xl bg-foreground px-4 py-2 text-sm font-medium text-background"
              >
                Install App <ExternalLink className="size-4" />
              </a>
              <button
                type="button"
                onClick={() => void loadConnection()}
                className="inline-flex items-center gap-2 rounded-xl border border-border px-4 py-2 text-sm"
              >
                <RefreshCw className="size-4" /> Refresh
              </button>
            </div>
          </section>
        ) : null}

        {connection?.status === "ready" ? (
          <section className="mt-9 space-y-6">
            <div className="inline-flex rounded-xl bg-muted p-1">
              {(["create", "import"] as const).map((item) => (
                <button
                  key={item}
                  type="button"
                  onClick={() => {
                    setMode(item);
                    setError(null);
                  }}
                  className={cn(
                    "rounded-lg px-4 py-2 text-sm font-medium",
                    mode === item ? "bg-background shadow-sm" : "text-muted-foreground",
                  )}
                >
                  {item === "create" ? "Create new" : "Import existing"}
                </button>
              ))}
            </div>
            {mode === "create" ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field
                  label="Project name"
                  value={name}
                  onChange={setName}
                  placeholder="My project"
                />
                <Field
                  label="Repository name"
                  value={repositoryName}
                  onChange={(value) => {
                    setRepositoryName(value);
                    if (!name) setName(value);
                  }}
                  placeholder="my-project"
                />
                <Field
                  label="Description"
                  value={description}
                  onChange={setDescription}
                  placeholder="Optional"
                  wide
                />
                <label className="space-y-1.5">
                  <span className="text-sm font-medium">Visibility</span>
                  <select
                    value={visibility}
                    onChange={(event) => setVisibility(event.target.value as "private" | "public")}
                    className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm"
                  >
                    <option value="private">Private (recommended)</option>
                    <option value="public">Public</option>
                  </select>
                </label>
              </div>
            ) : (
              <div>
                <div className="relative">
                  <Search className="absolute left-3 top-3 size-4 text-muted-foreground" />
                  <input
                    value={filter}
                    onChange={(event) => setFilter(event.target.value)}
                    placeholder="Search repositories"
                    className="h-10 w-full rounded-xl border border-border bg-background pl-9 pr-3 text-sm"
                  />
                </div>
                <div className="mt-3 max-h-72 overflow-y-auto rounded-xl border border-border">
                  {visibleRepositories.map((repository) => (
                    <button
                      type="button"
                      key={repository.id}
                      onClick={() => {
                        setSelectedRepositoryID(repository.id);
                        if (!name) setName(repository.name);
                      }}
                      className="flex w-full items-center gap-3 border-b border-border/60 px-3 py-3 text-left last:border-0 hover:bg-muted"
                    >
                      <span className="grid size-8 place-items-center rounded-lg bg-muted">
                        <Lock className="size-3.5" />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">
                          {repository.full_name}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          {repository.default_branch} · {Math.ceil(repository.size_kb / 1024)} MB
                        </span>
                      </span>
                      {selectedRepositoryID === repository.id ? (
                        <Check className="size-4 text-primary" />
                      ) : null}
                    </button>
                  ))}
                </div>
                {nextCursor ? (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void loadMoreRepositories()}
                    className="mt-3 inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground disabled:opacity-50"
                  >
                    <RefreshCw className="size-3.5" /> Load more repositories
                  </button>
                ) : null}
                <div className="mt-4">
                  <Field
                    label="Project name"
                    value={name}
                    onChange={setName}
                    placeholder={selectedRepository?.name || "Project name"}
                  />
                </div>
              </div>
            )}
            <label className="block space-y-1.5">
              <span className="text-sm font-medium">Default Agent Runtime</span>
              <select
                value={runtimeID}
                onChange={(event) => setRuntimeID(event.target.value)}
                className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm"
              >
                {runtimes.map((runtime) => (
                  <option key={runtime.id} value={runtime.id}>
                    {runtime.label}
                  </option>
                ))}
              </select>
            </label>
            {error ? (
              <p className="rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-600">{error}</p>
            ) : null}
            <button
              type="button"
              disabled={busy}
              onClick={() => void submit()}
              className="inline-flex items-center gap-2 rounded-xl bg-primary px-4 py-2.5 text-sm font-medium text-primary-foreground disabled:opacity-50"
            >
              {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}{" "}
              Create project
            </button>
          </section>
        ) : null}
        {error && connection?.status !== "ready" ? (
          <p className="mt-5 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-600">{error}</p>
        ) : null}
      </main>
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
      <span className="text-sm font-medium">{label}</span>
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className="h-10 w-full rounded-xl border border-border bg-background px-3 text-sm"
      />
    </label>
  );
}

function Notice({ title, detail }: { title: string; detail: string }) {
  return (
    <div className="mt-10 rounded-2xl border border-border bg-muted/40 p-6">
      <h2 className="font-semibold">{title}</h2>
      <p className="mt-1 text-sm text-muted-foreground">{detail}</p>
    </div>
  );
}
