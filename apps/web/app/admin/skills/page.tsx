"use client";

import {
  Sparkles as SkillsPageIcon,
} from "lucide-react";
import { ChangeEvent, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import {
  CheckCircle2,
  FileArchive,
  LoaderCircle,
  Sparkles,
  ToggleLeft,
  ToggleRight,
  Trash2,
  Upload,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { AdminPageHeader } from "@/components/admin/admin-ui";

type Skill = {
  id: string;
  name: string;
  description: string;
  version?: string;
  enabled: boolean;
  scope?: "admin" | "user" | string;
  source_type?: string;
  source_path?: string;
  file_count?: number;
  size_bytes?: number;
};

type Candidate = Skill & {
  path: string;
  valid: boolean;
  errors?: string[];
  warnings?: string[];
  content_sha256?: string;
};

export default function AdminSkillsPage() {
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

  const validCandidates = useMemo(() => candidates.filter((c) => c.valid), [candidates]);
  const allValidSelected =
    validCandidates.length > 0 && validCandidates.every((candidate) => selected[candidate.id]);

  const load = async (showLoading = true) => {
    if (showLoading) setLoading(true);
    setError(null);
    try {
      const res = await fetch("/api/admin/skills", { cache: "no-store" });
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
      const res = await fetch("/api/admin/skills/scan/archive", { method: "POST", body: form });
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
        const res = await fetch("/api/admin/skills/import/git", {
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
        const res = await fetch("/api/admin/skills/import/archive", { method: "POST", body: form });
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
      const res = await fetch("/api/admin/skills/scan/git", {
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
        `/api/admin/skills/${encodeURIComponent(skill.id)}/${skill.enabled ? "disable" : "enable"}`,
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
      const res = await fetch(`/api/admin/skills/${encodeURIComponent(skill.id)}`, {
        method: "DELETE",
      });
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
    <main className="mx-auto max-w-6xl space-y-6 px-6 py-6">
      <AdminPageHeader
        icon={<SkillsPageIcon className="size-[18px]" />}
        title="Skills"
        description="Publish shared skills for every user sandbox."
        actions={
          <label className="inline-flex h-9 cursor-pointer items-center gap-2 rounded-xl bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-within:ring-2 focus-within:ring-ring/40">
            <Upload className="size-4" />
            Upload zip
            <input
              type="file"
              accept=".zip,application/zip"
              className="hidden"
              onChange={chooseFile}
            />
          </label>
        }
      />

      {error ? (
        <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600">
          {error}
        </div>
      ) : null}

      <section className="rounded-lg border border-border bg-card p-4">
        <div className="mb-3 text-sm font-semibold">Import from Git</div>
        <div className="grid gap-3 md:grid-cols-[1fr_160px_160px_auto]">
          <input
            className="h-9 rounded-md border border-border bg-background px-3 text-sm outline-none focus:border-foreground/30"
            placeholder="https://github.com/org/repo.git"
            value={gitRepo}
            onChange={(event) => setGitRepo(event.target.value)}
          />
          <input
            className="h-9 rounded-md border border-border bg-background px-3 text-sm outline-none focus:border-foreground/30"
            placeholder="branch/tag"
            value={gitRef}
            onChange={(event) => setGitRef(event.target.value)}
          />
          <input
            className="h-9 rounded-md border border-border bg-background px-3 text-sm outline-none focus:border-foreground/30"
            placeholder="skills"
            value={gitPath}
            onChange={(event) => setGitPath(event.target.value)}
          />
          <button
            className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border px-3 text-sm hover:bg-accent disabled:opacity-50"
            disabled={gitScanning || working || !gitRepo.trim()}
            onClick={scanGit}
          >
            {gitScanning ? <LoaderCircle className="size-4 animate-spin" /> : null}
            {gitScanning ? "Scanning" : "Scan"}
          </button>
        </div>
      </section>

      {candidates.length ? (
        <section className="rounded-lg border border-border bg-card">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
            <div className="flex items-center gap-2">
              <FileArchive className="size-4 text-muted-foreground" />
              <div className="text-sm font-semibold">Archive candidates</div>
              <div className="text-xs text-muted-foreground">{validCandidates.length} valid</div>
            </div>
            <div className="flex gap-2">
              <button
                className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-accent"
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
                className="rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground disabled:opacity-50"
                disabled={working || !Object.values(selected).some(Boolean)}
                onClick={importSelected}
              >
                Import selected
              </button>
            </div>
          </div>
          <div className="grid gap-3 p-4 md:grid-cols-2">
            {candidates.map((candidate) => (
              <label
                key={`${candidate.path}:${candidate.id}`}
                className={cn(
                  "rounded-lg border p-4",
                  candidate.valid ? "border-border" : "border-red-500/30 bg-red-500/5",
                )}
              >
                <div className="flex gap-3">
                  <input
                    type="checkbox"
                    className="mt-1"
                    checked={!!selected[candidate.id]}
                    disabled={!candidate.valid}
                    onChange={(event) =>
                      setSelected((prev) => ({ ...prev, [candidate.id]: event.target.checked }))
                    }
                  />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-semibold">
                      {displaySkillName(candidate)}
                    </div>
                    <div className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                      {candidate.description || candidate.errors?.join("; ")}
                    </div>
                    <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted-foreground">
                      <span>{candidate.path || "."}</span>
                      <span>{candidate.file_count ?? 0} files</span>
                    </div>
                  </div>
                </div>
              </label>
            ))}
          </div>
        </section>
      ) : null}

      <section className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {loading ? (
          <div className="col-span-full flex h-28 items-center justify-center text-muted-foreground">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            Loading skills
          </div>
        ) : skills.length ? (
          skills.map((skill) => (
            <SkillCard
              key={skill.id}
              skill={skill}
              href={`/admin/skills/${encodeURIComponent(skill.id)}`}
              onToggle={() => setSkillEnabled(skill)}
              onDelete={() => deleteSkill(skill)}
              working={working && actionSkillId === skill.id}
            />
          ))
        ) : (
          <div className="col-span-full rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
            No skills imported yet.
          </div>
        )}
      </section>
    </main>
  );
}

function SkillCard({
  skill,
  href,
  onToggle,
  onDelete,
  working,
}: {
  skill: Skill;
  href: string;
  onToggle: () => void;
  onDelete: () => void;
  working: boolean;
}) {
  return (
    <div className="rounded-lg border border-border bg-card p-4 transition hover:border-foreground/20">
      <Link href={href} className="block">
        <div className="flex items-start gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-md bg-muted">
            <Sparkles className="size-4 text-muted-foreground" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">{displaySkillName(skill)}</div>
            <p className="mt-1 line-clamp-2 min-h-10 text-sm text-muted-foreground">
              {skill.description || "No description"}
            </p>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap gap-2 text-xs text-muted-foreground">
          <span className="rounded-md border border-border px-2 py-0.5">{skill.id}</span>
          <span className="rounded-md border border-border px-2 py-0.5">
            {skill.source_type || "manual"}
          </span>
          {skill.enabled ? (
            <span className="inline-flex items-center gap-1 rounded-md border border-emerald-500/30 px-2 py-0.5 text-emerald-600">
              <CheckCircle2 className="size-3" />
              enabled
            </span>
          ) : null}
        </div>
      </Link>
      <div className="mt-4 flex gap-2">
        <button
          className="inline-flex h-8 items-center gap-2 rounded-md border border-border px-2.5 text-sm hover:bg-accent disabled:opacity-50"
          disabled={working}
          onClick={onToggle}
        >
          {skill.enabled ? <ToggleRight className="size-4" /> : <ToggleLeft className="size-4" />}
          {skill.enabled ? "Disable" : "Enable"}
        </button>
        <button
          className="inline-flex h-8 items-center gap-2 rounded-md border border-border px-2.5 text-sm text-red-600 hover:bg-red-500/10 disabled:opacity-50"
          disabled={working}
          onClick={onDelete}
        >
          <Trash2 className="size-4" />
          Remove
        </button>
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
