"use client";

import {
  Button,
  Card,
  Checkbox,
  Chip,
  Input,
  Label,
  Switch,
  TextField,
  Tooltip,
} from "@heroui/react";
import { EmptyState } from "@heroui-pro/react/empty-state";
import {
  ChevronLeft,
  ChevronRight,
  FileArchive,
  GitBranch,
  LoaderCircle,
  Search,
  Sparkles,
  Trash2,
  Upload,
} from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useRef, useState, type ChangeEvent } from "react";
import {
  WorkspacePageAction,
  WorkspacePageFrame,
  WorkspacePageHeader,
  WorkspaceSearch,
  WorkspaceSectionHeader,
} from "@/components/heroui-workspace/workspace-ui";
import { SkillIcon } from "@/components/ui/skill-icon";
import { paginateCatalog } from "@/lib/catalog-pagination";

const SKILLS_PER_PAGE = 12;

type Skill = {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  scope?: "admin" | "user" | string;
  source_type?: string;
  source_path?: string;
  file_count?: number;
};

type Candidate = Skill & {
  path: string;
  valid: boolean;
  errors?: string[];
};

export default function SkillsPage() {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [file, setFile] = useState<File | null>(null);
  const [gitRepo, setGitRepo] = useState("");
  const [gitRef, setGitRef] = useState("");
  const [gitPath, setGitPath] = useState("skills");
  const [candidateSource, setCandidateSource] = useState<"archive" | "git">("archive");
  const [loading, setLoading] = useState(true);
  const [working, setWorking] = useState(false);
  const [gitScanning, setGitScanning] = useState(false);
  const [actionSkillId, setActionSkillId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [skillQuery, setSkillQuery] = useState("");
  const [skillPage, setSkillPage] = useState(1);

  const filteredSkills = useMemo(() => {
    const query = skillQuery.trim().toLowerCase();
    return query
      ? skills.filter((skill) => displaySkillName(skill).toLowerCase().includes(query))
      : skills;
  }, [skillQuery, skills]);
  const sharedSkills = filteredSkills.filter((skill) => skill.scope !== "user");
  const personalSkills = filteredSkills.filter((skill) => skill.scope === "user");
  const skillCatalogPage = paginateCatalog(
    [...sharedSkills, ...personalSkills],
    skillPage,
    SKILLS_PER_PAGE,
  );
  const visibleSharedSkills = skillCatalogPage.items.filter((skill) => skill.scope !== "user");
  const visiblePersonalSkills = skillCatalogPage.items.filter((skill) => skill.scope === "user");
  const validCandidates = useMemo(
    () => candidates.filter((candidate) => candidate.valid),
    [candidates],
  );
  const allValidSelected =
    validCandidates.length > 0 && validCandidates.every((candidate) => selected[candidate.id]);

  const load = async (showLoading = true) => {
    if (showLoading) setLoading(true);
    setError(null);
    try {
      const response = await fetch("/api/skills", { cache: "no-store" });
      if (!response.ok) throw new Error(await readError(response));
      const data = await response.json();
      setSkills(data.skills ?? []);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      if (showLoading) setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const chooseFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const next = event.target.files?.[0] ?? null;
    setFile(next);
    setCandidateSource("archive");
    setCandidates([]);
    setSelected({});
    if (!next) return;
    setWorking(true);
    setError(null);
    try {
      const form = new FormData();
      form.append("file", next);
      const response = await fetch("/api/skills/scan/archive", { method: "POST", body: form });
      if (!response.ok) throw new Error(await readError(response));
      const data = await response.json();
      const found: Candidate[] = data.skills ?? [];
      setCandidates(found);
      setSelected(
        Object.fromEntries(
          found.filter((candidate) => candidate.valid).map((candidate) => [candidate.id, true]),
        ),
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setWorking(false);
      event.target.value = "";
    }
  };

  const scanGit = async () => {
    setGitScanning(true);
    setError(null);
    setCandidateSource("git");
    setFile(null);
    try {
      const response = await fetch("/api/skills/scan/git", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ repo_url: gitRepo, ref: gitRef, path: gitPath }),
      });
      if (!response.ok) throw new Error(await readError(response));
      const data = await response.json();
      const found: Candidate[] = data.skills ?? [];
      setCandidates(found);
      setSelected(
        Object.fromEntries(
          found.filter((candidate) => candidate.valid).map((candidate) => [candidate.id, true]),
        ),
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setGitScanning(false);
    }
  };

  const importSelected = async () => {
    setWorking(true);
    setError(null);
    try {
      if (candidateSource === "git") {
        const response = await fetch("/api/skills/import/git", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            repo_url: gitRepo,
            ref: gitRef,
            path: gitPath,
            selected_ids: Object.keys(selected).filter((id) => selected[id]),
          }),
        });
        if (!response.ok) throw new Error(await readError(response));
      } else {
        if (!file) return;
        const form = new FormData();
        form.append("file", file);
        for (const id of Object.keys(selected).filter((candidateID) => selected[candidateID])) {
          form.append("selected", id);
        }
        const response = await fetch("/api/skills/import/archive", { method: "POST", body: form });
        if (!response.ok) throw new Error(await readError(response));
      }
      setCandidates([]);
      setSelected({});
      setFile(null);
      await load(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setWorking(false);
    }
  };

  const setSkillEnabled = async (skill: Skill) => {
    const previous = skills;
    setActionSkillId(skill.id);
    setError(null);
    setSkills((current) =>
      current.map((item) => (item.id === skill.id ? { ...item, enabled: !skill.enabled } : item)),
    );
    try {
      const response = await fetch(
        `/api/skills/${encodeURIComponent(skill.id)}/${skill.enabled ? "disable" : "enable"}`,
        { method: "POST" },
      );
      if (!response.ok) throw new Error(await readError(response));
    } catch (cause) {
      setSkills(previous);
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setActionSkillId(null);
    }
  };

  const deleteSkill = async (skill: Skill) => {
    const previous = skills;
    setActionSkillId(skill.id);
    setError(null);
    setSkills((current) => current.filter((item) => item.id !== skill.id));
    try {
      const response = await fetch(`/api/skills/${encodeURIComponent(skill.id)}`, {
        method: "DELETE",
      });
      if (!response.ok) throw new Error(await readError(response));
    } catch (cause) {
      setSkills(previous);
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setActionSkillId(null);
    }
  };

  return (
    <WorkspacePageFrame>
      <WorkspacePageHeader
        action={
          <WorkspacePageAction isDisabled={working} onPress={() => fileInputRef.current?.click()}>
            <Upload className="size-4" />
            Upload zip
          </WorkspacePageAction>
        }
        description={`${skills.length} effective skills are currently loaded for this user.`}
        icon={<Sparkles className="size-5" />}
        title="Skills"
      />
      <input
        ref={fileInputRef}
        accept=".zip,application/zip"
        className="sr-only"
        type="file"
        onChange={chooseFile}
      />

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      <Card className="p-4">
        <Card.Header className="p-0">
          <Card.Title>Import from Git</Card.Title>
          <Card.Description>Scan a repository for SKILL.md packages.</Card.Description>
        </Card.Header>
        <Card.Content className="grid gap-3 p-0 lg:grid-cols-[minmax(0,1fr)_160px_180px_auto] lg:items-end">
          <TextField value={gitRepo} onChange={setGitRepo}>
            <Label>Repository</Label>
            <Input placeholder="owner/repository or Git URL" />
          </TextField>
          <TextField value={gitRef} onChange={setGitRef}>
            <Label>Ref</Label>
            <Input placeholder="main" />
          </TextField>
          <TextField value={gitPath} onChange={setGitPath}>
            <Label>Path</Label>
            <Input placeholder="skills/example" />
          </TextField>
          <Button
            isDisabled={gitScanning || working || !gitRepo.trim()}
            variant="outline"
            onPress={scanGit}
          >
            {gitScanning ? (
              <LoaderCircle className="size-4 animate-spin" />
            ) : (
              <GitBranch className="size-4" />
            )}
            {gitScanning ? "Scanning…" : "Scan repository"}
          </Button>
        </Card.Content>
      </Card>

      {candidates.length ? (
        <Card className="p-4">
          <Card.Header className="flex-row items-start justify-between gap-4 p-0">
            <span>
              <Card.Title>Candidates</Card.Title>
              <Card.Description>{validCandidates.length} valid packages found.</Card.Description>
            </span>
            <span className="flex gap-2">
              <Button
                size="sm"
                variant="outline"
                onPress={() =>
                  setSelected(
                    allValidSelected
                      ? {}
                      : Object.fromEntries(
                          validCandidates.map((candidate) => [candidate.id, true]),
                        ),
                  )
                }
              >
                {allValidSelected ? "Clear all" : "Select all"}
              </Button>
              <Button
                isDisabled={working || !Object.values(selected).some(Boolean)}
                size="sm"
                variant="primary"
                onPress={importSelected}
              >
                Import selected
              </Button>
            </span>
          </Card.Header>
          <Card.Content className="grid gap-3 p-0 md:grid-cols-2">
            {candidates.map((candidate) => (
              <div
                key={`${candidate.path}:${candidate.id}`}
                className="border-separator bg-surface-secondary flex items-start gap-3 rounded-2xl border p-3"
              >
                <Checkbox
                  className="mt-2"
                  isDisabled={!candidate.valid}
                  isSelected={Boolean(selected[candidate.id])}
                  onChange={(checked) =>
                    setSelected((current) => ({ ...current, [candidate.id]: checked }))
                  }
                />
                <SkillIcon name={displaySkillName(candidate) || candidate.id} size="sm" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-semibold">
                    {displaySkillName(candidate)}
                  </span>
                  <span className="text-muted mt-1 line-clamp-2 text-xs">
                    {candidate.description || candidate.errors?.join("; ")}
                  </span>
                  <span className="mt-2 flex flex-wrap gap-1.5">
                    <Chip color={candidate.valid ? "success" : "danger"} size="sm" variant="soft">
                      {candidate.valid ? "Valid" : "Invalid"}
                    </Chip>
                    <Chip size="sm" variant="soft">
                      {candidate.path}
                    </Chip>
                  </span>
                </span>
              </div>
            ))}
          </Card.Content>
        </Card>
      ) : null}

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <WorkspaceSectionHeader
          description={`${filteredSkills.length} workspace capabilities managed by the platform.`}
          title="Available skills"
        />
        <WorkspaceSearch
          placeholder="Search skills by name"
          value={skillQuery}
          onChange={(value) => {
            setSkillQuery(value);
            setSkillPage(1);
          }}
        />
      </div>

      {loading ? (
        <div className="grid min-h-48 place-items-center">
          <LoaderCircle className="text-muted size-5 animate-spin" />
        </div>
      ) : filteredSkills.length ? (
        <div className="grid gap-5">
          {visibleSharedSkills.length ? (
            <SkillSection
              actionSkillId={actionSkillId}
              skills={visibleSharedSkills}
              title="Shared skills"
              total={sharedSkills.length}
              onToggle={setSkillEnabled}
            />
          ) : null}
          {visiblePersonalSkills.length ? (
            <SkillSection
              actionSkillId={actionSkillId}
              skills={visiblePersonalSkills}
              title="My skills"
              total={personalSkills.length}
              onDelete={deleteSkill}
              onToggle={setSkillEnabled}
            />
          ) : null}
          <SkillPagination
            end={skillCatalogPage.end}
            page={skillCatalogPage.page}
            pageCount={skillCatalogPage.pageCount}
            start={skillCatalogPage.start}
            total={skillCatalogPage.total}
            onPageChange={setSkillPage}
          />
        </div>
      ) : (
        <EmptyState>
          <EmptyState.Header>
            <EmptyState.Media variant="icon">
              {skillQuery ? (
                <Search className="text-violet-500" />
              ) : (
                <Sparkles className="text-violet-500" />
              )}
            </EmptyState.Media>
            <EmptyState.Title>No skills found</EmptyState.Title>
            <EmptyState.Description>
              {skillQuery
                ? "Try another name or clear the search."
                : "No skills are available yet."}
            </EmptyState.Description>
          </EmptyState.Header>
          {skillQuery ? (
            <EmptyState.Content>
              <Button
                size="sm"
                variant="outline"
                onPress={() => {
                  setSkillQuery("");
                  setSkillPage(1);
                }}
              >
                Clear search
              </Button>
            </EmptyState.Content>
          ) : null}
        </EmptyState>
      )}
    </WorkspacePageFrame>
  );
}

function SkillSection({
  actionSkillId,
  skills,
  title,
  total,
  onDelete,
  onToggle,
}: {
  actionSkillId: string | null;
  skills: Skill[];
  title: string;
  total: number;
  onDelete?: (skill: Skill) => void;
  onToggle: (skill: Skill) => void;
}) {
  return (
    <section className="grid gap-3">
      <WorkspaceSectionHeader description={`${total} ${title.toLowerCase()}`} title={title} />
      <div className="cocola-web-catalog-grid grid items-stretch gap-3 md:grid-cols-2 xl:grid-cols-4">
        {skills.map((skill) => (
          <SkillCard
            key={skill.id}
            skill={skill}
            working={actionSkillId === skill.id}
            onDelete={onDelete ? () => onDelete(skill) : undefined}
            onToggle={() => onToggle(skill)}
          />
        ))}
      </div>
    </section>
  );
}

function SkillPagination({
  end,
  page,
  pageCount,
  start,
  total,
  onPageChange,
}: {
  end: number;
  page: number;
  pageCount: number;
  start: number;
  total: number;
  onPageChange: (page: number) => void;
}) {
  return (
    <nav
      aria-label="Skills pagination"
      className="flex flex-wrap items-center justify-between gap-3 pt-1"
    >
      <span className="text-muted text-xs tabular-nums">
        Showing {total === 0 ? 0 : start + 1}–{end} of {total}
      </span>
      <span className="flex items-center gap-2">
        <Tooltip delay={0}>
          <Button
            isIconOnly
            aria-label="Previous Skills page"
            isDisabled={page === 1}
            size="sm"
            variant="outline"
            onPress={() => onPageChange(Math.max(1, page - 1))}
          >
            <ChevronLeft className="size-4" />
          </Button>
          <Tooltip.Content>Previous page</Tooltip.Content>
        </Tooltip>
        <span className="text-muted min-w-14 text-center text-xs tabular-nums">
          {page} / {pageCount}
        </span>
        <Tooltip delay={0}>
          <Button
            isIconOnly
            aria-label="Next Skills page"
            isDisabled={page === pageCount}
            size="sm"
            variant="outline"
            onPress={() => onPageChange(Math.min(pageCount, page + 1))}
          >
            <ChevronRight className="size-4" />
          </Button>
          <Tooltip.Content>Next page</Tooltip.Content>
        </Tooltip>
      </span>
    </nav>
  );
}

function SkillCard({
  skill,
  working,
  onDelete,
  onToggle,
}: {
  skill: Skill;
  working: boolean;
  onDelete?: () => void;
  onToggle: () => void;
}) {
  return (
    <Card className="cocola-web-catalog-card cocola-web-skill-card h-full min-h-[13rem] p-4">
      <Card.Content className="flex h-full min-w-0 flex-col p-0">
        <Link
          className="group min-w-0 no-underline"
          href={`/skills/${encodeURIComponent(skill.id)}`}
        >
          <SkillIcon
            className="cocola-web-catalog-card-icon"
            name={displaySkillName(skill) || skill.id}
          />
          <span className="text-foreground mt-3 block truncate font-semibold">
            {displaySkillName(skill)}
          </span>
          <span className="text-muted mt-1 line-clamp-2 min-h-10 text-sm leading-5">
            {skill.description || "No description"}
          </span>
          <span className="mt-3 flex flex-wrap gap-1.5">
            <Chip color={skill.scope === "user" ? "accent" : "default"} size="sm" variant="soft">
              {skill.scope === "user" ? "Personal" : "Shared"}
            </Chip>
            {skill.file_count ? (
              <Chip size="sm" variant="soft">
                {skill.file_count} files
              </Chip>
            ) : null}
          </span>
        </Link>
        <div className="border-separator mt-auto flex items-center justify-end gap-2 border-t pt-3">
          {onDelete ? (
            <Button
              isIconOnly
              aria-label={`Remove ${displaySkillName(skill)}`}
              isDisabled={working}
              size="sm"
              variant="danger-soft"
              onPress={onDelete}
            >
              <Trash2 className="size-3.5" />
            </Button>
          ) : null}
          <Switch
            aria-label={`${skill.enabled ? "Disable" : "Enable"} ${displaySkillName(skill)}`}
            isDisabled={working}
            isSelected={skill.enabled}
            onChange={onToggle}
          >
            <Switch.Content>
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
            </Switch.Content>
          </Switch>
        </div>
      </Card.Content>
    </Card>
  );
}

async function readError(response: Response) {
  const text = await response.text();
  try {
    const json = JSON.parse(text);
    if (typeof json.error === "string") return json.error;
    if (json.error && typeof json.error === "object") {
      const message = typeof json.error.message === "string" ? json.error.message : "";
      const code = typeof json.error.code === "string" ? json.error.code : "";
      return message || code || text;
    }
    return json.message || text;
  } catch {
    return text || response.statusText;
  }
}

function displaySkillName(skill: Pick<Skill, "id" | "name" | "source_path">) {
  return skill.name?.trim() || skill.id;
}
