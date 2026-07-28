"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { ArrowRight, Bot, Loader2, Plus, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { AgentAvatar } from "@/components/agents/agent-avatar";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ModelIcon } from "@/components/ui/model-icon";
import { SelectControl } from "@/components/ui/select-control";
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

  return (
    <main className="h-full min-w-0 flex-1 overflow-y-auto bg-background">
      <div className="mx-auto max-w-7xl px-6 py-10 sm:px-8">
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-1">
            <h1 className="text-2xl font-bold tracking-tight">Agents</h1>
            <p className="text-sm text-muted-foreground">
              Create focused assistants with their own instructions, model, and Feishu bot.
            </p>
          </div>
          <button
            type="button"
            onClick={() => {
              setError("");
              setCreateOpen(true);
            }}
            disabled={catalogLoading || !runtimeID || models.length === 0}
            className="inline-flex h-9 items-center gap-2 rounded-xl px-3.5 text-sm font-semibold text-white shadow-xs transition-opacity brand-gradient hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Plus className="size-4" />
            New Agent
          </button>
        </header>

        {error && !createOpen ? (
          <div className="mt-6 rounded-xl border border-red-500/20 bg-red-500/10 px-3.5 py-2.5 text-sm text-red-600">
            {error}
          </div>
        ) : null}

        {loading ? (
          <div className="grid min-h-64 place-items-center text-muted-foreground">
            <Loader2 className="size-5 animate-spin" />
          </div>
        ) : agents.length === 0 ? (
          <section className="mt-8 grid min-h-72 place-items-center rounded-2xl border border-dashed border-border bg-muted/20 px-6 text-center">
            <div className="max-w-sm">
              <span className="mx-auto grid size-12 place-items-center rounded-2xl bg-blue-500/10 text-blue-600">
                <Bot className="size-6" />
              </span>
              <h2 className="mt-4 text-base font-semibold">No Agents yet</h2>
              <p className="mt-1 text-sm leading-6 text-muted-foreground">
                Start with one clear role. You can still use standard chat without selecting an
                Agent.
              </p>
            </div>
          </section>
        ) : (
          <section
            aria-label="Your Agents"
            className="mt-8 rounded-[1.75rem] border border-border/60 bg-muted/35 p-2.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.9),0_18px_46px_-40px_rgba(15,23,42,0.45)] sm:p-3"
          >
            <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
              {agents.map((agent) => {
                const model = modelsByID.get(agent.model_route_id);
                const selectedSkills = (agent.skill_ids ?? [])
                  .map((id) => skillCatalog.find((skill) => skill.id === id))
                  .filter((skill): skill is AgentSkillCatalogItem => Boolean(skill?.available));
                return (
                  <Link
                    key={agent.id}
                    href={`/agents/${encodeURIComponent(agent.id)}`}
                    className="group flex min-h-[15.75rem] min-w-0 flex-col rounded-[1.35rem] border border-transparent bg-card p-5 text-left shadow-[0_1px_0_rgba(15,23,42,0.025),0_12px_32px_-30px_rgba(15,23,42,0.32)] transition-[background-color,border-color,box-shadow] duration-200 hover:border-border hover:bg-muted/75 hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.75),0_1px_2px_rgba(15,23,42,0.04),0_16px_36px_-32px_rgba(15,23,42,0.38)] focus-visible:border-foreground/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/25 focus-visible:ring-offset-2"
                  >
                    <span className="flex min-w-0 items-center gap-3">
                      <AgentAvatar
                        avatarKey={agent.avatar_key}
                        avatarColor={agent.avatar_color}
                        className="size-12 rounded-full shadow-[0_2px_5px_-4px_rgba(15,23,42,0.45)]"
                        iconClassName="size-[1.35rem]"
                      />
                      <span className="min-w-0 flex-1 truncate text-[17px] font-semibold tracking-[-0.02em]">
                        {agent.name}
                      </span>
                      <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-foreground text-background opacity-0 transition-[opacity] duration-200 group-hover:opacity-100 group-focus-visible:opacity-100">
                        <ArrowRight className="size-[18px]" />
                      </span>
                    </span>

                    <span className="mt-7 line-clamp-2 min-h-12 text-sm leading-6 text-muted-foreground">
                      {agent.description || "No description"}
                    </span>

                    <span className="mt-4 flex min-h-7 flex-wrap items-start gap-1.5">
                      {(agent.skill_ids ?? []).length === 0 ? (
                        <span className="rounded-md bg-muted px-2 py-1 text-[11px] font-medium text-muted-foreground">
                          Default skills
                        </span>
                      ) : selectedSkills.length === 0 ? (
                        <span className="rounded-md bg-amber-500/10 px-2 py-1 text-[11px] font-medium text-amber-700">
                          Custom skills unavailable
                        </span>
                      ) : (
                        <>
                          {selectedSkills.slice(0, 2).map((skill) => (
                            <span
                              key={skill.id}
                              className="max-w-28 truncate rounded-md bg-muted px-2 py-1 text-[11px] font-medium text-muted-foreground"
                            >
                              {skill.name}
                            </span>
                          ))}
                          {selectedSkills.length > 2 ? (
                            <span className="rounded-md bg-muted px-2 py-1 text-[11px] font-medium text-muted-foreground">
                              +{selectedSkills.length - 2}
                            </span>
                          ) : null}
                        </>
                      )}
                    </span>

                    <span className="mt-auto inline-flex max-w-full items-center self-start rounded-lg border border-transparent bg-muted/80 px-2.5 py-1.5 text-xs font-medium text-muted-foreground transition-[background-color,border-color,color] duration-200 group-hover:border-border group-hover:bg-background/55 group-hover:text-foreground">
                      <ModelIcon icon={model?.icon} className="mr-1.5 size-4" bare />
                      <span className="truncate">{agent.model_alias}</span>
                    </span>
                  </Link>
                );
              })}
            </div>
          </section>
        )}
      </div>

      <Dialog.Root open={createOpen} onOpenChange={(next) => !creating && setCreateOpen(next)}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-[70] bg-slate-950/30 backdrop-blur-[2px]" />
          <Dialog.Content className="cocola-user-ui fixed left-1/2 top-1/2 z-[71] w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-border bg-background p-5 text-foreground shadow-2xl outline-none">
            <form onSubmit={(event) => void createAgent(event)}>
              <div className="flex items-start gap-3">
                <AgentAvatar
                  avatarKey={DEFAULT_AGENT_AVATAR_KEY}
                  avatarColor={DEFAULT_AGENT_AVATAR_COLOR}
                  className="size-10"
                />
                <div className="min-w-0 flex-1">
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
                  <SelectControl
                    id="agent-model"
                    value={modelID}
                    onValueChange={setModelID}
                    options={models.map((model) => ({
                      value: model.id,
                      label: `${model.label}${model.provider ? ` · ${model.provider}` : ""}`,
                    }))}
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
                  className="inline-flex h-9 items-center gap-2 rounded-xl bg-blue-600 px-4 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
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
