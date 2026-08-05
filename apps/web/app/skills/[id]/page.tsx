"use client";

import { Button, Card, Chip } from "@heroui/react";
import { Sheet } from "@heroui-pro/react/sheet";
import { ArrowLeft, FileText, Folder, Hash, LoaderCircle, Trash2 } from "lucide-react";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
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
    if (!skill) return;
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
          aria-label="Back to Skills"
          variant="ghost"
          onPress={() => router.push("/skills")}
        >
          <ArrowLeft className="size-4" />
        </Button>
        <span className="bg-accent-soft text-accent flex size-11 items-center justify-center rounded-2xl">
          <SkillIcon name={skill?.name || id} size="sm" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-semibold tracking-[-0.03em]">{skill?.name || "Skill"}</h1>
          <p className="text-muted mt-1 text-sm">{skill?.description || skill?.id || id}</p>
        </div>
      </header>

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      {skill ? (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Chip size="sm" variant="soft">
              {skill.scope === "user" ? "Personal" : "Shared"}
            </Chip>
            <Chip size="sm" variant="soft">
              {skill.source_type || "manual"}
            </Chip>
            <Chip color={skill.enabled ? "success" : "warning"} size="sm" variant="soft">
              {skill.enabled ? "Enabled" : "Disabled"}
            </Chip>
            <div className="ml-auto flex gap-2">
              {skill.scope === "user" ? (
                <Button size="sm" variant="danger-soft" onPress={() => setRemoveOpen(true)}>
                  <Trash2 className="size-3.5" />
                  Remove
                </Button>
              ) : null}
              <Button
                isPending={busy}
                size="sm"
                variant={skill.enabled ? "outline" : "primary"}
                onPress={() => void toggle()}
              >
                {skill.enabled ? "Disable" : "Enable"}
              </Button>
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <InfoCard
              icon={<Folder className="size-4" />}
              label="Source path"
              value={skill.source_path || "—"}
            />
            <InfoCard
              icon={<FileText className="size-4" />}
              label="Files"
              value={String(skill.file_count ?? 0)}
            />
            <InfoCard
              icon={<FileText className="size-4" />}
              label="Size"
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
              <Card.Description>
                Full instructions loaded when the Skill matches a request.
              </Card.Description>
            </Card.Header>
            <Card.Content className="mt-5 p-0">
              <pre className="bg-surface-secondary max-h-[34rem] overflow-auto whitespace-pre-wrap rounded-2xl p-5 font-mono text-sm leading-7">
                {skill.skill_md || "No SKILL.md captured."}
              </pre>
            </Card.Content>
          </Card>
        </>
      ) : null}

      <Sheet isOpen={removeOpen} placement="right" onOpenChange={setRemoveOpen}>
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[420px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label="Close remove confirmation" />
              <Sheet.Header>
                <Sheet.Heading>Remove this Skill?</Sheet.Heading>
                <p className="text-muted text-sm">
                  This personal Skill will no longer be available to Agents or new chats.
                </p>
              </Sheet.Header>
              <Sheet.Footer className="gap-2">
                <Button variant="outline" onPress={() => setRemoveOpen(false)}>
                  Cancel
                </Button>
                <Button isPending={busy} variant="danger-soft" onPress={() => void remove()}>
                  Remove Skill
                </Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
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
