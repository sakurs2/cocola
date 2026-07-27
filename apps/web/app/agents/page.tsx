"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { ArrowRight, Bot, Loader2, Plus, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { AgentAvatar } from "@/components/agents/agent-avatar";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  DEFAULT_AGENT_AVATAR_COLOR,
  DEFAULT_AGENT_AVATAR_KEY,
  agentResponseError,
  configuredAgentRuntimeID,
  normalizeAgentModels,
  normalizeAgentRuntimes,
  type AgentModelOption,
  type AgentProfile,
} from "@/lib/agents";

export default function AgentsPage() {
  const router = useRouter();
  const [agents, setAgents] = useState<AgentProfile[]>([]);
  const [models, setModels] = useState<AgentModelOption[]>([]);
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
        const [modelsResponse, runtimesResponse, configResponse] = await Promise.all([
          fetch("/api/models", { cache: "no-store", signal: controller.signal }),
          fetch("/api/agent-runtimes", { cache: "no-store", signal: controller.signal }),
          fetch("/api/product-config", { cache: "no-store", signal: controller.signal }),
        ]);
        if (!modelsResponse.ok || !runtimesResponse.ok || !configResponse.ok) {
          throw new Error("Model configuration is temporarily unavailable.");
        }
        const modelRows = normalizeAgentModels(await modelsResponse.json());
        const runtimes = normalizeAgentRuntimes(await runtimesResponse.json());
        const configuredRuntimeID = configuredAgentRuntimeID(await configResponse.json());
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
      <div className="mx-auto max-w-5xl px-8 py-10">
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
          <section className="mt-8 grid gap-4 sm:grid-cols-2">
            {agents.map((agent) => (
              <Link
                key={agent.id}
                href={`/agents/${encodeURIComponent(agent.id)}`}
                className="group flex min-h-36 items-start gap-4 rounded-2xl border border-border bg-card p-5 shadow-sm transition hover:-translate-y-0.5 hover:border-foreground/15 hover:shadow-md"
              >
                <AgentAvatar
                  avatarKey={agent.avatar_key}
                  avatarColor={agent.avatar_color}
                  className="size-11 rounded-2xl"
                  iconClassName="size-5"
                />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-semibold">{agent.name}</span>
                  <span className="mt-1 line-clamp-2 block text-sm leading-5 text-muted-foreground">
                    {agent.description || "No description"}
                  </span>
                  <span className="mt-4 block truncate text-xs text-muted-foreground">
                    {agent.model_alias}
                  </span>
                </span>
                <ArrowRight className="mt-1 size-4 shrink-0 text-muted-foreground transition group-hover:translate-x-0.5 group-hover:text-foreground" />
              </Link>
            ))}
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
                  <select
                    id="agent-model"
                    value={modelID}
                    onChange={(event) => setModelID(event.target.value)}
                    className="h-9 w-full rounded-xl border border-input bg-background px-3 text-sm outline-none focus:border-foreground/30 focus:ring-2 focus:ring-blue-500/20"
                  >
                    {models.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.label}
                        {model.provider ? ` · ${model.provider}` : ""}
                      </option>
                    ))}
                  </select>
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
