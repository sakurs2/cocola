"use client";

import { ChangeEvent, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import {
  Check,
  FileArchive,
  GitBranch,
  LoaderCircle,
  Power,
  Search,
  Trash2,
  Upload,
  Sparkles,
  Zap,
  Users,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { SkillIcon } from "@/components/ui/skill-icon";

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
  return <SkillsWorkspace />;
}

function SkillsWorkspace() {
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

  const filteredSkills = useMemo(() => {
    const query = skillQuery.trim().toLowerCase();
    if (!query) return skills;
    return skills.filter((skill) => displaySkillName(skill).toLowerCase().includes(query));
  }, [skillQuery, skills]);
  const shared = filteredSkills.filter((skill) => skill.scope !== "user");
  const mine = filteredSkills.filter((skill) => skill.scope === "user");
  const searching = skillQuery.trim().length > 0;
  const validCandidates = useMemo(() => candidates.filter((c) => c.valid), [candidates]);
  const allValidSelected =
    validCandidates.length > 0 && validCandidates.every((candidate) => selected[candidate.id]);

  const totalCount = skills.length;
  const enabledCount = skills.filter((skill) => skill.enabled).length;
  const mineCount = skills.filter((skill) => skill.scope === "user").length;

  const load = async (showLoading = true) => {
    if (showLoading) setLoading(true);
    setError(null);
    try {
      const res = await fetch("/api/skills", { cache: "no-store" });
      if (!res.ok) throw new Error(await readError(res));
      const data = await res.json();
      setSkills(data.skills ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
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
      const res = await fetch("/api/skills/scan/archive", { method: "POST", body: form });
      if (!res.ok) throw new Error(await readError(res));
      const data = await res.json();
      const found: Candidate[] = data.skills ?? [];
      setCandidates(found);
      setSelected(Object.fromEntries(found.filter((c) => c.valid).map((c) => [c.id, true])));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setWorking(false);
    }
  };

  const importSelected = async () => {
    setWorking(true);
    setError(null);
    try {
      if (candidateSource === "git") {
        const res = await fetch("/api/skills/import/git", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            repo_url: gitRepo,
            ref: gitRef,
            path: gitPath,
            selected_ids: Object.keys(selected).filter((id) => selected[id]),
          }),
        });
        if (!res.ok) throw new Error(await readError(res));
      } else {
        if (!file) return;
        const form = new FormData();
        form.append("file", file);
        for (const id of Object.keys(selected).filter((id) => selected[id])) {
          form.append("selected", id);
        }
        const res = await fetch("/api/skills/import/archive", { method: "POST", body: form });
        if (!res.ok) throw new Error(await readError(res));
      }
      setCandidates([]);
      setSelected({});
      setFile(null);
      await load(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setWorking(false);
    }
  };

  const scanGit = async () => {
    setGitScanning(true);
    setError(null);
    setCandidateSource("git");
    setFile(null);
    try {
      const res = await fetch("/api/skills/scan/git", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ repo_url: gitRepo, ref: gitRef, path: gitPath }),
      });
      if (!res.ok) throw new Error(await readError(res));
      const data = await res.json();
      const found: Candidate[] = data.skills ?? [];
      setCandidates(found);
      setSelected(Object.fromEntries(found.filter((c) => c.valid).map((c) => [c.id, true])));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setGitScanning(false);
    }
  };

  const setSkillEnabled = async (skill: Skill) => {
    const previous = skills;
    setActionSkillId(skill.id);
    setWorking(true);
    setError(null);
    setSkills((current) =>
      current.map((item) => (item.id === skill.id ? { ...item, enabled: !skill.enabled } : item)),
    );
    try {
      const res = await fetch(
        `/api/skills/${encodeURIComponent(skill.id)}/${skill.enabled ? "disable" : "enable"}`,
        { method: "POST" },
      );
      if (!res.ok) throw new Error(await readError(res));
    } catch (err) {
      setSkills(previous);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActionSkillId(null);
      setWorking(false);
    }
  };

  const deleteSkill = async (skill: Skill) => {
    const previous = skills;
    setActionSkillId(skill.id);
    setWorking(true);
    setError(null);
    setSkills((current) => current.filter((item) => item.id !== skill.id));
    try {
      const res = await fetch(`/api/skills/${encodeURIComponent(skill.id)}`, { method: "DELETE" });
      if (!res.ok) throw new Error(await readError(res));
    } catch (err) {
      setSkills(previous);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActionSkillId(null);
      setWorking(false);
    }
  };

  return (
    <main className="user-canvas user-page user-theme-violet h-full min-w-0 flex-1 overflow-y-auto">
      <div className="mx-auto max-w-5xl space-y-6 px-8 py-10">
        <header className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3.5">
            <span className="user-page-icon">
              <Sparkles className="size-6" />
            </span>
            <div className="space-y-1">
              <div className="user-eyebrow">Extensions</div>
              <h1 className="text-2xl font-bold tracking-tight">Skills</h1>
              <p className="text-sm text-muted-foreground">
                Choose shared skills and manage your personal skill packages.
              </p>
            </div>
          </div>
          <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto">
            <label className="relative min-w-0 flex-1 sm:w-64 sm:flex-none">
              <span className="sr-only">Search Skills by name</span>
              <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <input
                type="search"
                value={skillQuery}
                onChange={(event) => setSkillQuery(event.target.value)}
                placeholder="input skill name"
                className="user-search-input h-11 w-full rounded-xl border border-border bg-white pl-9 pr-3.5 text-sm outline-none transition-[border-color,box-shadow]"
              />
            </label>
            <label className="user-accent-btn inline-flex h-11 cursor-pointer items-center gap-2 rounded-xl px-4 text-sm font-semibold">
              <Upload className="size-4" />
              Upload zip
              <input
                type="file"
                accept=".zip,application/zip"
                className="hidden"
                onChange={chooseFile}
              />
            </label>
          </div>
        </header>

        {error ? (
          <div className="rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-sm text-red-600">
            {error}
          </div>
        ) : null}

        {/* overview metrics */}
        <section className="grid gap-4 sm:grid-cols-3">
          <div className="user-metric-card" data-tone="violet">
            <div className="user-metric-head">
              <span className="user-metric-glyph">
                <Sparkles className="size-[22px]" />
              </span>
              <span className="user-metric-key">Total Skills</span>
            </div>
            <div className="user-metric-val">{totalCount}</div>
            <div className="user-metric-detail">Shared &amp; personal packages</div>
          </div>
          <div className="user-metric-card" data-tone="emerald">
            <div className="user-metric-head">
              <span className="user-metric-glyph">
                <Zap className="size-[22px]" />
              </span>
              <span className="user-metric-key">Enabled</span>
            </div>
            <div className="user-metric-val">{enabledCount}</div>
            <div className="user-metric-detail">Active in your sessions</div>
          </div>
          <div className="user-metric-card" data-tone="indigo">
            <div className="user-metric-head">
              <span className="user-metric-glyph">
                <Users className="size-[22px]" />
              </span>
              <span className="user-metric-key">My Skills</span>
            </div>
            <div className="user-metric-val">{mineCount}</div>
            <div className="user-metric-detail">Uploaded or imported by you</div>
          </div>
        </section>

        {/* Import from Git */}
        <div className="user-panel">
          <div className="mb-4 flex items-center gap-2.5">
            <span className="user-panel-glyph">
              <GitBranch className="size-[19px]" />
            </span>
            <div>
              <div className="text-sm font-bold text-foreground">Import from Git</div>
              <div className="mt-0.5 text-xs text-muted-foreground">
                Scan a repository for SKILL.md packages.
              </div>
            </div>
          </div>
          <div className="grid gap-3 md:grid-cols-[2fr_1fr_1fr_auto] md:items-end">
            <div className="space-y-1.5">
              <label
                htmlFor="git-repo"
                className="block text-[11px] font-bold uppercase tracking-wide text-muted-foreground"
              >
                Repository
              </label>
              <input
                id="git-repo"
                placeholder="https://github.com/org/repo.git"
                value={gitRepo}
                onChange={(event) => setGitRepo(event.target.value)}
                className="user-field-input h-[42px] w-full rounded-xl border border-border bg-white px-3 text-sm outline-none transition-[border-color,box-shadow]"
              />
            </div>
            <div className="space-y-1.5">
              <label
                htmlFor="git-ref"
                className="block text-[11px] font-bold uppercase tracking-wide text-muted-foreground"
              >
                Ref
              </label>
              <input
                id="git-ref"
                placeholder="branch / tag"
                value={gitRef}
                onChange={(event) => setGitRef(event.target.value)}
                className="user-field-input h-[42px] w-full rounded-xl border border-border bg-white px-3 text-sm outline-none transition-[border-color,box-shadow]"
              />
            </div>
            <div className="space-y-1.5">
              <label
                htmlFor="git-path"
                className="block text-[11px] font-bold uppercase tracking-wide text-muted-foreground"
              >
                Path
              </label>
              <input
                id="git-path"
                placeholder="skills"
                value={gitPath}
                onChange={(event) => setGitPath(event.target.value)}
                className="user-field-input h-[42px] w-full rounded-xl border border-border bg-white px-3 text-sm outline-none transition-[border-color,box-shadow]"
              />
            </div>
            <button
              className="user-tbtn user-tbtn--fill h-[42px] px-5"
              disabled={gitScanning || working || !gitRepo.trim()}
              onClick={scanGit}
            >
              {gitScanning ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Search className="size-4" />
              )}
              {gitScanning ? "Scanning" : "Scan"}
            </button>
          </div>
        </div>

        {/* Archive / Git candidates */}
        {candidates.length ? (
          <div className="user-panel">
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-2.5">
                <span className="user-panel-glyph">
                  <FileArchive className="size-[19px]" />
                </span>
                <div>
                  <div className="text-sm font-bold text-foreground">
                    Candidates · {validCandidates.length} valid
                  </div>
                  <div className="mt-0.5 text-xs text-muted-foreground">
                    Select packages to import.
                  </div>
                </div>
              </div>
              <div className="flex gap-2">
                <button
                  className="user-tbtn user-tbtn--ghost px-3.5"
                  onClick={() =>
                    setSelected(
                      allValidSelected
                        ? {}
                        : Object.fromEntries(validCandidates.map((c) => [c.id, true])),
                    )
                  }
                >
                  {allValidSelected ? "Clear all" : "Select all"}
                </button>
                <button
                  className="user-tbtn user-tbtn--fill px-3.5"
                  disabled={working || !Object.values(selected).some(Boolean)}
                  onClick={importSelected}
                >
                  Import selected
                </button>
              </div>
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              {candidates.map((candidate) => (
                <label
                  key={`${candidate.path}:${candidate.id}`}
                  className={cn(
                    "user-card user-card--hover cursor-pointer",
                    !candidate.valid && "cursor-not-allowed opacity-60",
                  )}
                >
                  <div className="flex items-start gap-3">
                    <input
                      type="checkbox"
                      className="mt-1 size-4 shrink-0 accent-[var(--page-accent)]"
                      checked={!!selected[candidate.id]}
                      disabled={!candidate.valid}
                      onChange={(event) =>
                        setSelected((prev) => ({ ...prev, [candidate.id]: event.target.checked }))
                      }
                    />
                    <SkillIcon name={displaySkillName(candidate) || candidate.id} size="sm" />
                    <div className="min-w-0 flex-1">
                      <div className="user-card-name truncate">{displaySkillName(candidate)}</div>
                      <div className="user-card-desc mt-1 line-clamp-2">
                        {candidate.description || candidate.errors?.join("; ")}
                      </div>
                    </div>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-1.5">
                    <span className="user-tag user-tag--accent">
                      {candidate.valid ? "valid" : "invalid"}
                    </span>
                    <span className="user-tag">{candidate.path}</span>
                  </div>
                </label>
              ))}
            </div>
          </div>
        ) : null}

        {loading ? (
          <div className="flex h-28 items-center justify-center text-muted-foreground">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            Loading skills
          </div>
        ) : (
          <>
            <SkillSection
              title="Shared Skills"
              count={shared.length}
              empty="No shared skills published by administrators."
              skills={shared}
              searching={searching}
              working={working}
              actionSkillId={actionSkillId}
              onToggle={(skill) => setSkillEnabled(skill)}
            />
            <SkillSection
              title="My Skills"
              count={mine.length}
              empty="Upload a zip package to add your own skills."
              skills={mine}
              searching={searching}
              working={working}
              actionSkillId={actionSkillId}
              onToggle={(skill) => setSkillEnabled(skill)}
              onDelete={(skill) => deleteSkill(skill)}
            />
          </>
        )}
      </div>
    </main>
  );
}

function SkillSection({
  title,
  count,
  empty,
  skills,
  searching,
  working,
  actionSkillId,
  onToggle,
  onDelete,
}: {
  title: string;
  count: number;
  empty: string;
  skills: Skill[];
  searching: boolean;
  working: boolean;
  actionSkillId: string | null;
  onToggle: (skill: Skill) => void;
  onDelete?: (skill: Skill) => void;
}) {
  return (
    <section className="space-y-4">
      <div className="flex items-center gap-2.5">
        <h2 className="user-section-title">{title}</h2>
        <span className="user-count-badge">{count}</span>
      </div>
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {skills.length ? (
          skills.map((skill) => (
            <SkillCard
              key={skill.id}
              skill={skill}
              working={working && actionSkillId === skill.id}
              onToggle={() => onToggle(skill)}
              onDelete={onDelete ? () => onDelete(skill) : undefined}
            />
          ))
        ) : (
          <div className="user-empty col-span-full">
            <div className="grid size-10 place-items-center rounded-xl bg-muted">
              {searching ? (
                <Search className="size-4 text-muted-foreground" />
              ) : (
                <Upload className="size-4 text-muted-foreground" />
              )}
            </div>
            <p className="text-sm text-muted-foreground">
              {searching ? "No skills match this name." : empty}
            </p>
          </div>
        )}
      </div>
    </section>
  );
}

function SkillCard({
  skill,
  working,
  onToggle,
  onDelete,
}: {
  skill: Skill;
  working: boolean;
  onToggle: () => void;
  onDelete?: () => void;
}) {
  return (
    <div className="user-card user-card--hover group">
      <Link href={`/skills/${encodeURIComponent(skill.id)}`} className="block">
        <div className="flex items-start gap-3">
          <SkillIcon name={displaySkillName(skill) || skill.id} />
          <div className="min-w-0 flex-1">
            <h3 className="user-card-name truncate">{displaySkillName(skill)}</h3>
            <p className="user-card-desc mt-1 line-clamp-2 min-h-10">
              {skill.description || "No description"}
            </p>
          </div>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          <span className={cn("user-tag", skill.scope === "user" && "user-tag--accent")}>
            {skill.scope === "user" ? "personal" : "shared"}
          </span>
          {skill.enabled ? (
            <span className="user-tag user-tag--ok">
              <span className="user-tag-dot" /> enabled
            </span>
          ) : null}
        </div>
      </Link>
      <div className="mt-4 flex items-center gap-2 border-t border-border/60 pt-4">
        {skill.enabled ? (
          <button
            className="user-tbtn user-tbtn--ghost flex-1"
            disabled={working}
            onClick={onToggle}
          >
            {working ? (
              <LoaderCircle className="size-3.5 animate-spin" />
            ) : (
              <Check className="size-3.5 text-emerald-600" />
            )}
            Enabled
          </button>
        ) : (
          <button
            className="user-tbtn user-tbtn--fill flex-1"
            disabled={working}
            onClick={onToggle}
          >
            {working ? (
              <LoaderCircle className="size-3.5 animate-spin" />
            ) : (
              <Power className="size-3.5" />
            )}
            Enable
          </button>
        )}
        {onDelete ? (
          <button
            className="user-iconbtn"
            disabled={working}
            onClick={onDelete}
            title="Remove"
          >
            <Trash2 className="size-3.5" />
          </button>
        ) : null}
      </div>
    </div>
  );
}

async function readError(res: Response) {
  const text = await res.text();
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
    return text || res.statusText;
  }
}

function displaySkillName(skill: Pick<Skill, "id" | "name" | "source_path">) {
  return skill.name?.trim() || "";
}
