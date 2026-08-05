"use client";

import { Button, Card, Chip, Input, Label, Separator, TextField } from "@heroui/react";
import { EmptyState } from "@heroui-pro/react/empty-state";
import { ArrowRight, Bot, Check, Loader2, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { AgentAvatar } from "@/components/agents/agent-avatar";
import { HeroUIAgentModelSelect } from "@/components/agents/heroui-agent-model-select";
import {
  WorkspaceEntitySheet,
  WorkspacePageAction,
  WorkspacePageFrame,
  WorkspacePageHeader,
  WorkspaceSectionHeader,
} from "@/components/heroui-workspace/workspace-ui";
import { ModelIcon } from "@/components/ui/model-icon";
import {
  AGENT_AVATAR_COLORS,
  AGENT_AVATAR_KEYS,
  DEFAULT_AGENT_AVATAR_COLOR,
  DEFAULT_AGENT_AVATAR_KEY,
  agentResponseError,
  configuredAgentRuntimeID,
  normalizeAgentSkillCatalog,
  normalizeAgentModels,
  normalizeAgentRuntimes,
  type AgentModelOption,
  type AgentProfile,
  type AgentSkillCatalogItem,
} from "@/lib/agents";

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

export default function AgentsPage() {
  const router = useRouter();
  const [agents, setAgents] = useState<AgentProfile[]>([]);
  const [models, setModels] = useState<AgentModelOption[]>([]);
  const [skillCatalog, setSkillCatalog] = useState<AgentSkillCatalogItem[]>([]);
  const [runtimeID, setRuntimeID] = useState("");
  const [loading, setLoading] = useState(true);
  const [catalogLoading, setCatalogLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [modelID, setModelID] = useState("");
  const [avatarKey, setAvatarKey] = useState<string>(DEFAULT_AGENT_AVATAR_KEY);
  const [avatarColor, setAvatarColor] = useState<string>(DEFAULT_AGENT_AVATAR_COLOR);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const response = await fetch("/api/agents", { cache: "no-store", signal: controller.signal });
        if (!response.ok) throw new Error(await agentResponseError(response));
        const rows = (await response.json()) as AgentProfile[];
        if (!controller.signal.aborted) setAgents(Array.isArray(rows) ? rows : []);
      } catch (cause) {
        if (!controller.signal.aborted) {
          setError(cause instanceof Error ? cause.message : "Could not load Agents");
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    })();
    return () => controller.abort();
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const [modelsResponse, runtimesResponse, configResponse, skillsResponse] = await Promise.all([
          fetch("/api/models", { cache: "no-store", signal: controller.signal }),
          fetch("/api/agent-runtimes", { cache: "no-store", signal: controller.signal }),
          fetch("/api/product-config", { cache: "no-store", signal: controller.signal }),
          fetch("/api/skills/agent-catalog", { cache: "no-store", signal: controller.signal }),
        ]);
        if (!modelsResponse.ok || !runtimesResponse.ok || !configResponse.ok || !skillsResponse.ok) {
          throw new Error("Agent capability configuration is temporarily unavailable.");
        }
        const modelRows = normalizeAgentModels(await modelsResponse.json());
        const runtimes = normalizeAgentRuntimes(await runtimesResponse.json());
        const configuredRuntimeID = configuredAgentRuntimeID(await configResponse.json());
        const loadedSkillCatalog = normalizeAgentSkillCatalog(await skillsResponse.json());
        const runtime =
          runtimes.find((item) => item.id === configuredRuntimeID) ??
          runtimes.find((item) => item.isDefault);
        if (!runtime) throw new Error("The default Agent runtime is not configured.");
        const compatible = modelRows.filter((model) => model.protocols.includes(runtime.modelProtocol));
        if (!controller.signal.aborted) {
          setRuntimeID(runtime.id);
          setModels(compatible);
          setSkillCatalog(loadedSkillCatalog);
          setModelID(compatible.find((model) => model.isDefault)?.id ?? compatible[0]?.id ?? "");
        }
      } catch (cause) {
        if (!controller.signal.aborted) {
          setError(cause instanceof Error ? cause.message : "Could not load models");
        }
      } finally {
        if (!controller.signal.aborted) setCatalogLoading(false);
      }
    })();
    return () => controller.abort();
  }, []);

  const selectedModel = useMemo(
    () => models.find((model) => model.id === modelID) ?? null,
    [modelID, models],
  );
  const modelsByID = useMemo(() => new Map(models.map((model) => [model.id, model])), [models]);

  const openCreateSheet = () => {
    setError("");
    setName("");
    setDescription("");
    setAvatarKey(DEFAULT_AGENT_AVATAR_KEY);
    setAvatarColor(DEFAULT_AGENT_AVATAR_COLOR);
    setModelID(models.find((model) => model.isDefault)?.id ?? models[0]?.id ?? "");
    setCreateOpen(true);
  };

  const createAgent = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!name.trim() || !runtimeID || !selectedModel) return;
    setCreating(true);
    setError("");
    try {
      const response = await fetch("/api/agents", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          name: name.trim(),
          description: description.trim(),
          instructions: "",
          avatar_key: avatarKey,
          avatar_color: avatarColor,
          runtime_id: runtimeID,
          model_route_id: selectedModel.id,
          model_alias: selectedModel.alias,
          skill_ids: [],
          knowledge_sources: [],
        }),
      });
      if (!response.ok) throw new Error(await agentResponseError(response));
      const created = (await response.json()) as AgentProfile;
      setCreateOpen(false);
      router.push(`/agents/${encodeURIComponent(created.id)}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not create Agent");
    } finally {
      setCreating(false);
    }
  };

  return (
    <WorkspacePageFrame>
      <WorkspacePageHeader
        action={
          <WorkspacePageAction
            isDisabled={catalogLoading || !runtimeID || models.length === 0}
            onPress={openCreateSheet}
          >
            <Plus className="size-4" />
            New agent
          </WorkspacePageAction>
        }
        description="Reusable profiles that bind a model, prompt, skills, knowledge, and channels."
        icon={<Bot className="size-5" />}
        title="Agents"
      />

      {error && !createOpen ? (
        <div className="bg-danger/10 text-danger rounded-2xl px-4 py-3 text-sm">{error}</div>
      ) : null}

      <WorkspaceSectionHeader
        description={`${agents.length} reusable profile${agents.length === 1 ? "" : "s"} available for new chats.`}
        title="Your agents"
      />

      {loading ? (
        <div className="grid min-h-64 place-items-center">
          <Loader2 className="text-muted size-5 animate-spin" />
        </div>
      ) : agents.length ? (
        <section className="cocola-web-catalog-grid grid items-stretch gap-3 md:grid-cols-2 xl:grid-cols-3">
          {agents.map((agent) => {
            const model = modelsByID.get(agent.model_route_id);
            const selectedSkills = (agent.skill_ids ?? [])
              .map((id) => skillCatalog.find((skill) => skill.id === id))
              .filter((skill): skill is AgentSkillCatalogItem => Boolean(skill?.available));
            return (
              <Link
                key={agent.id}
                className="cocola-web-catalog-trigger group rounded-2xl no-underline outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background"
                href={`/agents/${encodeURIComponent(agent.id)}`}
              >
                <Card className="cocola-web-catalog-card h-full min-h-[15.5rem] p-5">
                  <Card.Content className="flex h-full min-w-0 flex-col items-start p-0">
                    <span className="flex w-full items-start justify-between gap-3">
                      <AgentAvatar
                        avatarColor={agent.avatar_color}
                        avatarKey={agent.avatar_key}
                        className="size-11 rounded-2xl"
                      />
                      <Chip
                        color={agent.status === "active" ? "success" : "warning"}
                        size="sm"
                        variant="soft"
                      >
                        {agent.status === "active" ? "Active" : "Archived"}
                      </Chip>
                    </span>
                    <span className="text-foreground mt-4 block max-w-full truncate font-semibold">
                      {agent.name}
                    </span>
                    <span className="text-muted mt-1 line-clamp-2 min-h-10 text-sm leading-5">
                      {agent.description || "No description"}
                    </span>
                    <span className="mt-4 flex min-h-7 max-w-full flex-wrap gap-1.5">
                      {(agent.skill_ids ?? []).length === 0 ? (
                        <Chip size="sm" variant="soft">Default Skills</Chip>
                      ) : selectedSkills.length ? (
                        <>
                          {selectedSkills.slice(0, 2).map((skill) => (
                            <Chip key={skill.id} size="sm" variant="soft">{skill.name}</Chip>
                          ))}
                          {selectedSkills.length > 2 ? (
                            <Chip size="sm" variant="soft">+{selectedSkills.length - 2}</Chip>
                          ) : null}
                        </>
                      ) : (
                        <Chip color="warning" size="sm" variant="soft">Skills unavailable</Chip>
                      )}
                    </span>
                    <span className="mt-auto block w-full">
                      <Separator className="mb-4 mt-3" />
                      <span className="text-muted flex w-full min-w-0 items-center justify-between gap-3 text-xs">
                        <span className="bg-surface-secondary flex min-w-0 items-center gap-2 rounded-xl px-2.5 py-1.5">
                          <ModelIcon className="size-4 shrink-0" icon={model?.icon} bare />
                          <span className="truncate">{agent.model_alias}</span>
                        </span>
                        <span className="text-accent flex shrink-0 items-center gap-1 font-medium">
                          Open Agent
                          <ArrowRight className="cocola-web-catalog-card-arrow size-3.5" />
                        </span>
                      </span>
                    </span>
                  </Card.Content>
                </Card>
              </Link>
            );
          })}
        </section>
      ) : (
        <Card className="p-5">
          <EmptyState>
            <EmptyState.Header>
              <EmptyState.Media variant="icon"><Bot className="text-cyan-500" /></EmptyState.Media>
              <EmptyState.Title>No Agents yet</EmptyState.Title>
              <EmptyState.Description>
                Create a focused profile, or continue using standard chat without an Agent.
              </EmptyState.Description>
            </EmptyState.Header>
            <EmptyState.Content>
              <Button size="sm" variant="outline" onPress={openCreateSheet}>
                <Plus className="size-4" /> New agent
              </Button>
            </EmptyState.Content>
          </EmptyState>
        </Card>
      )}

      <WorkspaceEntitySheet
        description="Create a focused identity and choose its compatible model. Configure capabilities and channels next."
        isOpen={createOpen}
        title="New Agent"
        onOpenChange={(next) => !creating && setCreateOpen(next)}
      >
        <form className="grid gap-5" onSubmit={createAgent}>
          <div className="bg-surface-secondary flex items-center gap-3 rounded-2xl px-4 py-3">
            <AgentAvatar avatarColor={avatarColor} avatarKey={avatarKey} className="size-10" />
            <span className="min-w-0">
              <span className="block truncate text-sm font-medium">{name.trim() || "New Agent"}</span>
              <span className="text-muted mt-1 block text-xs">
                Instructions, knowledge, and channels are configured after creation.
              </span>
            </span>
          </div>

          <TextField isRequired value={name} onChange={setName}>
            <Label>Name</Label>
            <Input autoFocus maxLength={100} placeholder="Data analyst" />
          </TextField>
          <TextField value={description} onChange={setDescription}>
            <Label>Description</Label>
            <Input maxLength={500} placeholder="What this Agent is best at" />
          </TextField>

          <div>
            <Label>Icon</Label>
            <div className="mt-2 flex flex-wrap gap-2">
              {AGENT_AVATAR_KEYS.map((key) => (
                <Button
                  key={key}
                  isIconOnly
                  aria-label={`Use ${key} icon`}
                  className={avatarKey === key ? "ring-2 ring-accent" : ""}
                  size="sm"
                  variant="ghost"
                  onPress={() => setAvatarKey(key)}
                >
                  <AgentAvatar avatarColor={avatarColor} avatarKey={key} className="size-8 rounded-lg" />
                </Button>
              ))}
            </div>
          </div>

          <div>
            <Label>Color</Label>
            <div className="mt-2 flex flex-wrap gap-2">
              {AGENT_AVATAR_COLORS.map((color) => (
                <Button
                  key={color}
                  isIconOnly
                  aria-label={`Use ${color} color`}
                  className={avatarColor === color ? "ring-2 ring-foreground/30" : ""}
                  size="sm"
                  variant="ghost"
                  onPress={() => setAvatarColor(color)}
                >
                  <span className={`grid size-6 place-items-center rounded-full ${COLOR_SWATCHES[color]}`}>
                    {avatarColor === color ? <Check className="size-3.5 text-white" /> : null}
                  </span>
                </Button>
              ))}
            </div>
          </div>

          <div>
            <Label>Model</Label>
            <div className="mt-2">
              <HeroUIAgentModelSelect
                models={models}
                value={modelID}
                onChange={setModelID}
              />
            </div>
          </div>

          {error ? <div className="bg-danger/10 text-danger rounded-xl px-3 py-2 text-sm">{error}</div> : null}

          <div className="flex justify-end gap-2">
            <Button isDisabled={creating} variant="outline" onPress={() => setCreateOpen(false)}>
              Cancel
            </Button>
            <Button
              isDisabled={creating || !name.trim() || !selectedModel}
              isPending={creating}
              type="submit"
              variant="primary"
            >
              {creating ? "Creating…" : "Create Agent"}
            </Button>
          </div>
        </form>
      </WorkspaceEntitySheet>
    </WorkspacePageFrame>
  );
}
