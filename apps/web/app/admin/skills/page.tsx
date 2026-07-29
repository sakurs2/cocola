"use client";

import { Sparkles as SkillsPageIcon } from "lucide-react";
import {
  ChangeEvent,
  useCallback,
  useEffect,
  useMemo,
  useState,
  type CSSProperties,
  type ComponentType,
  type ReactNode,
} from "react";
import Link from "next/link";
import {
  Blocks,
  Bot,
  Boxes,
  Braces,
  CheckCircle2,
  Compass,
  FileArchive,
  Gem,
  LoaderCircle,
  Power,
  Search,
  Puzzle,
  Rocket,
  Sparkles,
  ToggleLeft,
  ToggleRight,
  Trash2,
  Upload,
  Wand2,
  Zap,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { AdminPagination } from "@/components/admin/admin-ui";

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

const SKILLS_PAGE_SIZE = 24;

// A small curated palette + icon set so each skill tile reads as distinct
// while still living inside the amber page theme.
const GLYPH_ICONS: ComponentType<{ className?: string }>[] = [
  Sparkles,
  Puzzle,
  Blocks,
  Wand2,
  Zap,
  Boxes,
  Rocket,
  Gem,
  Compass,
  Braces,
  Bot,
];

const GLYPH_TONES: { ink: string; soft: string; ring: string }[] = [
  { ink: "#d97706", soft: "#fef3c7", ring: "#fcd34d" }, // amber
  { ink: "#ea580c", soft: "#ffedd5", ring: "#fdba74" }, // orange
  { ink: "#7c3aed", soft: "#ede9fe", ring: "#c4b5fd" }, // violet
  { ink: "#2563eb", soft: "#dbeafe", ring: "#93c5fd" }, // blue
  { ink: "#0d9488", soft: "#ccfbf1", ring: "#5eead4" }, // teal
  { ink: "#16a34a", soft: "#dcfce7", ring: "#86efac" }, // green
  { ink: "#db2777", soft: "#fce7f3", ring: "#f9a8d4" }, // pink
  { ink: "#0891b2", soft: "#cffafe", ring: "#67e8f9" }, // cyan
  { ink: "#c026d3", soft: "#fae8ff", ring: "#f0abfc" }, // fuchsia
  { ink: "#4f46e5", soft: "#e0e7ff", ring: "#a5b4fc" }, // indigo
];

function hashString(value: string): number {
  let hash = 0;
  for (let i = 0; i < value.length; i += 1) {
    hash = (hash * 31 + value.charCodeAt(i)) | 0;
  }
  return Math.abs(hash);
}

function glyphFor(id: string) {
  const h = hashString(id || "skill");
  const Icon = GLYPH_ICONS[h % GLYPH_ICONS.length] ?? Sparkles;
  const tone =
    GLYPH_TONES[h % GLYPH_TONES.length] ??
    ({ ink: "#d97706", soft: "#fef3c7", ring: "#fcd34d" } as const);
  const style = {
    "--glyph-ink": tone.ink,
    "--glyph-soft": tone.soft,
    "--glyph-ring": tone.ring,
  } as CSSProperties;
  return { Icon, style };
}

export default function AdminSkillsPage() {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [page, setPage] = useState(0);
  const [total, setTotal] = useState(0);
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
  const [search, setSearch] = useState("");
  const [error, setError] = useState<string | null>(null);

  const validCandidates = useMemo(() => candidates.filter((c) => c.valid), [candidates]);

  const searching = search.trim().length > 0;
  const visibleSkills = useMemo(() => {
    const needle = search.trim().toLowerCase();
    if (!needle) return skills;
    return skills.filter((skill) =>
      [displaySkillName(skill), skill.id].join(" ").toLowerCase().includes(needle),
    );
  }, [skills, search]);
  const allValidSelected =
    validCandidates.length > 0 && validCandidates.every((candidate) => selected[candidate.id]);

  const stats = useMemo(
    () => ({
      total,
      enabled: skills.filter((s) => s.enabled).length,
      disabled: skills.filter((s) => !s.enabled).length,
    }),
    [skills, total],
  );

  const load = useCallback(
    async (showLoading = true) => {
      if (showLoading) setLoading(true);
      setError(null);
      const searchingNow = search.trim().length > 0;
      const query = new URLSearchParams(
        searchingNow
          ? { limit: "1000", offset: "0" }
          : {
              limit: String(SKILLS_PAGE_SIZE),
              offset: String(page * SKILLS_PAGE_SIZE),
            },
      );
      try {
        const res = await fetch(`/api/admin/skills?${query}`, { cache: "no-store" });
        if (!res.ok) throw new Error(await readError(res));
        const data = (await res.json()) as { skills?: Skill[]; total?: number };
        setSkills(Array.isArray(data.skills) ? data.skills : []);
        setTotal(typeof data.total === "number" ? data.total : 0);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        if (showLoading) setLoading(false);
      }
    },
    [page, search],
  );

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    const lastPage = Math.max(0, Math.ceil(total / SKILLS_PAGE_SIZE) - 1);
    if (page > lastPage) setPage(lastPage);
  }, [page, total]);

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
      await load(false);
    } catch (err) {
      setSkills(previous);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setActionSkillId(null);
      setWorking(false);
    }
  };

  return (
    <main className="admin-theme-amber min-h-screen bg-background text-foreground">
      <div className="mx-auto w-full max-w-[100rem] space-y-6 px-4 py-5 sm:px-6 sm:py-6">
        {/* Header */}
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-3">
            <span className="admin-entity-glyph">
              <SkillsPageIcon className="size-[20px]" />
            </span>
            <div>
              <h1 className="text-xl font-semibold tracking-[-0.02em] text-foreground sm:text-2xl">
                Skills
              </h1>
              <p className="mt-0.5 text-sm text-muted-foreground">
                Publish shared skills for every user sandbox.
              </p>
            </div>
          </div>
          <label className="admin-primary-btn inline-flex h-10 cursor-pointer items-center gap-2 rounded-xl px-4 text-sm font-medium">
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

        {error ? (
          <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600">
            {error}
          </div>
        ) : null}

        {/* Metric cards */}
        <section className="grid gap-4 md:grid-cols-3">
          <MetricCard
            tone="amber"
            label="Skills"
            value={stats.total}
            icon={<Sparkles className="size-[22px]" />}
          />
          <MetricCard
            tone="green"
            label="Enabled"
            value={stats.enabled}
            icon={<CheckCircle2 className="size-[22px]" />}
          />
          <MetricCard
            tone="slate"
            label="Disabled"
            value={stats.disabled}
            icon={<Power className="size-[22px]" />}
          />
        </section>

        {/* Import from Git */}
        <section className="admin-entity-card">
          <div className="mb-3 text-sm font-semibold">Import from Git</div>
          <div className="grid gap-3 md:grid-cols-[1fr_160px_160px_auto]">
            <input
              className="admin-drawer-form h-10 rounded-xl border border-input bg-background px-3 text-sm outline-none focus:border-ring"
              placeholder="https://github.com/org/repo.git"
              value={gitRepo}
              onChange={(event) => setGitRepo(event.target.value)}
            />
            <input
              className="admin-drawer-form h-10 rounded-xl border border-input bg-background px-3 text-sm outline-none focus:border-ring"
              placeholder="branch/tag"
              value={gitRef}
              onChange={(event) => setGitRef(event.target.value)}
            />
            <input
              className="admin-drawer-form h-10 rounded-xl border border-input bg-background px-3 text-sm outline-none focus:border-ring"
              placeholder="skills"
              value={gitPath}
              onChange={(event) => setGitPath(event.target.value)}
            />
            <button
              className="admin-primary-btn inline-flex h-10 items-center justify-center gap-2 rounded-xl px-4 text-sm font-medium disabled:opacity-50"
              disabled={gitScanning || working || !gitRepo.trim()}
              onClick={scanGit}
            >
              {gitScanning ? <LoaderCircle className="size-4 animate-spin" /> : null}
              {gitScanning ? "Scanning" : "Scan"}
            </button>
          </div>
        </section>

        {candidates.length ? (
          <section className="admin-entity-card p-0">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border/60 px-4 py-3">
              <div className="flex items-center gap-2">
                <FileArchive className="size-4 text-muted-foreground" />
                <div className="text-sm font-semibold">Archive candidates</div>
                <div className="text-xs text-muted-foreground">{validCandidates.length} valid</div>
              </div>
              <div className="flex gap-2">
                <button
                  className="admin-card-btn"
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
                  className="admin-primary-btn rounded-full px-4 py-1.5 text-sm font-medium disabled:opacity-50"
                  disabled={working || !Object.values(selected).some(Boolean)}
                  onClick={importSelected}
                >
                  Import selected
                </button>
              </div>
            </div>
            <div className="grid gap-3 p-4 md:grid-cols-2">
              {candidates.map((candidate) => {
                const { Icon, style } = glyphFor(candidate.id);
                return (
                  <label
                    key={`${candidate.path}:${candidate.id}`}
                    className={cn(
                      "flex cursor-pointer gap-3 rounded-2xl p-4 transition-shadow",
                      candidate.valid
                        ? "bg-white shadow-[0_1px_0_0_rgba(0,0,0,0.02),0_8px_24px_-14px_rgba(0,0,0,0.16)] hover:shadow-[0_12px_30px_-16px_rgba(0,0,0,0.22)]"
                        : "border border-red-500/30 bg-red-500/5",
                    )}
                  >
                    <input
                      type="checkbox"
                      className="mt-1 size-4 accent-primary"
                      checked={!!selected[candidate.id]}
                      disabled={!candidate.valid}
                      onChange={(event) =>
                        setSelected((prev) => ({ ...prev, [candidate.id]: event.target.checked }))
                      }
                    />
                    <div className="admin-entity-glyph" style={style}>
                      <Icon className="size-[18px]" />
                    </div>
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
                  </label>
                );
              })}
            </div>
          </section>
        ) : null}

        <div className="flex items-center justify-between gap-3">
          <label className="admin-entity-search">
            <Search className="size-4" />
            <input
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setPage(0);
              }}
              placeholder="Search skills by name"
            />
          </label>
          {searching ? (
            <span className="text-xs text-muted-foreground">
              {visibleSkills.length} match{visibleSkills.length === 1 ? "" : "es"}
            </span>
          ) : null}
        </div>

        <section className="admin-entity-grid md:grid-cols-2 xl:grid-cols-3">
          {loading ? (
            <div className="col-span-full flex h-28 items-center justify-center text-muted-foreground">
              <LoaderCircle className="mr-2 size-4 animate-spin" />
              Loading skills
            </div>
          ) : visibleSkills.length ? (
            visibleSkills.map((skill) => (
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
            <div className="col-span-full rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
              {searching ? `No skills match "${search.trim()}".` : "No skills imported yet."}
            </div>
          )}
        </section>
        {searching ? null : (
          <AdminPagination
            page={page}
            pageSize={SKILLS_PAGE_SIZE}
            count={skills.length}
            total={total}
            loading={loading}
            label="skills"
            onPageChange={setPage}
          />
        )}
      </div>
    </main>
  );
}

function MetricCard({
  tone,
  label,
  value,
  icon,
}: {
  tone: "amber" | "green" | "slate";
  label: string;
  value: number;
  icon: ReactNode;
}) {
  return (
    <div className="admin-metric-card" data-tone={tone}>
      <div className="admin-metric-head">
        <span className="admin-metric-glyph">{icon}</span>
        <span className="admin-metric-key">{label}</span>
      </div>
      <div className="admin-metric-val">{value}</div>
    </div>
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
  const { Icon, style } = glyphFor(skill.id);
  return (
    <div className="admin-entity-card admin-entity-card--hover">
      <Link href={href} className="block">
        <div className="flex items-start gap-3">
          <div className="admin-entity-glyph" style={style}>
            <Icon className="size-[18px]" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">{displaySkillName(skill)}</div>
            <p className="mt-1 line-clamp-2 min-h-10 text-sm text-muted-foreground">
              {skill.description || "No description"}
            </p>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap gap-2">
          <span className="admin-entity-tag">{skill.id}</span>
          <span className="admin-entity-tag">{skill.source_type || "manual"}</span>
          {skill.enabled ? (
            <span className="admin-chip admin-chip--ok">
              <CheckCircle2 />
              enabled
            </span>
          ) : (
            <span className="admin-chip admin-chip--off">
              <span className="admin-chip-dot" />
              disabled
            </span>
          )}
        </div>
      </Link>
      <div className="mt-auto flex gap-2 pt-4">
        <button className="admin-card-btn" disabled={working} onClick={onToggle}>
          {working ? (
            <LoaderCircle className="size-4 animate-spin" />
          ) : skill.enabled ? (
            <ToggleRight className="size-4" />
          ) : (
            <ToggleLeft className="size-4" />
          )}
          {skill.enabled ? "Disable" : "Enable"}
        </button>
        <button
          className="admin-card-btn admin-card-btn--danger"
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
