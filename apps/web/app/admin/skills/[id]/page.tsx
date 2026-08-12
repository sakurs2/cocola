"use client";

import { Button, Card, Chip } from "@heroui/react";
import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { ArrowLeft, FileText, Folder, Hash, LoaderCircle } from "lucide-react";
import { SkillIcon } from "@/components/ui/skill-icon";
import { AdminErrorDialog } from "@/components/admin/admin-ui";

type Skill = {
  id: string;
  name: string;
  description: string;
  version?: string;
  enabled: boolean;
  scope?: string;
  owner_user_id?: string;
  source_type?: string;
  source_path?: string;
  content_sha256?: string;
  file_count?: number;
  size_bytes?: number;
  skill_md?: string;
  manifest_json?: unknown;
};

export default function AdminSkillDetailPage() {
  const t = useTranslations("skills.detail");
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [skill, setSkill] = useState<Skill | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await fetch(`/api/admin/skills/${encodeURIComponent(id)}`, {
          cache: "no-store",
        });
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
    <main className="admin-theme-amber min-h-screen bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
        <header className="flex flex-wrap items-center gap-3">
          <Button
            isIconOnly
            aria-label={t("back")}
            variant="ghost"
            onPress={() => router.push("/admin/skills")}
          >
            <ArrowLeft className="size-4" />
          </Button>
          <span className="bg-accent-soft text-accent flex size-11 items-center justify-center rounded-2xl">
            <SkillIcon name={skill?.name || id} size="sm" />
          </span>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-2xl font-semibold tracking-[-0.03em]">
              {skill ? displaySkillName(skill) : t("fallback")}
            </h1>
            <p className="text-muted mt-1 truncate text-sm">
              {skill?.description || skill?.id || id}
            </p>
          </div>
        </header>

        <AdminErrorDialog error={error} onDismiss={() => setError(null)} />

        {!skill && !error ? (
          <div className="text-muted flex h-40 items-center justify-center">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            {t("loading")}
          </div>
        ) : null}

        {skill ? (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <Chip size="sm" variant="soft">
                {skill.scope || t("adminScope")}
              </Chip>
              <Chip size="sm" variant="soft">
                {skill.source_type || t("manual")}
              </Chip>
              <Chip color={skill.enabled ? "success" : "warning"} size="sm" variant="soft">
                {skill.enabled ? t("enabled") : t("disabled")}
              </Chip>
              {skill.version ? (
                <Chip size="sm" variant="soft">
                  v{skill.version}
                </Chip>
              ) : null}
            </div>

            <section className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              <Info
                icon={<Folder className="size-4" />}
                label={t("sourcePath")}
                value={skill.source_path || "—"}
              />
              <Info
                icon={<FileText className="size-4" />}
                label={t("files")}
                value={String(skill.file_count ?? 0)}
              />
              <Info
                icon={<FileText className="size-4" />}
                label={t("size")}
                value={formatBytes(skill.size_bytes ?? 0)}
              />
              <Info
                icon={<Hash className="size-4" />}
                label="SHA256"
                value={skill.content_sha256 || "—"}
              />
            </section>

            <Card className="p-5">
              <Card.Header className="p-0">
                <Card.Title>SKILL.md</Card.Title>
                <Card.Description>{t("instructionsDescription")}</Card.Description>
              </Card.Header>
              <Card.Content className="mt-5 p-0">
                <pre className="bg-surface-secondary max-h-[34rem] overflow-auto whitespace-pre-wrap rounded-2xl p-5 font-mono text-sm leading-7">
                  {skill.skill_md || t("noInstructions")}
                </pre>
              </Card.Content>
            </Card>
          </>
        ) : null}
      </div>
    </main>
  );
}

function Info({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <Card className="p-4">
      <Card.Content className="flex min-w-0 items-start gap-3 p-0">
        <span className="bg-surface-secondary text-accent grid size-9 shrink-0 place-items-center rounded-xl">
          {icon}
        </span>
        <span className="min-w-0">
          <span className="text-muted block text-xs">{label}</span>
          <span className="mt-1 block break-all text-sm font-medium">{value}</span>
        </span>
      </Card.Content>
    </Card>
  );
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  return `${Math.round(bytes / 1024)} KB`;
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
