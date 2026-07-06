"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, FileText, LoaderCircle, Sparkles } from "lucide-react";

type Skill = {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  scope?: string;
  source_type?: string;
  source_path?: string;
  content_sha256?: string;
  file_count?: number;
  size_bytes?: number;
  skill_md?: string;
};

export default function SkillDetailPage({ params }: { params: { id: string } }) {
  return <SkillDetail id={params.id} />;
}

function SkillDetail({ id }: { id: string }) {
  const [skill, setSkill] = useState<Skill | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await fetch(`/api/skills/${encodeURIComponent(id)}`, { cache: "no-store" });
        if (!res.ok) throw new Error(await readError(res));
        const data = await res.json();
        if (!cancelled) setSkill(data);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [id]);

  return (
    <main className="h-full min-w-0 overflow-y-auto">
      <div className="mx-auto max-w-5xl space-y-5 px-6 py-6">
        <header className="flex items-center gap-3">
          <Link
            href="/skills"
            className="grid size-9 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            title="Back"
          >
            <ArrowLeft className="size-4" />
          </Link>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-xl font-semibold">
              {skill ? displaySkillName(skill) : "Skill"}
            </h1>
            <p className="truncate text-sm text-muted-foreground">{skill?.id || id}</p>
          </div>
        </header>

        {error ? (
          <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600">
            {error}
          </div>
        ) : null}

        {!skill && !error ? (
          <div className="flex h-40 items-center justify-center text-muted-foreground">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            Loading skill
          </div>
        ) : null}

        {skill ? (
          <>
            <section className="rounded-lg border border-border bg-card p-5">
              <div className="flex items-start gap-4">
                <div className="grid size-11 shrink-0 place-items-center rounded-md bg-muted">
                  <Sparkles className="size-5 text-muted-foreground" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-base font-semibold">{displaySkillName(skill)}</h2>
                    <span className="rounded-md border border-border px-2 py-0.5 text-xs text-muted-foreground">
                      {skill.enabled ? "enabled" : "disabled"}
                    </span>
                    <span className="rounded-md border border-border px-2 py-0.5 text-xs text-muted-foreground">
                      {skill.scope === "user" ? "personal" : "shared"}
                    </span>
                  </div>
                  <p className="mt-2 text-sm text-muted-foreground">{skill.description}</p>
                </div>
              </div>
            </section>

            <section className="grid gap-3 md:grid-cols-2">
              <Info label="Source" value={skill.source_type || "manual"} />
              <Info label="Source Path" value={skill.source_path || "-"} />
              <Info label="Files" value={String(skill.file_count ?? 0)} />
              <Info label="Size" value={`${skill.size_bytes ?? 0} bytes`} />
              <Info label="SHA256" value={skill.content_sha256 || "-"} />
            </section>

            <section className="rounded-lg border border-border bg-card">
              <div className="flex items-center gap-2 border-b border-border px-4 py-3">
                <FileText className="size-4 text-muted-foreground" />
                <h2 className="text-sm font-semibold">SKILL.md</h2>
              </div>
              <pre className="max-h-[520px] overflow-auto whitespace-pre-wrap p-4 text-xs leading-5">
                {skill.skill_md || "No SKILL.md captured."}
              </pre>
            </section>
          </>
        ) : null}
      </div>
    </main>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 break-words text-sm font-medium">{value}</div>
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
