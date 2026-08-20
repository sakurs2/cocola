"use client";

import { Sparkles as SkillsPageIcon } from "lucide-react";
import {
  ChangeEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ComponentType,
} from "react";
import {
  Button,
  Card,
  Checkbox,
  Chip,
  Input,
  Label,
  SearchField,
  Switch,
  TextField,
} from "@heroui/react";
import Link from "next/link";
import { useTranslations } from "next-intl";
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
  Search,
  Puzzle,
  Rocket,
  Sparkles,
  Trash2,
  Upload,
  Wand2,
  Zap,
} from "lucide-react";
import {
  AdminConfirmDialog,
  AdminErrorDialog,
  AdminPage,
  AdminPageHeader,
  AdminPagination,
} from "@/components/admin/admin-ui";

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
  const t = useTranslations("admin.skillsPage");
  const skillsT = useTranslations("skills");
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
  const [deleteTarget, setDeleteTarget] = useState<Skill | null>(null);
  const [search, setSearch] = useState("");
  const [error, setError] = useState<string | null>(null);
  const uploadInputRef = useRef<HTMLInputElement>(null);

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
      setDeleteTarget(null);
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
    <AdminPage>
      <AdminPageHeader
        icon={<SkillsPageIcon className="size-5" />}
        title={t("title")}
        description={t("description")}
        actions={
          <Button isDisabled={working} onPress={() => uploadInputRef.current?.click()}>
            <Upload className="size-4" />
            {skillsT("uploadZip")}
          </Button>
        }
      />
      <input
        ref={uploadInputRef}
        type="file"
        accept=".zip,application/zip"
        className="sr-only"
        onChange={chooseFile}
      />

      <AdminErrorDialog error={error} onDismiss={() => setError(null)} />

      <Card className="p-5">
        <Card.Header className="p-0">
          <Card.Title>{skillsT("importGit")}</Card.Title>
          <Card.Description>{skillsT("importGitDescription")}</Card.Description>
        </Card.Header>
        <Card.Content className="mt-5 grid gap-3 p-0 lg:grid-cols-[minmax(0,1fr)_160px_180px_auto] lg:items-end">
          <TextField value={gitRepo} variant="secondary" onChange={setGitRepo}>
            <Label>{skillsT("repository")}</Label>
            <Input placeholder={skillsT("repositoryPlaceholder")} />
          </TextField>
          <TextField value={gitRef} variant="secondary" onChange={setGitRef}>
            <Label>{skillsT("ref")}</Label>
            <Input placeholder="main" />
          </TextField>
          <TextField value={gitPath} variant="secondary" onChange={setGitPath}>
            <Label>{skillsT("path")}</Label>
            <Input placeholder="skills/example" />
          </TextField>
          <Button
            isDisabled={gitScanning || working || !gitRepo.trim()}
            variant="outline"
            onPress={() => void scanGit()}
          >
            {gitScanning ? <LoaderCircle className="size-4 animate-spin" /> : null}
            {gitScanning ? skillsT("scanning") : skillsT("scanRepository")}
          </Button>
        </Card.Content>
      </Card>

      {candidates.length ? (
        <Card className="p-5">
          <Card.Header className="flex-row flex-wrap items-center justify-between gap-3 p-0">
            <div className="flex items-center gap-2">
              <FileArchive className="size-4 text-muted" />
              <div className="text-sm font-semibold">{t("candidateTitle")}</div>
              <div className="text-xs text-muted">
                {t("valid", { count: validCandidates.length })}
              </div>
            </div>
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="outline"
                onPress={() =>
                  setSelected(
                    allValidSelected
                      ? {}
                      : Object.fromEntries(validCandidates.map((c) => [c.id, true])),
                  )
                }
              >
                {allValidSelected ? skillsT("clearAll") : skillsT("selectAll")}
              </Button>
              <Button
                size="sm"
                isDisabled={working || !Object.values(selected).some(Boolean)}
                onPress={() => void importSelected()}
              >
                {skillsT("importSelected")}
              </Button>
            </div>
          </Card.Header>
          <Card.Content className="mt-4 grid gap-3 p-0 md:grid-cols-2">
            {candidates.map((candidate) => {
              const { Icon, style } = glyphFor(candidate.id);
              return (
                <label
                  key={`${candidate.path}:${candidate.id}`}
                  className={`flex cursor-pointer gap-3 rounded-2xl p-4 transition-shadow ${candidate.valid ? "bg-surface-secondary hover:shadow-lg" : "border-danger/30 bg-danger/5 border"}`}
                >
                  <Checkbox
                    className="mt-1"
                    isDisabled={!candidate.valid}
                    isSelected={Boolean(selected[candidate.id])}
                    onChange={(checked) =>
                      setSelected((current) => ({ ...current, [candidate.id]: checked }))
                    }
                  />
                  <div className="admin-entity-glyph" style={style}>
                    <Icon className="size-[18px]" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-semibold">
                      {displaySkillName(candidate)}
                    </div>
                    <div className="mt-1 line-clamp-2 text-sm text-muted">
                      {candidate.description || candidate.errors?.join("; ")}
                    </div>
                    <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted">
                      <span>{candidate.path || "."}</span>
                      <span>{skillsT("files", { count: candidate.file_count ?? 0 })}</span>
                    </div>
                  </div>
                </label>
              );
            })}
          </Card.Content>
        </Card>
      ) : null}

      <div className="flex items-center justify-between gap-3">
        <SearchField
          aria-label={skillsT("search")}
          className="w-full max-w-sm"
          value={search}
          onChange={(value) => {
            setSearch(value);
            setPage(0);
          }}
        >
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder={skillsT("search")} />
            <SearchField.ClearButton />
          </SearchField.Group>
        </SearchField>
        {searching ? (
          <span className="text-xs text-muted">
            {t("matches", { count: visibleSkills.length })}
          </span>
        ) : null}
      </div>

      <section className="cocola-admin-catalog-grid cocola-resource-card-grid">
        {loading ? (
          <div className="cocola-resource-card-grid-full flex h-28 items-center justify-center text-muted">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            {t("loading")}
          </div>
        ) : visibleSkills.length ? (
          visibleSkills.map((skill) => (
            <SkillCard
              key={skill.id}
              skill={skill}
              href={`/admin/skills/${encodeURIComponent(skill.id)}`}
              onToggle={() => setSkillEnabled(skill)}
              onDelete={() => {
                setError(null);
                setDeleteTarget(skill);
              }}
              working={actionSkillId === skill.id}
            />
          ))
        ) : (
          <Card className="cocola-resource-card-grid-full border-separator border border-dashed p-8 text-center text-sm text-muted">
            {searching ? t("noMatch", { query: search.trim() }) : t("empty")}
          </Card>
        )}
      </section>
      {searching ? null : (
        <AdminPagination
          page={page}
          pageSize={SKILLS_PAGE_SIZE}
          count={skills.length}
          total={total}
          loading={loading}
          label={t("pagination")}
          onPageChange={setPage}
        />
      )}
      <AdminConfirmDialog
        busy={deleteTarget !== null && working && actionSkillId === deleteTarget.id}
        confirmLabel={skillsT("remove")}
        description={t("removeDescription", {
          name: deleteTarget ? displaySkillName(deleteTarget) : t("thisSkill"),
        })}
        destructive
        error={error}
        open={deleteTarget !== null}
        title={skillsT("removeTitle")}
        onConfirm={() => {
          if (deleteTarget) void deleteSkill(deleteTarget);
        }}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null);
            setError(null);
          }
        }}
      />
    </AdminPage>
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
  const t = useTranslations("admin.skillsPage");
  const skillsT = useTranslations("skills");
  const { Icon, style } = glyphFor(skill.id);
  return (
    <Card className="admin-skill-card min-h-64 p-5">
      <Link href={href} className="block min-w-0">
        <div className="flex items-start gap-3">
          <div className="admin-entity-glyph admin-skill-card-icon" style={style}>
            <Icon className="size-[18px]" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">{displaySkillName(skill)}</div>
            <p className="mt-1 line-clamp-2 min-h-10 text-sm text-muted">
              {skill.description || skillsT("noDescription")}
            </p>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap gap-2">
          <Chip size="sm" variant="soft">
            {skill.id}
          </Chip>
          <Chip size="sm" variant="soft">
            {skill.source_type || t("manual")}
          </Chip>
        </div>
      </Link>
      <div className="mt-auto flex items-center justify-between gap-3 pt-4">
        <Switch
          aria-label={
            skill.enabled
              ? skillsT("disableAria", { name: displaySkillName(skill) })
              : skillsT("enableAria", { name: displaySkillName(skill) })
          }
          isDisabled={working}
          isSelected={skill.enabled}
          onChange={onToggle}
        >
          <Switch.Content>
            <Switch.Control>
              <Switch.Thumb className="admin-switch-thumb shadow-sm" />
            </Switch.Control>
          </Switch.Content>
        </Switch>
        <Button size="sm" variant="danger-soft" isDisabled={working} onPress={onDelete}>
          <Trash2 className="size-4" />
          {skillsT("remove")}
        </Button>
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
