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
} from "lucide-react";

import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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

  const shared = skills.filter((skill) => skill.scope !== "user");
  const mine = skills.filter((skill) => skill.scope === "user");
  const validCandidates = useMemo(() => candidates.filter((c) => c.valid), [candidates]);
  const allValidSelected =
    validCandidates.length > 0 && validCandidates.every((candidate) => selected[candidate.id]);

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
    <main className="h-full min-w-0 flex-1 overflow-y-auto bg-background">
      <div className="mx-auto max-w-5xl space-y-8 px-8 py-10">
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-1">
            <h1 className="text-2xl font-bold tracking-tight">Skills</h1>
            <p className="text-sm text-muted-foreground">
              Choose shared skills and manage your personal skill packages.
            </p>
          </div>
          <label className="inline-flex h-9 cursor-pointer items-center gap-2 rounded-xl px-3.5 text-sm font-semibold text-white shadow-xs transition-opacity brand-gradient hover:opacity-90">
            <Upload className="size-4" />
            Upload zip
            <input
              type="file"
              accept=".zip,application/zip"
              className="hidden"
              onChange={chooseFile}
            />
          </label>
        </header>

        {error ? (
          <div className="rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-sm text-red-600">
            {error}
          </div>
        ) : null}

        {/* Import from Git */}
        <Card className="shadow-card">
          <div className="flex items-center gap-2 border-b border-border px-5 py-4">
            <GitBranch className="size-4 text-orange-500" />
            <h2 className="text-sm font-semibold">Import from Git</h2>
          </div>
          <div className="grid gap-4 p-5 md:grid-cols-[1fr_150px_150px_auto]">
            <div className="space-y-1.5">
              <Label htmlFor="git-repo">Repository</Label>
              <Input
                id="git-repo"
                placeholder="https://github.com/org/repo.git"
                value={gitRepo}
                onChange={(event) => setGitRepo(event.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="git-ref">Ref</Label>
              <Input
                id="git-ref"
                placeholder="branch / tag"
                value={gitRef}
                onChange={(event) => setGitRef(event.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="git-path">Path</Label>
              <Input
                id="git-path"
                placeholder="skills"
                value={gitPath}
                onChange={(event) => setGitPath(event.target.value)}
              />
            </div>
            <div className="flex items-end">
              <button
                className="inline-flex h-9 w-full items-center justify-center gap-2 rounded-xl border border-border bg-background px-4 text-sm font-medium shadow-xs transition-colors hover:bg-muted disabled:opacity-50"
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
        </Card>

        {/* Archive / Git candidates */}
        {candidates.length ? (
          <Card className="shadow-card">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-4">
              <div className="flex items-center gap-2">
                <FileArchive className="size-4 text-muted-foreground" />
                <h2 className="text-sm font-semibold">Candidates</h2>
                <Badge>{validCandidates.length} valid</Badge>
              </div>
              <div className="flex gap-2">
                <button
                  className="inline-flex h-8 items-center rounded-xl border border-border bg-background px-3 text-sm font-medium shadow-xs transition-colors hover:bg-muted"
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
                  className="inline-flex h-8 items-center rounded-xl px-3 text-sm font-semibold text-white shadow-xs transition-opacity brand-gradient hover:opacity-90 disabled:opacity-50"
                  disabled={working || !Object.values(selected).some(Boolean)}
                  onClick={importSelected}
                >
                  Import selected
                </button>
              </div>
            </div>
            <div className="grid gap-3 p-5 md:grid-cols-2">
              {candidates.map((candidate) => (
                <label
                  key={`${candidate.path}:${candidate.id}`}
                  className={cn(
                    "flex cursor-pointer gap-3 rounded-2xl border border-border p-4 transition-colors hover:bg-muted/50",
                    !candidate.valid && "cursor-not-allowed opacity-60",
                  )}
                >
                  <input
                    type="checkbox"
                    className="mt-1 size-4 shrink-0 accent-blue-600"
                    checked={!!selected[candidate.id]}
                    disabled={!candidate.valid}
                    onChange={(event) =>
                      setSelected((prev) => ({ ...prev, [candidate.id]: event.target.checked }))
                    }
                  />
                  <SkillIcon name={displaySkillName(candidate) || candidate.id} size="sm" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-bold">{displaySkillName(candidate)}</div>
                    <div className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                      {candidate.description || candidate.errors?.join("; ")}
                    </div>
                  </div>
                </label>
              ))}
            </div>
          </Card>
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
              working={working}
              actionSkillId={actionSkillId}
              onToggle={(skill) => setSkillEnabled(skill)}
            />
            <SkillSection
              title="My Skills"
              count={mine.length}
              empty="Upload a zip package to add your own skills."
              skills={mine}
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
  working,
  actionSkillId,
  onToggle,
  onDelete,
}: {
  title: string;
  count: number;
  empty: string;
  skills: Skill[];
  working: boolean;
  actionSkillId: string | null;
  onToggle: (skill: Skill) => void;
  onDelete?: (skill: Skill) => void;
}) {
  return (
    <section className="space-y-4">
      <div className="flex items-center gap-2">
        <h2 className="text-sm font-semibold">{title}</h2>
        <Badge>{count}</Badge>
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
          <label className="col-span-full flex min-h-[140px] cursor-pointer flex-col items-center justify-center gap-2 rounded-2xl border border-dashed border-border p-6 text-center transition-colors hover:border-foreground/25 hover:bg-muted/50">
            <div className="grid size-10 place-items-center rounded-xl bg-muted">
              <Upload className="size-4 text-muted-foreground" />
            </div>
            <p className="text-sm text-muted-foreground">{empty}</p>
          </label>
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
    <Card className="group flex flex-col p-5 shadow-card transition hover:-translate-y-0.5 hover:shadow-lg">
      <Link href={`/skills/${encodeURIComponent(skill.id)}`} className="block">
        <div className="flex items-start gap-3">
          <SkillIcon name={displaySkillName(skill) || skill.id} />
          <div className="min-w-0 flex-1">
            <h3 className="truncate text-sm font-bold">{displaySkillName(skill)}</h3>
            <p className="mt-1 line-clamp-2 min-h-10 text-sm text-muted-foreground">
              {skill.description || "No description"}
            </p>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <Badge>{skill.scope === "user" ? "personal" : "shared"}</Badge>
          {skill.enabled ? (
            <Badge variant="success">
              <span className="size-1.5 rounded-full bg-emerald-500" /> enabled
            </Badge>
          ) : null}
        </div>
      </Link>
      <div className="mt-5 flex items-center gap-2 border-t border-border pt-4">
        {skill.enabled ? (
          <button
            className="inline-flex h-8 flex-1 items-center justify-center gap-2 rounded-lg border border-border bg-background px-3 text-xs font-medium shadow-xs transition-colors hover:bg-muted disabled:opacity-50"
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
            className="inline-flex h-8 flex-1 items-center justify-center gap-2 rounded-lg px-3 text-xs font-semibold text-white shadow-xs transition-opacity brand-gradient hover:opacity-90 disabled:opacity-50"
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
            className="inline-flex size-8 items-center justify-center rounded-lg border border-border bg-background text-muted-foreground shadow-xs transition-colors hover:border-red-200 hover:bg-red-50 hover:text-red-600 disabled:opacity-50"
            disabled={working}
            onClick={onDelete}
            title="Remove"
          >
            <Trash2 className="size-3.5" />
          </button>
        ) : null}
      </div>
    </Card>
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
