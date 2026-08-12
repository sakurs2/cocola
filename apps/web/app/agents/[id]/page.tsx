"use client";

import { Archive, ArrowLeft, Check, Loader2, Save } from "lucide-react";
import { Button, Card, Chip, Input, Label, TextArea, TextField } from "@heroui/react";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { useFormatter, useTranslations } from "next-intl";
import { AgentAvatar } from "@/components/agents/agent-avatar";
import { AgentCapabilitiesEditor } from "@/components/agents/agent-capabilities-editor";
import { HeroUIAgentModelSelect } from "@/components/agents/heroui-agent-model-select";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { FeishuConnectorCard } from "@/components/connectors/feishu-connector-card";
import {
  AGENT_AVATAR_COLORS,
  AGENT_AVATAR_KEYS,
  agentResponseError,
  normalizeAgentSkillCatalog,
  normalizeAgentModels,
  normalizeAgentRuntimes,
  type AgentKnowledgeSource,
  type AgentModelOption,
  type AgentProfile,
  type AgentSkillCatalogItem,
} from "@/lib/agents";

const MAX_INSTRUCTIONS_BYTES = 32 * 1024;

const COLOR_SWATCHES: Record<string, string> = {
  slate: "bg-slate-500",
  blue: "bg-blue-500",
  cyan: "bg-cyan-500",
  emerald: "bg-emerald-500",
  amber: "bg-amber-500",
  orange: "bg-orange-500",
  rose: "bg-rose-500",
  violet: "bg-violet-500",
};

export default function AgentPage() {
  const t = useTranslations("agents.detail");
  const format = useFormatter();
  const params = useParams<{ id: string }>();
  const agentID = params.id;
  const router = useRouter();
  const { showError, showSuccess } = useWorkspaceToast();
  const [agent, setAgent] = useState<AgentProfile | null>(null);
  const [models, setModels] = useState<AgentModelOption[]>([]);
  const [skillCatalog, setSkillCatalog] = useState<AgentSkillCatalogItem[]>([]);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [instructions, setInstructions] = useState("");
  const [avatarKey, setAvatarKey] = useState("");
  const [avatarColor, setAvatarColor] = useState("");
  const [modelID, setModelID] = useState("");
  const [skillIDs, setSkillIDs] = useState<string[]>([]);
  const [knowledgeSources, setKnowledgeSources] = useState<AgentKnowledgeSource[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);
  const [archiving, setArchiving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    void (async () => {
      try {
        const [agentResponse, modelsResponse, runtimesResponse, skillsResponse] = await Promise.all(
          [
            fetch(`/api/agents/${encodeURIComponent(agentID)}`, {
              cache: "no-store",
              signal: controller.signal,
            }),
            fetch("/api/models", { cache: "no-store", signal: controller.signal }),
            fetch("/api/agent-runtimes", { cache: "no-store", signal: controller.signal }),
            fetch("/api/skills/agent-catalog", {
              cache: "no-store",
              signal: controller.signal,
            }),
          ],
        );
        if (!agentResponse.ok) throw new Error(await agentResponseError(agentResponse));
        if (!modelsResponse.ok || !runtimesResponse.ok || !skillsResponse.ok) {
          throw new Error(t("capabilitiesUnavailable"));
        }
        const loaded = (await agentResponse.json()) as AgentProfile;
        const modelRows = normalizeAgentModels(await modelsResponse.json());
        const runtimes = normalizeAgentRuntimes(await runtimesResponse.json());
        const loadedSkillCatalog = normalizeAgentSkillCatalog(await skillsResponse.json());
        const runtime = runtimes.find((item) => item.id === loaded.runtime_id);
        if (!runtime) throw new Error(t("runtimeUnavailable"));
        const compatible = modelRows.filter((model) =>
          model.protocols.includes(runtime.modelProtocol),
        );
        if (!controller.signal.aborted) {
          setAgent(loaded);
          setName(loaded.name);
          setDescription(loaded.description);
          setInstructions(loaded.instructions);
          setAvatarKey(loaded.avatar_key);
          setAvatarColor(loaded.avatar_color);
          setModelID(loaded.model_route_id);
          setModels(compatible);
          setSkillCatalog(loadedSkillCatalog);
          setSkillIDs(loaded.skill_ids ?? []);
          setKnowledgeSources(loaded.knowledge_sources ?? []);
        }
      } catch (cause) {
        if (!controller.signal.aborted) {
          const message = cause instanceof Error ? cause.message : t("loadFailed");
          setError(message);
          showError(message);
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    })();
    return () => controller.abort();
  }, [agentID, showError, t]);

  const selectedModel = useMemo(
    () => models.find((model) => model.id === modelID) ?? null,
    [modelID, models],
  );
  const instructionsBytes = useMemo(
    () => new TextEncoder().encode(instructions).byteLength,
    [instructions],
  );
  const instructionsTooLarge = instructionsBytes > MAX_INSTRUCTIONS_BYTES;
  const dirty = useMemo(() => {
    if (!agent) return false;
    return (
      name !== agent.name ||
      description !== agent.description ||
      instructions !== agent.instructions ||
      avatarKey !== agent.avatar_key ||
      avatarColor !== agent.avatar_color ||
      modelID !== agent.model_route_id ||
      JSON.stringify(skillIDs) !== JSON.stringify(agent.skill_ids ?? []) ||
      JSON.stringify(knowledgeSources) !== JSON.stringify(agent.knowledge_sources ?? [])
    );
  }, [
    agent,
    avatarColor,
    avatarKey,
    description,
    instructions,
    knowledgeSources,
    modelID,
    name,
    skillIDs,
  ]);

  const save = async () => {
    if (!agent || !name.trim() || !selectedModel || instructionsTooLarge) return;
    setSaving(true);
    setError("");
    try {
      const response = await fetch(`/api/agents/${encodeURIComponent(agent.id)}`, {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name: name.trim(),
          description: description.trim(),
          instructions,
          avatar_key: avatarKey,
          avatar_color: avatarColor,
          runtime_id: agent.runtime_id,
          model_route_id: selectedModel.id,
          model_alias: selectedModel.alias,
          skill_ids: skillIDs,
          knowledge_sources: knowledgeSources,
          version: agent.version,
        }),
      });
      if (!response.ok) throw new Error(await agentResponseError(response));
      const updated = (await response.json()) as AgentProfile;
      setAgent(updated);
      setName(updated.name);
      setDescription(updated.description);
      setInstructions(updated.instructions);
      setAvatarKey(updated.avatar_key);
      setAvatarColor(updated.avatar_color);
      setModelID(updated.model_route_id);
      setSkillIDs(updated.skill_ids);
      setKnowledgeSources(updated.knowledge_sources);
      showSuccess(t("savedNotice"));
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : t("saveFailed");
      setError(message);
      showError(message);
    } finally {
      setSaving(false);
    }
  };

  const archive = async () => {
    if (!agent) return;
    setArchiving(true);
    setError("");
    try {
      const response = await fetch(`/api/agents/${encodeURIComponent(agent.id)}`, {
        method: "DELETE",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ version: agent.version }),
      });
      if (!response.ok) throw new Error(await agentResponseError(response));
      setArchiveOpen(false);
      showSuccess(t("archivedNotice"));
      router.push("/agents");
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : t("archiveFailed");
      setError(message);
      showError(message);
    } finally {
      setArchiving(false);
    }
  };

  if (loading) {
    return (
      <div className="cocola-web-page grid min-h-64 w-full place-items-center p-8">
        <Loader2 className="text-muted size-5 animate-spin" />
      </div>
    );
  }

  if (!agent) {
    return (
      <div className="cocola-web-page mx-auto flex w-full max-w-4xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
        <Button
          isIconOnly
          aria-label={t("back")}
          variant="ghost"
          onPress={() => router.push("/agents")}
        >
          <ArrowLeft className="size-4" />
        </Button>
        <div className="bg-danger/10 text-danger rounded-2xl p-5 text-sm">
          {error || t("notFound")}
        </div>
      </div>
    );
  }

  return (
    <div className="cocola-agent-detail cocola-web-page mx-auto flex w-full max-w-4xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <span className="flex min-w-0 items-start gap-3">
          <Button
            isIconOnly
            aria-label={t("back")}
            variant="ghost"
            onPress={() => router.push("/agents")}
          >
            <ArrowLeft className="size-4" />
          </Button>
          <AgentAvatar
            avatarColor={avatarColor}
            avatarKey={avatarKey}
            className="size-11 rounded-2xl"
            iconClassName="size-5"
          />
          <span className="min-w-0">
            <span className="text-accent block text-xs font-semibold tracking-[0.12em] uppercase">
              {t("eyebrow")}
            </span>
            <h1 className="text-foreground mt-1 truncate text-2xl font-semibold tracking-[-0.03em]">
              {agent.name}
            </h1>
            <span className="text-muted mt-1 block text-sm">{t("changesApply")}</span>
          </span>
        </span>
        <Button
          className="cocola-web-page-primary-action"
          isDisabled={
            !dirty || !name.trim() || !selectedModel || instructionsTooLarge || models.length === 0
          }
          isPending={saving}
          onPress={() => void save()}
        >
          {saving ? null : dirty ? <Save className="size-4" /> : <Check className="size-4" />}
          {saving ? t("saving") : dirty ? t("save") : t("saved")}
        </Button>
      </header>

      {error ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      <Card className="p-5">
        <Card.Header className="p-0">
          <Card.Title>{t("identity")}</Card.Title>
          <Card.Description>{t("identityDescription")}</Card.Description>
        </Card.Header>
        <Card.Content className="grid gap-5 p-0">
          <div className="grid gap-4 sm:grid-cols-2">
            <TextField isRequired value={name} variant="secondary" onChange={setName}>
              <Label>{t("name")}</Label>
              <Input maxLength={100} />
            </TextField>
            <TextField value={description} variant="secondary" onChange={setDescription}>
              <Label>{t("description")}</Label>
              <Input maxLength={500} placeholder={t("descriptionPlaceholder")} />
            </TextField>
          </div>
          <div className="grid gap-5">
            <div>
              <Label>{t("icon")}</Label>
              <div aria-label={t("iconGroup")} className="mt-2 flex flex-wrap gap-2" role="group">
                {AGENT_AVATAR_KEYS.map((key) => (
                  <Button
                    key={key}
                    aria-label={t("useIcon", { name: key })}
                    aria-pressed={avatarKey === key}
                    className={`h-auto min-h-0 min-w-0 rounded-2xl p-1.5 ${avatarKey === key ? "bg-accent-soft ring-accent/30 ring-1" : ""}`}
                    variant="ghost"
                    onPress={() => setAvatarKey(key)}
                  >
                    <AgentAvatar
                      avatarColor={avatarColor}
                      avatarKey={key}
                      className="size-9 rounded-xl"
                      iconClassName="size-4"
                    />
                  </Button>
                ))}
              </div>
            </div>
            <div>
              <Label>{t("color")}</Label>
              <div aria-label={t("colorGroup")} className="mt-2 flex flex-wrap gap-2" role="group">
                {AGENT_AVATAR_COLORS.map((color) => (
                  <Button
                    key={color}
                    aria-label={t("useColor", { name: color })}
                    aria-pressed={avatarColor === color}
                    className={`grid size-9 min-w-9 place-items-center rounded-full p-0 ring-2 ring-transparent ring-offset-2 ring-offset-background ${avatarColor === color ? "ring-foreground/30" : ""}`}
                    variant="ghost"
                    onPress={() => setAvatarColor(color)}
                  >
                    <span
                      className={`grid size-7 place-items-center rounded-full ${COLOR_SWATCHES[color]}`}
                    >
                      {avatarColor === color ? <Check className="size-3.5 text-white" /> : null}
                    </span>
                  </Button>
                ))}
              </div>
            </div>
          </div>
        </Card.Content>
      </Card>

      <Card className="p-5">
        <Card.Header className="flex-row items-start justify-between gap-4 p-0">
          <span>
            <Card.Title>{t("instructions")}</Card.Title>
            <Card.Description>{t("instructionsDescription")}</Card.Description>
          </span>
          <Chip color={instructionsTooLarge ? "danger" : "accent"} size="sm" variant="soft">
            {t("bytes", {
              used: format.number(instructionsBytes),
              limit: format.number(MAX_INSTRUCTIONS_BYTES),
            })}
          </Chip>
        </Card.Header>
        <Card.Content className="p-0">
          <TextField variant="secondary">
            <Label className="sr-only">{t("instructionsAria")}</Label>
            <TextArea
              className="min-h-64 resize-y font-mono text-sm leading-6"
              placeholder={t("instructionsPlaceholder")}
              value={instructions}
              onChange={(event) => setInstructions(event.target.value)}
            />
          </TextField>
          {instructionsTooLarge ? (
            <p className="text-danger mt-2 text-xs">{t("tooLarge")}</p>
          ) : null}
        </Card.Content>
      </Card>

      <Card className="p-5">
        <Card.Header className="p-0">
          <Card.Title>{t("model")}</Card.Title>
          <Card.Description>{t("modelDescription")}</Card.Description>
        </Card.Header>
        <Card.Content className="p-0">
          <HeroUIAgentModelSelect
            fallbackLabel={agent.model_alias}
            models={models}
            value={modelID}
            onChange={setModelID}
          />
          <p className="text-muted mt-3 text-xs">{t("compatibleModels")}</p>
        </Card.Content>
      </Card>

      <AgentCapabilitiesEditor
        skills={skillCatalog}
        skillIDs={skillIDs}
        onSkillIDsChange={setSkillIDs}
        knowledgeSources={knowledgeSources}
        onKnowledgeSourcesChange={setKnowledgeSources}
      />

      <FeishuConnectorCard agentId={agent.id} />

      <Card className="border-danger/20 bg-danger/[0.025] p-5">
        <Card.Header className="flex-row items-center justify-between gap-4 p-0">
          <span>
            <Card.Title>{t("archive")}</Card.Title>
            <Card.Description>{t("archiveDescription")}</Card.Description>
          </span>
          <Button
            variant="danger-soft"
            onPress={() => {
              setError("");
              setArchiveOpen(true);
            }}
          >
            <Archive className="size-4" /> {t("archiveAction")}
          </Button>
        </Card.Header>
      </Card>

      <p className="text-muted text-right text-xs tabular-nums">
        {t("version", { version: agent.version, revision: agent.knowledge_revision })}
      </p>

      <ActionConfirmDialog
        busy={archiving}
        confirmLabel={t("archive")}
        description={t("archiveConfirmDescription")}
        error={error || null}
        icon={Archive}
        open={archiveOpen}
        title={t("archiveTitle")}
        tone="danger"
        onConfirm={() => void archive()}
        onOpenChange={setArchiveOpen}
      />
    </div>
  );
}
