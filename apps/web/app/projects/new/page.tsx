"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
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
import {
  Button,
  Card,
  Chip,
  Dropdown,
  Input,
  Label,
  SearchField,
  TextArea,
  TextField,
} from "@heroui/react";
import { ItemCard } from "@cocola/ui-compat/item-card";
import { ItemCardGroup } from "@cocola/ui-compat/item-card-group";
import { PressableFeedback } from "@cocola/ui-compat/pressable-feedback";
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
  const t = useTranslations("projects.new");
  const { refreshProjects } = useCocola();
  const [connection, setConnection] = useState<Connection | null>(null);
  const [mode, setMode] = useState<Mode>("empty");
  const [name, setName] = useState("");
  const [repositoryName, setRepositoryName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<"private" | "public">("private");
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
      if (!response.ok) throw new Error(t("errors.connection"));
      setConnection((await response.json()) as Connection);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, [t]);

  useEffect(() => {
    void loadConnection();
  }, [loadConnection]);

  const loadRepositories = useCallback(
    async (cursor = "") => {
      setBusy(true);
      setError("");
      try {
        const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
        const response = await fetch(`/api/scm/github/repositories${query}`, {
          cache: "no-store",
        });
        if (!response.ok) throw new Error(t("errors.repositories"));
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
    },
    [t],
  );

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
    if (!projectName) {
      setError(t("errors.projectName"));
      return;
    }
    if (mode === "github_create" && !repositoryName.trim()) {
      setError(t("errors.repositoryName"));
      return;
    }
    if (mode === "github_import" && !selectedRepository) {
      setError(t("errors.chooseRepository"));
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
        throw new Error(body.error?.message || t("errors.create"));
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
    <WorkspacePageFrame layout="content">
      <header className="flex items-center gap-3">
        <Button isIconOnly aria-label={t("back")} variant="ghost" onPress={() => router.back()}>
          <ArrowLeft className="size-4" />
        </Button>
        <span className="bg-accent-soft text-accent flex size-11 items-center justify-center rounded-2xl">
          <Plus className="size-5" />
        </span>
        <div>
          <h1 className="text-2xl font-semibold tracking-[-0.03em]">{t("title")}</h1>
          <p className="text-muted mt-1 text-sm">{t("description")}</p>
        </div>
      </header>

      <ItemCardGroup className="cocola-web-item-grid" columns={3} layout="grid">
        <SourceCard
          active={mode === "empty"}
          kind="empty"
          icon={FolderGit2}
          title={t("sources.empty.title")}
          detail={t("sources.empty.description")}
          onClick={() => setMode("empty")}
        />
        <SourceCard
          active={mode === "github_create"}
          kind="github"
          icon={GitHubIcon}
          title={t("sources.create.title")}
          detail={
            githubReady
              ? t("sources.connected", { account: connection.external_login || "GitHub" })
              : t("sources.connectorRequired")
          }
          onClick={() => setMode("github_create")}
        />
        <SourceCard
          active={mode === "github_import"}
          kind="import"
          icon={GitFork}
          title={t("sources.import.title")}
          detail={githubReady ? t("sources.import.description") : t("sources.connectorRequired")}
          onClick={() => setMode("github_import")}
        />
      </ItemCardGroup>

      {mode !== "empty" && !githubReady ? (
        <Card className="border-warning/25 bg-warning/5 p-5">
          <Card.Header className="p-0">
            <Card.Title>{t("connect.title")}</Card.Title>
          </Card.Header>
          <Card.Content className="mt-2 p-0">
            <p className="text-muted text-sm leading-6">{t("connect.description")}</p>
          </Card.Content>
          <Card.Footer className="mt-4 p-0">
            <Button variant="outline" onPress={() => router.push("/connectors")}>
              {t("connect.action")}
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
          <Card.Header className="p-0">
            <Card.Title>{t("details.title")}</Card.Title>
            <Card.Description>{t("details.editable")}</Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 grid gap-4 p-0 sm:grid-cols-2">
            <ProjectField
              label={t("fields.projectName")}
              value={name}
              onChange={setName}
              placeholder={t("fields.projectNamePlaceholder")}
            />
            {mode === "github_create" ? (
              <ProjectField
                label={t("fields.repositoryName")}
                value={repositoryName}
                onChange={(value) => {
                  setRepositoryName(value);
                  if (!name) setName(value);
                }}
                placeholder="my-project"
              />
            ) : null}
            <ProjectField
              label={t("fields.description")}
              value={description}
              onChange={setDescription}
              placeholder={t("fields.optional")}
              wide
            />
            {mode === "github_create" ? (
              <ChoiceDropdown
                label={t("fields.visibility")}
                value={visibility === "private" ? t("visibility.private") : t("visibility.public")}
                options={[
                  { id: "private", label: t("visibility.private") },
                  { id: "public", label: t("visibility.public") },
                ]}
                onChange={(value) => setVisibility(value as "private" | "public")}
              />
            ) : null}
          </Card.Content>
        </Card>
      ) : null}

      {mode === "github_import" && githubReady ? (
        <Card className="p-5">
          <Card.Header className="p-0">
            <Card.Title>{t("details.title")}</Card.Title>
            <Card.Description>{t("details.imported")}</Card.Description>
          </Card.Header>
          <Card.Content className="mt-5 grid gap-4 p-0 sm:grid-cols-2">
            <ProjectField
              label={t("fields.projectName")}
              value={name}
              onChange={setName}
              placeholder={selectedRepository?.name || t("fields.projectName")}
            />
            <ProjectField
              label={t("fields.description")}
              value={description}
              onChange={setDescription}
              placeholder={t("fields.optional")}
            />
          </Card.Content>
        </Card>
      ) : null}

      {mode === "empty" || githubReady ? (
        <div className="border-separator flex flex-col gap-3 border-t pt-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0 flex-1">
            {error ? (
              <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
                {error}
              </p>
            ) : null}
          </div>
          <div className="flex shrink-0 justify-end gap-2">
            <Button variant="outline" onPress={() => router.back()}>
              {t("actions.cancel")}
            </Button>
            <Button isDisabled={busy} isPending={busy} onPress={() => void submit()}>
              {busy ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
              {t("actions.create")}
            </Button>
          </div>
        </div>
      ) : null}
    </WorkspacePageFrame>
  );
}

function SourceCard({
  active,
  kind,
  icon: Icon,
  title,
  detail,
  onClick,
}: {
  active: boolean;
  kind: "empty" | "github" | "import";
  icon: typeof GitFork;
  title: string;
  detail: string;
  onClick: () => void;
}) {
  return (
    <ItemCard<"button">
      className={`relative w-full cursor-pointer overflow-hidden ${active ? "ring-accent bg-accent-soft ring-2" : ""}`}
      render={(props) => <button type="button" {...props} onClick={onClick} />}
    >
      <PressableFeedback.Highlight />
      <ItemCard.Icon
        className={
          kind === "empty"
            ? "bg-amber-100 text-amber-600 dark:bg-amber-950 dark:text-amber-300"
            : kind === "github"
              ? "bg-zinc-950 text-white dark:bg-white dark:text-zinc-950"
              : "bg-indigo-100 text-indigo-600 dark:bg-indigo-950 dark:text-indigo-300"
        }
      >
        <Icon className="size-5" />
      </ItemCard.Icon>
      <ItemCard.Content>
        <ItemCard.Title>{title}</ItemCard.Title>
        <ItemCard.Description>{detail}</ItemCard.Description>
      </ItemCard.Content>
      <ItemCard.Action>
        {active ? <CircleCheck className="text-accent size-4" /> : null}
      </ItemCard.Action>
    </ItemCard>
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
  const t = useTranslations("projects.new.repositories");
  return (
    <Card className="p-5">
      <Card.Header className="flex-row items-start justify-between gap-3 p-0">
        <span>
          <Card.Title>{t("title")}</Card.Title>
          <Card.Description>{t("description")}</Card.Description>
        </span>
        <Chip color="success" size="sm" variant="soft">
          {t("ready")}
        </Chip>
      </Card.Header>
      <Card.Content className="mt-5 p-0">
        <SearchField aria-label={t("search")} value={filter} onChange={onFilter}>
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder={t("search")} />
            <SearchField.ClearButton />
          </SearchField.Group>
        </SearchField>
        <ItemCardGroup className="mt-3 max-h-80 overflow-y-auto">
          {repositories.map((repository) => (
            <ItemCard<"button">
              key={repository.id}
              className={`relative w-full cursor-pointer overflow-hidden ${selectedID === repository.id ? "ring-accent bg-accent-soft ring-2" : ""}`}
              render={(props) => (
                <button type="button" {...props} onClick={() => onSelect(repository)} />
              )}
            >
              <PressableFeedback.Highlight />
              <ItemCard.Icon className="bg-zinc-950 text-white dark:bg-white dark:text-zinc-950">
                <Lock className="size-4" />
              </ItemCard.Icon>
              <ItemCard.Content>
                <ItemCard.Title>{repository.full_name}</ItemCard.Title>
                <ItemCard.Description>
                  {repository.default_branch} · {Math.ceil(repository.size_kb / 1024)} MB
                </ItemCard.Description>
              </ItemCard.Content>
              <ItemCard.Action>
                {selectedID === repository.id ? <Check className="text-accent size-4" /> : null}
              </ItemCard.Action>
            </ItemCard>
          ))}
        </ItemCardGroup>
        {nextCursor ? (
          <Button
            className="mt-3"
            isDisabled={busy}
            size="sm"
            variant="outline"
            onPress={onLoadMore}
          >
            <RefreshCw className={`size-3.5 ${busy ? "animate-spin" : ""}`} />
            {t("loadMore")}
          </Button>
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
  return (
    <TextField
      className={wide ? "sm:col-span-2" : ""}
      value={value}
      variant="secondary"
      onChange={onChange}
    >
      <Label>{label}</Label>
      {wide ? <TextArea rows={4} placeholder={placeholder} /> : <Input placeholder={placeholder} />}
    </TextField>
  );
}

function ChoiceDropdown({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: { id: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <div>
      <Label>{label}</Label>
      <Dropdown>
        <Dropdown.Trigger
          aria-label={label}
          className="border-separator bg-default hover:bg-default-hover mt-2 flex h-11 w-full min-w-0 items-center justify-between rounded-2xl border px-3 text-sm"
        >
          <span className="truncate">{value}</span>
          <ChevronDown className="text-muted size-4" />
        </Dropdown.Trigger>
        <Dropdown.Popover placement="bottom start">
          <Dropdown.Menu aria-label={label} onAction={(key) => onChange(String(key))}>
            {options.map((option) => (
              <Dropdown.Item key={option.id} id={option.id} textValue={option.label}>
                {option.label}
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        </Dropdown.Popover>
      </Dropdown>
    </div>
  );
}
