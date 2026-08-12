"use client";

import { Button, Card, Chip } from "@heroui/react";
import {
  AlertTriangle,
  ArrowLeft,
  FileText,
  Folder,
  Hash,
  LoaderCircle,
  Trash2,
} from "lucide-react";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useTranslations } from "next-intl";
import { DeleteConfirmDialog } from "@/components/assistant-ui/delete-confirm-dialog";
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
  available?: boolean;
  unavailable_reason?: string;
};

export default function SkillDetailPage() {
  const t = useTranslations("skills.detail");
  const skillsT = useTranslations("skills");
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [skill, setSkill] = useState<Skill | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [removeOpen, setRemoveOpen] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const response = await fetch(`/api/skills/${encodeURIComponent(id)}`, {
          cache: "no-store",
          signal: controller.signal,
        });
        if (!response.ok) throw new Error(await readError(response));
        const loaded = (await response.json()) as Skill;
        if (!controller.signal.aborted) setSkill(loaded);
      } catch (cause) {
        if (!controller.signal.aborted)
          setError(cause instanceof Error ? cause.message : String(cause));
      }
    })();
    return () => controller.abort();
  }, [id]);

  const toggle = async () => {
    if (!skill || skill.available === false) return;
    const previous = skill;
    const nextEnabled = !skill.enabled;
    setSkill({ ...skill, enabled: nextEnabled });
    setBusy(true);
    setError("");
    try {
      const response = await fetch(
        `/api/skills/${encodeURIComponent(skill.id)}/${nextEnabled ? "enable" : "disable"}`,
        { method: "POST" },
      );
      if (!response.ok) throw new Error(await readError(response));
    } catch (cause) {
      setSkill(previous);
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!skill) return;
    setBusy(true);
    setError("");
    try {
      const response = await fetch(`/api/skills/${encodeURIComponent(skill.id)}`, {
        method: "DELETE",
      });
      if (!response.ok) throw new Error(await readError(response));
      router.push("/skills");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      setBusy(false);
    }
  };

  if (!skill && !error) {
    return (
      <div className="cocola-web-page grid min-h-64 place-items-center p-8">
        <LoaderCircle className="text-muted size-5 animate-spin" />
      </div>
    );
  }

  return (
    <div className="cocola-web-page mx-auto flex w-full max-w-6xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-wrap items-center gap-3">
        <Button
          isIconOnly
          aria-label={t("back")}
          variant="ghost"
          onPress={() => router.push("/skills")}
        >
          <ArrowLeft className="size-4" />
        </Button>
        <span className="bg-accent-soft text-accent flex size-11 items-center justify-center rounded-2xl">
          <SkillIcon name={skill?.name || id} size="sm" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-semibold tracking-[-0.03em]">
            {skill?.name || t("fallback")}
          </h1>
          <p className="text-muted mt-1 text-sm">{skill?.description || skill?.id || id}</p>
        </div>
      </header>

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      {skill?.available === false ? (
        <Card className="p-5">
          <Card.Content className="flex items-start gap-3 p-0">
            <span className="bg-warning/10 text-warning grid size-10 shrink-0 place-items-center rounded-xl">
              <AlertTriangle className="size-5" />
            </span>
            <span className="min-w-0 flex-1">
              <Card.Title>{t("unavailable")}</Card.Title>
              <Card.Description className="mt-1">
                {skill.unavailable_reason === "disabled_by_administrator"
                  ? t("adminDisabled")
                  : t("workspaceUnavailable")}
              </Card.Description>
              <Button
                className="mt-4"
                size="sm"
                variant="outline"
                onPress={() => router.push("/skills")}
              >
                {t("back")}
              </Button>
            </span>
          </Card.Content>
        </Card>
      ) : skill ? (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Chip size="sm" variant="soft">
              {skill.scope === "user" ? t("personal") : t("shared")}
            </Chip>
            <Chip size="sm" variant="soft">
              {skill.source_type || t("manual")}
            </Chip>
            <Chip color={skill.enabled ? "success" : "warning"} size="sm" variant="soft">
              {skill.enabled ? t("enabled") : t("disabled")}
            </Chip>
            <div className="ml-auto flex gap-2">
              {skill.scope === "user" ? (
                <Button size="sm" variant="danger-soft" onPress={() => setRemoveOpen(true)}>
                  <Trash2 className="size-3.5" />
                  {t("remove")}
                </Button>
              ) : null}
              <Button
                isPending={busy}
                size="sm"
                variant={skill.enabled ? "outline" : "primary"}
                onPress={() => void toggle()}
              >
                {skill.enabled ? t("disable") : t("enable")}
              </Button>
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <InfoCard
              icon={<Folder className="size-4" />}
              label={t("sourcePath")}
              value={skill.source_path || "—"}
            />
            <InfoCard
              icon={<FileText className="size-4" />}
              label={t("files")}
              value={String(skill.file_count ?? 0)}
            />
            <InfoCard
              icon={<FileText className="size-4" />}
              label={t("size")}
              value={formatBytes(skill.size_bytes ?? 0)}
            />
            <InfoCard
              icon={<Hash className="size-4" />}
              label="SHA256"
              value={skill.content_sha256 || "—"}
            />
          </div>

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

      <DeleteConfirmDialog
        busy={busy}
        confirmLabel={skillsT("remove")}
        description={t("removeDescription")}
        error={error || null}
        open={removeOpen}
        title={skillsT("removeTitle")}
        onConfirm={() => void remove()}
        onOpenChange={setRemoveOpen}
      />
    </div>
  );
}

function InfoCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
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

async function readError(response: Response) {
  const text = await response.text();
  try {
    const json = JSON.parse(text);
    if (typeof json.error === "string") return json.error;
    if (json.error && typeof json.error === "object")
      return json.error.message || json.error.code || text;
    return json.message || text;
  } catch {
    return text || response.statusText;
  }
}
