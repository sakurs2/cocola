"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, FileText, LoaderCircle } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
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
    <main className="h-full min-w-0 flex-1 overflow-y-auto bg-background">
      <div className="mx-auto max-w-5xl space-y-6 px-8 py-10">
        <header className="flex items-center gap-3">
          <Link
            href="/skills"
            className="grid size-9 shrink-0 place-items-center rounded-xl border border-border bg-background text-muted-foreground shadow-xs transition-colors hover:bg-muted hover:text-foreground"
            title="Back"
          >
            <ArrowLeft className="size-4" />
          </Link>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-2xl font-bold tracking-tight">
              {skill ? displaySkillName(skill) : "Skill"}
            </h1>
            <p className="truncate text-sm text-muted-foreground">{skill?.id || id}</p>
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
            <Card className="p-5 shadow-card">
              <div className="flex items-start gap-4">
                <SkillIcon name={displaySkillName(skill) || skill.id} />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-lg font-bold">{displaySkillName(skill)}</h2>
                    {skill.enabled ? (
                      <Badge variant="success">
                        <span className="size-1.5 rounded-full bg-emerald-500" /> enabled
                      </Badge>
                    ) : (
                      <Badge>disabled</Badge>
                    )}
                    <Badge>{skill.scope === "user" ? "personal" : "shared"}</Badge>
                  </div>
                  <p className="mt-2 text-sm text-muted-foreground">{skill.description}</p>
                </div>
              </div>
            </Card>

            <section className="grid gap-3 md:grid-cols-2">
              <Info label="Source" value={skill.source_type || "manual"} />
              <Info label="Source Path" value={skill.source_path || "-"} />
              <Info label="Files" value={String(skill.file_count ?? 0)} />
              <Info label="Size" value={`${skill.size_bytes ?? 0} bytes`} />
              <Info label="SHA256" value={skill.content_sha256 || "-"} />
            </section>

            <Card className="shadow-card">
              <div className="flex items-center gap-2 border-b border-border px-5 py-4">
                <FileText className="size-4 text-muted-foreground" />
                <h2 className="text-sm font-semibold">SKILL.md</h2>
              </div>
              <pre className="max-h-[520px] overflow-auto whitespace-pre-wrap p-5 text-xs leading-5">
                {skill.skill_md || "No SKILL.md captured."}
              </pre>
            </Card>
          </>
        ) : null}
      </div>
    </main>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <Card className="p-4 shadow-card">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 break-words text-sm font-medium">{value}</div>
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
