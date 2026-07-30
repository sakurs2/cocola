"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { ArrowRight, Blocks, Bot, CircleCheck, Loader2, Plus, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { AgentAvatar } from "@/components/agents/agent-avatar";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ModelIcon } from "@/components/ui/model-icon";
import { ModelSelectControl } from "@/components/ui/model-select-control";
import {
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
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const response = await fetch("/api/agents", {
          cache: "no-store",
          signal: controller.signal,
        });
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
        const [modelsResponse, runtimesResponse, configResponse, skillsResponse] =
          await Promise.all([
            fetch("/api/models", { cache: "no-store", signal: controller.signal }),
            fetch("/api/agent-runtimes", { cache: "no-store", signal: controller.signal }),
            fetch("/api/product-config", { cache: "no-store", signal: controller.signal }),
            fetch("/api/skills/agent-catalog", {
              cache: "no-store",
              signal: controller.signal,
            }),
          ]);
        if (
          !modelsResponse.ok ||
          !runtimesResponse.ok ||
          !configResponse.ok ||
          !skillsResponse.ok
        ) {
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
        const compatible = modelRows.filter((model) =>
          model.protocols.includes(runtime.modelProtocol),
        );
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

  const metrics = useMemo(() => {
    const total = agents.length;
    const active = agents.filter((agent) => agent.status === "active").length;
    const skillsWired = agents.reduce((sum, agent) => sum + (agent.skill_ids?.length ?? 0), 0);
    return { total, active, skillsWired };
  }, [agents]);

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
          avatar_key: DEFAULT_AGENT_AVATAR_KEY,
          avatar_color: DEFAULT_AGENT_AVATAR_COLOR,
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

  const hasAgents = !loading && agents.length > 0;

  return (
    <main className="user-canvas user-page user-theme-cyan h-full min-w-0 flex-1 overflow-y-auto px-4 py-5 sm:px-7 sm:py-7 lg:px-10">
      <div className="mx-auto max-w-7xl">
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex items-center gap-3.5">
            <span className="user-page-icon">
              <Bot className="size-6" />
            </span>
            <div className="space-y-1">
              <div className="user-eyebrow">Assistants</div>
              <h1 className="text-2xl font-bold tracking-tight">Agents</h1>
              <p className="text-sm text-muted-foreground">
                Create focused assistants with their own instructions, model, and Feishu bot.
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={() => {
              setError("");
              setCreateOpen(true);
            }}
            disabled={catalogLoading || !runtimeID || models.length === 0}
            className="user-accent-btn inline-flex h-11 items-center gap-2 rounded-xl px-4 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Plus className="size-4" />
            New Agent
          </button>
        </header>

        {error && !createOpen ? (
          <div className="mt-5 rounded-2xl border border-destructive/25 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        ) : null}

        {hasAgents ? (
          <div className="mt-7 grid gap-4 sm:grid-cols-3">
            <MetricCard
              tone="cyan"
              icon={<Bot className="size-[22px]" />}
              label="Total Agents"
              value={metrics.total}
              detail="Configured assistants"
            />
            <MetricCard
              tone="emerald"
              icon={<CircleCheck className="size-[22px]" />}
              label="Active"
              value={metrics.active}
              detail="Ready to run"
            />
            <MetricCard
              tone="amber"
              icon={<Blocks className="size-[22px]" />}
              label="Skills wired"
              value={metrics.skillsWired}
              detail="Across all Agents"
            />
          </div>
        ) : null}

        {loading ? (
          <div className="grid min-h-64 place-items-center text-muted-foreground">
            <Loader2 className="size-5 animate-spin" />
          </div>
        ) : agents.length === 0 ? (
          <div className="mt-8 flex min-h-[55vh] flex-col items-center justify-center text-center">
            <span className="user-page-icon size-14 rounded-2xl">
              <Bot className="size-7" />
            </span>
            <h2 className="mt-4 text-base font-semibold">No Agents yet</h2>
            <p className="mt-1 max-w-sm text-sm leading-6 text-muted-foreground">
              Start with one clear role. You can still use standard chat without selecting an Agent.
            </p>
            <button
              type="button"
              onClick={() => {
                setError("");
                setCreateOpen(true);
              }}
              disabled={catalogLoading || !runtimeID || models.length === 0}
              className="user-accent-btn mt-4 inline-flex h-11 items-center gap-2 rounded-xl px-4 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Plus className="size-4" /> New Agent
            </button>
          </div>
        ) : (
          <>
            <div className="mt-8 flex items-center gap-2.5">
              <span className="user-section-title">Your Agents</span>
              <span className="user-count-badge">{agents.length}</span>
            </div>

            <div className="mt-4 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              {agents.map((agent) => {
                const model = modelsByID.get(agent.model_route_id);
                const selectedSkills = (agent.skill_ids ?? [])
                  .map((id) => skillCatalog.find((skill) => skill.id === id))
                  .filter((skill): skill is AgentSkillCatalogItem => Boolean(skill?.available));
                return (
                  <Link
                    key={agent.id}
                    href={`/agents/${encodeURIComponent(agent.id)}`}
                    className="user-card user-card--hover group relative min-h-[15.5rem] cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
                  >
                    <span className="absolute right-4 top-4 grid size-9 place-items-center rounded-xl bg-foreground text-background opacity-0 transition-opacity duration-200 group-hover:opacity-100 group-focus-visible:opacity-100">
                      <ArrowRight className="size-[18px]" />
                    </span>
                    <div className="flex items-start gap-3">
                      <AgentAvatar
                        avatarKey={agent.avatar_key}
                        avatarColor={agent.avatar_color}
                        className="size-11 rounded-xl"
                        iconClassName="size-[1.2rem]"
                      />
                      <div className="min-w-0 flex-1 pr-8">
                        <h2 className="user-card-name truncate">{agent.name}</h2>
                        <p className="user-card-desc mt-1 line-clamp-2 min-h-9">
                          {agent.description || "No description"}
                        </p>
                      </div>
                    </div>

                    <div className="mt-4 flex min-h-7 flex-wrap items-start gap-1.5">
                      {(agent.skill_ids ?? []).length === 0 ? (
                        <span className="user-tag user-tag--muted text-[10px]">Default skills</span>
                      ) : selectedSkills.length === 0 ? (
                        <span className="user-tag user-tag--warn text-[10px]">
                          Custom skills unavailable
                        </span>
                      ) : (
                        <>
                          {selectedSkills.slice(0, 2).map((skill) => (
                            <span
                              key={skill.id}
                              className="user-tag user-tag--accent max-w-28 truncate text-[10px]"
                            >
                              {skill.name}
                            </span>
                          ))}
                          {selectedSkills.length > 2 ? (
                            <span className="user-tag user-tag--muted text-[10px]">
                              +{selectedSkills.length - 2}
                            </span>
                          ) : null}
                        </>
                      )}
                    </div>

                    <div className="mt-auto border-t border-border/45 pt-3">
                      <span className="inline-flex max-w-full items-center rounded-lg bg-muted/70 px-2.5 py-1.5 text-xs font-medium text-muted-foreground">
                        <ModelIcon icon={model?.icon} className="mr-1.5 size-4" bare />
                        <span className="truncate">{agent.model_alias}</span>
                      </span>
                    </div>
                  </Link>
                );
              })}
            </div>
          </>
        )}
      </div>

      <Dialog.Root open={createOpen} onOpenChange={(next) => !creating && setCreateOpen(next)}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-[70] bg-slate-950/30 backdrop-blur-[2px]" />
          <Dialog.Content className="cocola-user-ui user-theme-cyan fixed left-1/2 top-1/2 z-[71] w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-background p-5 text-foreground shadow-2xl outline-none">
            <form onSubmit={(event) => void createAgent(event)}>
              <div className="flex items-start gap-3">
                <AgentAvatar
                  avatarKey={DEFAULT_AGENT_AVATAR_KEY}
                  avatarColor={DEFAULT_AGENT_AVATAR_COLOR}
                  className="size-10"
                />
                <div className="min-w-0 flex-1">
                  <div className="user-eyebrow">New</div>
                  <Dialog.Title className="text-base font-semibold">Create an Agent</Dialog.Title>
                  <Dialog.Description className="mt-1.5 text-sm leading-6 text-muted-foreground">
                    Give it a focused role. Instructions and Feishu can be configured next.
                  </Dialog.Description>
                </div>
                <Dialog.Close asChild>
                  <button
                    type="button"
                    disabled={creating}
                    aria-label="Close"
                    className="grid size-8 place-items-center rounded-lg text-muted-foreground hover:bg-muted"
                  >
                    <X className="size-4" />
                  </button>
                </Dialog.Close>
              </div>

              <div className="mt-5 space-y-4">
                <div className="space-y-1.5">
                  <Label htmlFor="agent-name">Name</Label>
                  <Input
                    id="agent-name"
                    autoFocus
                    maxLength={100}
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder="Data analyst"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="agent-description">Description</Label>
                  <Input
                    id="agent-description"
                    maxLength={500}
                    value={description}
                    onChange={(event) => setDescription(event.target.value)}
                    placeholder="Analyzes business data and explains findings clearly"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="agent-model">Model</Label>
                  <ModelSelectControl
                    id="agent-model"
                    value={modelID}
                    onValueChange={setModelID}
                    models={models}
                    className="h-9 shadow-none focus-visible:border-foreground/30 focus-visible:ring-blue-500/20"
                    contentClassName="cocola-user-ui"
                  />
                </div>
              </div>

              {error ? (
                <div className="mt-4 rounded-xl bg-destructive/10 px-3 py-2 text-sm text-destructive">
                  {error}
                </div>
              ) : null}

              <div className="mt-5 flex justify-end gap-2">
                <Dialog.Close asChild>
                  <button
                    type="button"
                    disabled={creating}
                    className="h-9 rounded-xl px-3 text-sm text-muted-foreground hover:bg-muted"
                  >
                    Cancel
                  </button>
                </Dialog.Close>
                <button
                  type="submit"
                  disabled={creating || !name.trim() || !selectedModel}
                  className="user-accent-btn inline-flex h-9 items-center gap-2 rounded-xl px-4 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {creating ? <Loader2 className="size-4 animate-spin" /> : null}
                  {creating ? "Creating…" : "Create Agent"}
                </button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </main>
  );
}

function MetricCard({
  tone,
  icon,
  label,
  value,
  detail,
}: {
  tone: "cyan" | "indigo" | "emerald" | "amber";
  icon: React.ReactNode;
  label: string;
  value: number;
  detail: string;
}) {
  return (
    <div className="user-metric-card" data-tone={tone}>
      <div className="user-metric-head">
        <span className="user-metric-glyph">{icon}</span>
        <span className="user-metric-key">{label}</span>
      </div>
      <div className="user-metric-val">{value}</div>
      <div className="user-metric-detail">{detail}</div>
    </div>
  );
}
