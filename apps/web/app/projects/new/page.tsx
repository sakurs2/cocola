"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  Check,
  ChevronDown,
  CircleCheck,
  FolderGit2,
  GitFork,
  GitFork as GitHubIcon,
  Loader2,
  Lock,
  Plus,
  RefreshCw,
} from "lucide-react";
import { Button, Card, Chip, Dropdown, Input, Label, SearchField, TextArea, TextField } from "@heroui/react";
import { ItemCard } from "@heroui-pro/react/item-card";
import { ItemCardGroup } from "@heroui-pro/react/item-card-group";
import { PressableFeedback } from "@heroui-pro/react/pressable-feedback";
import { useCocola } from "@/app/runtime-provider";
import { WorkspacePageFrame } from "@/components/heroui-workspace/workspace-ui";
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
    <WorkspacePageFrame>
      <header className="flex items-center gap-3">
        <Button isIconOnly aria-label="Back to Projects" variant="ghost" onPress={() => router.back()}>
          <ArrowLeft className="size-4" />
        </Button>
        <span className="bg-accent-soft text-accent flex size-11 items-center justify-center rounded-2xl">
          <Plus className="size-5" />
        </span>
        <div>
          <h1 className="text-2xl font-semibold tracking-[-0.03em]">New Project</h1>
          <p className="text-muted mt-1 text-sm">Choose a source and repository policy.</p>
        </div>
      </header>

      <ItemCardGroup className="cocola-web-item-grid" columns={3} layout="grid">
            <SourceCard
              active={mode === "empty"}
              icon={FolderGit2}
              title="Empty Project"
              detail="Start from a clean local workspace."
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
      </ItemCardGroup>

          {mode !== "empty" && !githubReady ? (
            <Card className="border-warning/25 bg-warning/5 p-5">
              <Card.Header className="p-0"><Card.Title>Connect your personal GitHub App first</Card.Title></Card.Header>
              <Card.Content className="mt-2 p-0">
              <p className="text-muted text-sm leading-6">
                Empty Projects remain available without GitHub. GitHub create and import use your
                own private App.
              </p>
              </Card.Content>
              <Card.Footer className="mt-4 p-0">
              <Button variant="outline" onPress={() => router.push("/connectors")}>
                Open Connectors
              </Button>
              </Card.Footer>
            </Card>
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
            <Card className="p-5">
              <Card.Header className="p-0"><Card.Title>Project details</Card.Title><Card.Description>These settings are editable after provisioning.</Card.Description></Card.Header>
              <Card.Content className="mt-5 grid gap-4 p-0 sm:grid-cols-2">
              <ProjectField label="Project name" value={name} onChange={setName} placeholder="My project" />
              {mode === "github_create" ? (
                <ProjectField
                  label="Repository name"
                  value={repositoryName}
                  onChange={(value) => {
                    setRepositoryName(value);
                    if (!name) setName(value);
                  }}
                  placeholder="my-project"
                />
              ) : null}
              <ProjectField
                label="Description"
                value={description}
                onChange={setDescription}
                placeholder="Optional"
                wide
              />
              {mode === "github_create" ? (
                <ChoiceDropdown label="Visibility" value={visibility === "private" ? "Private (recommended)" : "Public"} options={[{id:"private", label:"Private (recommended)"},{id:"public", label:"Public"}]} onChange={(value) => setVisibility(value as "private" | "public")} />
              ) : null}
              </Card.Content>
            </Card>
          ) : null}

          {mode === "github_import" && githubReady ? (
            <Card className="p-5">
              <Card.Header className="p-0"><Card.Title>Project details</Card.Title><Card.Description>Name and describe the imported workspace.</Card.Description></Card.Header>
              <Card.Content className="mt-5 grid gap-4 p-0 sm:grid-cols-2">
              <ProjectField
                label="Project name"
                value={name}
                onChange={setName}
                placeholder={selectedRepository?.name || "Project name"}
              />
              <ProjectField
                label="Description"
                value={description}
                onChange={setDescription}
                placeholder="Optional"
              />
              </Card.Content>
            </Card>
          ) : null}

          {mode === "empty" || githubReady ? (
            <Card className="p-5">
              <Card.Header className="p-0"><Card.Title>Provisioning</Card.Title><Card.Description>Choose the runtime used when this Project starts work.</Card.Description></Card.Header>
              <Card.Content className="mt-5 grid gap-4 p-0">
              {runtimePickerEnabled ? (
                <ChoiceDropdown label="Default Agent Runtime" value={runtimes.find((runtime) => runtime.id === runtimeID)?.label || "Choose a runtime"} options={runtimes.map((runtime) => ({id:runtime.id,label:runtime.label}))} onChange={setRuntimeID} />
              ) : null}
              {error ? (
                <p
                  role="alert"
                  className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger"
                >
                  {error}
                </p>
              ) : null}
              </Card.Content>
              <Card.Footer className="mt-5 justify-end gap-2 p-0">
              <Button variant="outline" onPress={() => router.back()}>Cancel</Button>
              <Button isDisabled={busy || !runtimeID} isPending={busy} onPress={() => void submit()}>
                {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
                Create project
              </Button>
              </Card.Footer>
            </Card>
          ) : null}
    </WorkspacePageFrame>
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
  return <ItemCard<"button"> className={`relative w-full cursor-pointer overflow-hidden ${active ? "ring-accent bg-accent-soft ring-2" : ""}`} render={(props) => <button type="button" {...props} onClick={onClick} />}><PressableFeedback.Highlight /><ItemCard.Icon className={title === "Empty Project" ? "bg-amber-100 text-amber-600 dark:bg-amber-950 dark:text-amber-300" : title === "Create on GitHub" ? "bg-zinc-950 text-white dark:bg-white dark:text-zinc-950" : "bg-indigo-100 text-indigo-600 dark:bg-indigo-950 dark:text-indigo-300"}><Icon className="size-5" /></ItemCard.Icon><ItemCard.Content><ItemCard.Title>{title}</ItemCard.Title><ItemCard.Description>{detail}</ItemCard.Description></ItemCard.Content><ItemCard.Action>{active ? <CircleCheck className="text-accent size-4" /> : null}</ItemCard.Action></ItemCard>;
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
    <Card className="p-5">
      <Card.Header className="flex-row items-start justify-between gap-3 p-0"><span><Card.Title>GitHub repositories</Card.Title><Card.Description>Choose an installed repository to import.</Card.Description></span><Chip color="success" size="sm" variant="soft">Ready</Chip></Card.Header>
      <Card.Content className="mt-5 p-0">
      <SearchField aria-label="Search repositories" value={filter} onChange={onFilter}><SearchField.Group><SearchField.SearchIcon /><SearchField.Input placeholder="Search repositories" /><SearchField.ClearButton /></SearchField.Group></SearchField>
      <ItemCardGroup className="mt-3 max-h-80 overflow-y-auto">
        {repositories.map((repository) => (
          <ItemCard<"button">
            key={repository.id}
            className={`relative w-full cursor-pointer overflow-hidden ${selectedID === repository.id ? "ring-accent bg-accent-soft ring-2" : ""}`}
            render={(props) => <button type="button" {...props} onClick={() => onSelect(repository)} />}
          >
            <PressableFeedback.Highlight /><ItemCard.Icon className="bg-zinc-950 text-white dark:bg-white dark:text-zinc-950"><Lock className="size-4" /></ItemCard.Icon><ItemCard.Content><ItemCard.Title>{repository.full_name}</ItemCard.Title><ItemCard.Description>{repository.default_branch} · {Math.ceil(repository.size_kb / 1024)} MB</ItemCard.Description></ItemCard.Content><ItemCard.Action>{selectedID === repository.id ? <Check className="text-accent size-4" /> : null}</ItemCard.Action>
          </ItemCard>
        ))}
      </ItemCardGroup>
      {nextCursor ? (
        <Button className="mt-3" isDisabled={busy} size="sm" variant="outline" onPress={onLoadMore}><RefreshCw className={`size-3.5 ${busy ? "animate-spin" : ""}`} />Load more repositories</Button>
      ) : null}
      </Card.Content>
    </Card>
  );
}

function ProjectField({
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
  return <TextField className={wide ? "sm:col-span-2" : ""} value={value} variant="secondary" onChange={onChange}><Label>{label}</Label>{wide ? <TextArea rows={4} placeholder={placeholder} /> : <Input placeholder={placeholder} />}</TextField>;
}

function ChoiceDropdown({label, value, options, onChange}: {label:string; value:string; options:{id:string;label:string}[]; onChange:(value:string)=>void}) {
  return <div><Label>{label}</Label><Dropdown><Dropdown.Trigger aria-label={label} className="border-separator bg-default hover:bg-default-hover mt-2 flex h-11 w-full min-w-0 items-center justify-between rounded-2xl border px-3 text-sm"><span className="truncate">{value}</span><ChevronDown className="text-muted size-4" /></Dropdown.Trigger><Dropdown.Popover placement="bottom start"><Dropdown.Menu aria-label={label} onAction={(key) => onChange(String(key))}>{options.map((option) => <Dropdown.Item key={option.id} id={option.id} textValue={option.label}>{option.label}</Dropdown.Item>)}</Dropdown.Menu></Dropdown.Popover></Dropdown></div>;
}
