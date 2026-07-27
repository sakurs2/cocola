"use client";

import { Archive, ArrowLeft, Check, Loader2, Save } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { AgentAvatar } from "@/components/agents/agent-avatar";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";
import { FeishuConnectorCard } from "@/components/connectors/feishu-connector-card";
import { ActionConfirmDialog } from "@/components/ui/action-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SelectControl } from "@/components/ui/select-control";
import {
  AGENT_AVATAR_COLORS,
  AGENT_AVATAR_KEYS,
  agentResponseError,
  normalizeAgentModels,
  normalizeAgentRuntimes,
  type AgentModelOption,
  type AgentProfile,
} from "@/lib/agents";
import { cn } from "@/lib/utils";

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
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [instructions, setInstructions] = useState("");
  const [avatarKey, setAvatarKey] = useState("");
  const [avatarColor, setAvatarColor] = useState("");
  const [modelID, setModelID] = useState("");
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
        const [agentResponse, modelsResponse, runtimesResponse] = await Promise.all([
          fetch(`/api/agents/${encodeURIComponent(agentID)}`, {
            cache: "no-store",
            signal: controller.signal,
          }),
          fetch("/api/models", { cache: "no-store", signal: controller.signal }),
          fetch("/api/agent-runtimes", { cache: "no-store", signal: controller.signal }),
        ]);
        if (!agentResponse.ok) throw new Error(await agentResponseError(agentResponse));
        if (!modelsResponse.ok || !runtimesResponse.ok) {
          throw new Error("Model configuration is temporarily unavailable.");
        }
        const loaded = (await agentResponse.json()) as AgentProfile;
        const modelRows = normalizeAgentModels(await modelsResponse.json());
        const runtimes = normalizeAgentRuntimes(await runtimesResponse.json());
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
      <main className="grid h-full min-w-0 flex-1 place-items-center bg-background text-muted-foreground">
        <Loader2 className="size-5 animate-spin" />
      </main>
    );
  }

  if (!agent) {
    return (
      <main className="h-full min-w-0 flex-1 overflow-y-auto bg-background">
        <div className="mx-auto max-w-3xl px-8 py-10">
          <Link
            href="/agents"
            className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="size-4" /> Agents
          </Link>
          <div className="mt-8 rounded-2xl border border-red-500/20 bg-red-500/10 p-5 text-sm text-red-700">
            {error || "Agent not found"}
          </div>
        </div>
      </main>
    );
  }

  return (
    <main className="h-full min-w-0 flex-1 overflow-y-auto bg-background">
      <div className="mx-auto max-w-3xl px-8 py-10">
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <Link
              href="/agents"
              className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
            >
              <ArrowLeft className="size-4" /> Agents
            </Link>
            <div className="mt-4 flex items-center gap-3">
              <AgentAvatar
                avatarKey={avatarKey}
                avatarColor={avatarColor}
                className="size-11 rounded-2xl"
                iconClassName="size-5"
              />
              <div className="min-w-0">
                <h1 className="truncate text-2xl font-bold tracking-tight">{agent.name}</h1>
                <p className="mt-0.5 text-sm text-muted-foreground">
                  Changes apply to new conversations.
                </p>
              </div>
            </div>
          </div>
          <button
            type="button"
            onClick={() => void save()}
            disabled={
              saving ||
              !name.trim() ||
              !selectedModel ||
              instructionsTooLarge ||
              models.length === 0
            }
            className="inline-flex h-9 items-center gap-2 rounded-xl bg-blue-600 px-4 text-sm font-semibold text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {saving ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            {saving ? "Saving…" : "Save"}
          </button>
        </header>

        {error ? (
          <div className="mt-6 rounded-xl border border-red-500/20 bg-red-500/10 px-3.5 py-2.5 text-sm text-red-700">
            {error}
          </div>
        ) : null}

        <div className="mt-8 space-y-5">
          <section className="rounded-2xl border border-border bg-card p-5 shadow-sm">
            <div>
              <h2 className="text-sm font-semibold">Identity</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                How this Agent appears in Cocola and Feishu.
              </p>
            </div>
            <div className="mt-5 grid gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="agent-name">Name</Label>
                <Input
                  id="agent-name"
                  maxLength={100}
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="agent-description">Description</Label>
                <Input
                  id="agent-description"
                  maxLength={500}
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                  placeholder="What this Agent is best at"
                />
              </div>
              <div className="space-y-2">
                <Label>Icon</Label>
                <div className="flex flex-wrap gap-2">
                  {AGENT_AVATAR_KEYS.map((key) => (
                    <button
                      key={key}
                      type="button"
                      aria-label={`Use ${key} icon`}
                      aria-pressed={avatarKey === key}
                      onClick={() => setAvatarKey(key)}
                      className={cn(
                        "rounded-xl p-1.5 ring-2 ring-transparent transition hover:bg-muted",
                        avatarKey === key && "ring-blue-500/40",
                      )}
                    >
                      <AgentAvatar
                        avatarKey={key}
                        avatarColor={avatarColor}
                        className="size-8 rounded-lg"
                      />
                    </button>
                  ))}
                </div>
              </div>
              <div className="space-y-2">
                <Label>Color</Label>
                <div className="flex flex-wrap gap-2">
                  {AGENT_AVATAR_COLORS.map((color) => (
                    <button
                      key={color}
                      type="button"
                      aria-label={`Use ${color} color`}
                      aria-pressed={avatarColor === color}
                      onClick={() => setAvatarColor(color)}
                      className={cn(
                        "grid size-8 place-items-center rounded-full ring-2 ring-transparent ring-offset-2 ring-offset-background transition",
                        avatarColor === color && "ring-foreground/35",
                      )}
                    >
                      <span
                        className={cn(
                          "grid size-6 place-items-center rounded-full",
                          COLOR_SWATCHES[color],
                        )}
                      >
                        {avatarColor === color ? <Check className="size-3.5 text-white" /> : null}
                      </span>
                    </button>
                  ))}
                </div>
              </div>
            </div>
          </section>

          <section className="rounded-2xl border border-border bg-card p-5 shadow-sm">
            <h2 className="text-sm font-semibold">Instructions</h2>
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              Define the Agent&apos;s role and working rules. Platform and admin policies still take
              precedence; personal AGENTS.md rules are applied after these instructions.
            </p>
            <textarea
              value={instructions}
              onChange={(event) => setInstructions(event.target.value)}
              placeholder="You are a data analyst. Be concise, verify assumptions, and explain calculations..."
              className="mt-4 min-h-64 w-full resize-y rounded-xl border border-input bg-background px-3 py-2.5 font-mono text-sm leading-6 outline-none placeholder:text-muted-foreground focus:border-foreground/30 focus:ring-2 focus:ring-blue-500/20"
            />
            <div
              className={cn(
                "mt-2 text-right text-xs text-muted-foreground",
                instructionsTooLarge && "text-red-600",
              )}
            >
              {instructionsBytes.toLocaleString()} / {MAX_INSTRUCTIONS_BYTES.toLocaleString()} bytes
            </div>
          </section>

          <section className="rounded-2xl border border-border bg-card p-5 shadow-sm">
            <h2 className="text-sm font-semibold">Model</h2>
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              Conversations using this Agent always use this model.
            </p>
            <div className="mt-4 space-y-1.5">
              <Label htmlFor="agent-model">Fixed model</Label>
              <SelectControl
                id="agent-model"
                value={modelID}
                onValueChange={setModelID}
                options={[
                  ...(!models.some((model) => model.id === modelID)
                    ? [{ value: modelID, label: `${agent.model_alias} · unavailable` }]
                    : []),
                  ...models.map((model) => ({
                    value: model.id,
                    label: `${model.label}${model.provider ? ` · ${model.provider}` : ""}`,
                  })),
                ]}
                className="h-9 shadow-none focus-visible:border-foreground/30 focus-visible:ring-blue-500/20"
                contentClassName="cocola-user-ui"
              />
            </div>
          </section>

          <FeishuConnectorCard agentId={agent.id} />

          <section className="rounded-2xl border border-red-500/20 bg-red-500/[0.025] p-5">
            <div className="flex flex-wrap items-center justify-between gap-4">
              <div>
                <h2 className="text-sm font-semibold">Archive Agent</h2>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">
                  It will disappear from new chats. Existing conversation history remains.
                </p>
              </div>
              <button
                type="button"
                onClick={() => {
                  setError("");
                  setArchiveOpen(true);
                }}
                className="inline-flex h-9 items-center gap-2 rounded-xl border border-red-500/25 px-3 text-sm font-medium text-red-600 hover:bg-red-500/10"
              >
                <Archive className="size-4" />
                Archive
              </button>
            </div>
          </section>
        </div>
      </div>

      <ActionConfirmDialog
        open={archiveOpen}
        title="Archive this Agent?"
        description="Disconnect its Feishu bot first. Existing conversations stay in history, but this Agent cannot start or continue turns after it is archived."
        confirmLabel="Archive"
        busy={archiving}
        error={archiving ? null : error || null}
        tone="danger"
        icon={Archive}
        onOpenChange={setArchiveOpen}
        onConfirm={() => void archive()}
      />
    </main>
  );
}
