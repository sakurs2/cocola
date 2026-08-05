"use client";

import { Archive, ArrowLeft, Check, Loader2, Save } from "lucide-react";
import { Button, Card, Chip, Input, Label, TextArea, TextField } from "@heroui/react";
import { Sheet } from "@heroui-pro/react/sheet";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { AgentAvatar } from "@/components/agents/agent-avatar";
import { AgentCapabilitiesEditor } from "@/components/agents/agent-capabilities-editor";
import { HeroUIAgentModelSelect } from "@/components/agents/heroui-agent-model-select";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";
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
          throw new Error("Agent capability configuration is temporarily unavailable.");
        }
        const loaded = (await agentResponse.json()) as AgentProfile;
        const modelRows = normalizeAgentModels(await modelsResponse.json());
        const runtimes = normalizeAgentRuntimes(await runtimesResponse.json());
        const loadedSkillCatalog = normalizeAgentSkillCatalog(await skillsResponse.json());
        const runtime = runtimes.find((item) => item.id === loaded.runtime_id);
        if (!runtime) throw new Error("This Agent's runtime is no longer available.");
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
          const message = cause instanceof Error ? cause.message : "Could not load Agent";
          setError(message);
          showError(message);
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    })();
    return () => controller.abort();
  }, [agentID, showError]);

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
      showSuccess("Agent saved");
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : "Could not save Agent";
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
      showSuccess("Agent archived");
      router.push("/agents");
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : "Could not archive Agent";
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
        <Button isIconOnly aria-label="Back to Agents" variant="ghost" onPress={() => router.push("/agents")}>
          <ArrowLeft className="size-4" />
        </Button>
        <div className="bg-danger/10 text-danger rounded-2xl p-5 text-sm">
          {error || "Agent not found"}
        </div>
      </div>
    );
  }

  return (
    <div className="cocola-agent-detail cocola-web-page mx-auto flex w-full max-w-4xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <span className="flex min-w-0 items-start gap-3">
          <Button isIconOnly aria-label="Back to Agents" variant="ghost" onPress={() => router.push("/agents")}>
            <ArrowLeft className="size-4" />
          </Button>
          <AgentAvatar avatarColor={avatarColor} avatarKey={avatarKey} className="size-11 rounded-2xl" iconClassName="size-5" />
          <span className="min-w-0">
            <span className="text-accent block text-xs font-semibold tracking-[0.12em] uppercase">Assistants</span>
            <h1 className="text-foreground mt-1 truncate text-2xl font-semibold tracking-[-0.03em]">{agent.name}</h1>
            <span className="text-muted mt-1 block text-sm">Changes apply to new conversations.</span>
          </span>
        </span>
        <Button
          className="cocola-web-page-primary-action"
          isDisabled={!dirty || !name.trim() || !selectedModel || instructionsTooLarge || models.length === 0}
          isPending={saving}
          onPress={() => void save()}
        >
          {saving ? null : dirty ? <Save className="size-4" /> : <Check className="size-4" />}
          {saving ? "Saving…" : dirty ? "Save" : "Saved"}
        </Button>
      </header>

      {error ? <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div> : null}

      <Card className="p-5">
        <Card.Header className="p-0">
          <Card.Title>Identity</Card.Title>
          <Card.Description>How this Agent appears throughout Cocola and Feishu.</Card.Description>
        </Card.Header>
        <Card.Content className="grid gap-5 p-0">
          <div className="grid gap-4 sm:grid-cols-2">
            <TextField isRequired value={name} variant="secondary" onChange={setName}>
              <Label>Name</Label>
              <Input maxLength={100} />
            </TextField>
            <TextField value={description} variant="secondary" onChange={setDescription}>
              <Label>Description</Label>
              <Input maxLength={500} placeholder="What this Agent is best at" />
            </TextField>
          </div>
          <div className="grid gap-5">
            <div>
              <Label>Icon</Label>
              <div aria-label="Agent icon" className="mt-2 flex flex-wrap gap-2" role="group">
                {AGENT_AVATAR_KEYS.map((key) => (
                  <Button
                    key={key}
                    aria-label={`Use ${key} icon`}
                    aria-pressed={avatarKey === key}
                    className={`h-auto min-h-0 min-w-0 rounded-2xl p-1.5 ${avatarKey === key ? "bg-accent-soft ring-accent/30 ring-1" : ""}`}
                    variant="ghost"
                    onPress={() => setAvatarKey(key)}
                  >
                    <AgentAvatar avatarColor={avatarColor} avatarKey={key} className="size-9 rounded-xl" iconClassName="size-4" />
                  </Button>
                ))}
              </div>
            </div>
            <div>
              <Label>Color</Label>
              <div aria-label="Agent color" className="mt-2 flex flex-wrap gap-2" role="group">
                {AGENT_AVATAR_COLORS.map((color) => (
                  <Button
                    key={color}
                    aria-label={`Use ${color} color`}
                    aria-pressed={avatarColor === color}
                    className={`grid size-9 min-w-9 place-items-center rounded-full p-0 ring-2 ring-transparent ring-offset-2 ring-offset-background ${avatarColor === color ? "ring-foreground/30" : ""}`}
                    variant="ghost"
                    onPress={() => setAvatarColor(color)}
                  >
                    <span className={`grid size-7 place-items-center rounded-full ${COLOR_SWATCHES[color]}`}>
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
            <Card.Title>Instructions</Card.Title>
            <Card.Description>Define the Agent&apos;s role and working rules. Platform and administrator policies still take precedence.</Card.Description>
          </span>
          <Chip color={instructionsTooLarge ? "danger" : "accent"} size="sm" variant="soft">
            {instructionsBytes.toLocaleString()} / {MAX_INSTRUCTIONS_BYTES.toLocaleString()} bytes
          </Chip>
        </Card.Header>
        <Card.Content className="p-0">
          <TextField variant="secondary">
            <Label className="sr-only">Agent Instructions</Label>
            <TextArea
              className="min-h-64 resize-y font-mono text-sm leading-6"
              placeholder="Describe the Agent's role and working rules…"
              value={instructions}
              onChange={(event) => setInstructions(event.target.value)}
            />
          </TextField>
          {instructionsTooLarge ? <p className="text-danger mt-2 text-xs">Instructions exceed the 32KB limit and cannot be saved.</p> : null}
        </Card.Content>
      </Card>

      <Card className="p-5">
        <Card.Header className="p-0">
          <Card.Title>Model</Card.Title>
          <Card.Description>Conversations using this Agent always use this compatible model.</Card.Description>
        </Card.Header>
        <Card.Content className="p-0">
          <HeroUIAgentModelSelect fallbackLabel={agent.model_alias} models={models} value={modelID} onChange={setModelID} />
          <p className="text-muted mt-3 text-xs">Only compatible models are available.</p>
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
            <Card.Title>Archive Agent</Card.Title>
            <Card.Description>It will disappear from new chats. Existing conversation history remains.</Card.Description>
          </span>
          <Button variant="danger-soft" onPress={() => { setError(""); setArchiveOpen(true); }}>
            <Archive className="size-4" /> Archive
          </Button>
        </Card.Header>
      </Card>

      <p className="text-muted text-right text-xs tabular-nums">Agent version {agent.version} · Knowledge revision {agent.knowledge_revision}</p>

      <Sheet isOpen={archiveOpen} placement="right" onOpenChange={setArchiveOpen}>
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[440px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label="Close archive confirmation" />
              <Sheet.Header>
                <Sheet.Heading>Archive this Agent?</Sheet.Heading>
                <p className="text-muted text-sm">Disconnect its Feishu bot first. Existing conversations stay in history, but this Agent cannot start or continue turns after it is archived.</p>
              </Sheet.Header>
              <Sheet.Body>
                {error ? <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div> : <div className="bg-surface-secondary rounded-2xl px-4 py-3 text-sm">The Agent is ready to archive. This action cannot be reversed from the user workspace.</div>}
              </Sheet.Body>
              <Sheet.Footer className="gap-2">
                <Button variant="outline" onPress={() => setArchiveOpen(false)}>Cancel</Button>
                <Button isPending={archiving} variant="danger-soft" onPress={() => void archive()}>Archive Agent</Button>
              </Sheet.Footer>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
    </div>
  );
}
