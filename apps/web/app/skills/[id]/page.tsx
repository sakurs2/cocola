"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { ArrowLeft, FileText, LoaderCircle } from "lucide-react";

import { SkillIcon } from "@/components/ui/skill-icon";

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

export default function SkillDetailPage() {
  const { id } = useParams<{ id: string }>();
  return <SkillDetail id={id} />;
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
    <main className="user-canvas user-page user-theme-violet h-full min-w-0 flex-1 overflow-y-auto">
      <div className="mx-auto max-w-5xl space-y-6 px-8 py-10">
        <header className="flex items-center gap-3.5">
          <Link href="/skills" className="user-back-btn" title="Back">
            <ArrowLeft className="size-[17px]" />
          </Link>
          <div className="min-w-0 flex-1">
            <div className="user-eyebrow">Extensions</div>
            <h1 className="truncate text-2xl font-bold tracking-tight">
              {skill ? displaySkillName(skill) : "Skill"}
            </h1>
            <p className="user-card-mono truncate">{skill?.id || id}</p>
          </div>
        </header>

        {error ? (
          <div className="rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-sm text-red-600">
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
            <div className="user-card">
              <div className="flex items-start gap-4">
                <div className="user-card-glyph lg">
                  <SkillIcon name={displaySkillName(skill) || skill.id} />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-lg font-bold text-foreground">{displaySkillName(skill)}</h2>
                    {skill.enabled ? (
                      <span className="user-tag user-tag--ok">
                        <span className="user-tag-dot" /> enabled
                      </span>
                    ) : (
                      <span className="user-tag">disabled</span>
                    )}
                    <span
                      className={`user-tag${skill.scope === "user" ? " user-tag--accent" : ""}`}
                    >
                      {skill.scope === "user" ? "personal" : "shared"}
                    </span>
                  </div>
                  <p className="mt-2 text-sm text-muted-foreground">{skill.description}</p>
                </div>
              </div>
            </div>

            <section className="grid gap-3 md:grid-cols-2">
              <Info label="Source" value={skill.source_type || "manual"} />
              <Info label="Source Path" value={skill.source_path || "-"} />
              <Info label="Files" value={String(skill.file_count ?? 0)} />
              <Info label="Size" value={`${skill.size_bytes ?? 0} bytes`} />
              <Info label="SHA256" value={skill.content_sha256 || "-"} full />
            </section>

            <div className="user-doc-card">
              <div className="flex items-center gap-2 border-b border-border/60 px-5 py-4">
                <FileText className="size-4 text-muted-foreground" />
                <h2 className="text-sm font-bold text-foreground">SKILL.md</h2>
              </div>
              <pre className="max-h-[520px] overflow-auto whitespace-pre-wrap p-5 text-xs leading-6 text-slate-600">
                {skill.skill_md || "No SKILL.md captured."}
              </pre>
            </div>
          </>
        ) : null}
      </div>
    </main>
  );
}

function Info({ label, value, full }: { label: string; value: string; full?: boolean }) {
  return (
    <div className={`user-info-card${full ? " md:col-span-2" : ""}`}>
      <div className="text-[11px] font-bold uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className="mt-1.5 break-all text-sm font-semibold text-foreground">{value}</div>
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
