"use client";

import { AlertTriangle, BookOpenText, Check, Loader2, Plus, Search, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type {
  AgentKnowledgeSource,
  AgentSkillCatalogItem,
  AgentSuggestedPrompt,
} from "@/lib/agents";
import { agentKnowledgeSourceKey } from "@/lib/agents";
import { cn } from "@/lib/utils";

export type KnowledgeCheckStatus =
  | "ready"
  | "connector_required"
  | "permission_required"
  | "not_found"
  | "temporarily_unavailable"
  | "unsupported";

type Props = {
  skills: AgentSkillCatalogItem[];
  skillIDs: string[];
  onSkillIDsChange: (value: string[]) => void;
  knowledgeSources: AgentKnowledgeSource[];
  onKnowledgeSourcesChange: (value: AgentKnowledgeSource[]) => void;
  knowledgeStatuses: Record<string, KnowledgeCheckStatus>;
  checkingKnowledge: boolean;
  knowledgeCheckDisabled: boolean;
  onCheckKnowledge: () => void;
  suggestedPrompts: AgentSuggestedPrompt[];
  onSuggestedPromptsChange: (value: AgentSuggestedPrompt[]) => void;
};

const REQUIRED_KNOWLEDGE_SKILLS: Record<AgentKnowledgeSource["type"], string[]> = {
  feishu_doc: ["lark-doc"],
  feishu_wiki: ["lark-wiki", "lark-doc"],
  feishu_sheet: ["lark-sheets"],
  feishu_base: ["lark-base"],
  cocola_wiki: [],
};

const KNOWLEDGE_STATUS_LABELS: Record<KnowledgeCheckStatus, string> = {
  ready: "Ready",
  connector_required: "Connector required",
  permission_required: "Permission required",
  not_found: "Not found",
  temporarily_unavailable: "Temporarily unavailable",
  unsupported: "Unavailable",
};

type AgentWikiNode = {
  id: string;
  kind: "folder" | "file";
  name: string;
  logical_path?: string;
};

export function AgentCapabilitiesEditor({
  skills,
  skillIDs,
  onSkillIDsChange,
  knowledgeSources,
  onKnowledgeSourcesChange,
  knowledgeStatuses,
  checkingKnowledge,
  knowledgeCheckDisabled,
  onCheckKnowledge,
  suggestedPrompts,
  onSuggestedPromptsChange,
}: Props) {
  const [knowledgeURL, setKnowledgeURL] = useState("");
  const [knowledgeLabel, setKnowledgeLabel] = useState("");
  const [wikiPickerOpen, setWikiPickerOpen] = useState(false);
  const [wikiNodes, setWikiNodes] = useState<AgentWikiNode[]>([]);
  const [wikiQuery, setWikiQuery] = useState("");
  const [wikiLoading, setWikiLoading] = useState(false);
  const [wikiError, setWikiError] = useState("");
  const [capabilityMessage, setCapabilityMessage] = useState("");
  const selectedIDs = useMemo(() => new Set(skillIDs), [skillIDs]);
  const catalogByID = useMemo(() => new Map(skills.map((skill) => [skill.id, skill])), [skills]);
  const displayedSkills = useMemo(() => {
    const missing = skillIDs
      .filter((id) => !catalogByID.has(id))
      .map<AgentSkillCatalogItem>((id) => ({
        id,
        runtime_id: id,
        name: id,
        description: "",
        source: "personal",
        available: false,
        default_enabled: false,
        unavailable_reason: "missing",
      }));
    return [...skills, ...missing];
  }, [catalogByID, skillIDs, skills]);
  const filteredWikiFiles = useMemo(() => {
    const query = wikiQuery.trim().toLowerCase();
    return wikiNodes
      .filter(
        (node) =>
          node.kind === "file" &&
          (!query ||
            node.name.toLowerCase().includes(query) ||
            (node.logical_path ?? "").toLowerCase().includes(query)),
      )
      .slice(0, 30);
  }, [wikiNodes, wikiQuery]);

  const toggleSkill = (skill: AgentSkillCatalogItem) => {
    setCapabilityMessage("");
    if (selectedIDs.has(skill.id)) {
      onSkillIDsChange(skillIDs.filter((id) => id !== skill.id));
      return;
    }
    if (!skill.available || skillIDs.length >= 32) return;
    const duplicate = skillIDs.some((id) => catalogByID.get(id)?.runtime_id === skill.runtime_id);
    if (duplicate) {
      setCapabilityMessage(
        `Only one Skill with the runtime ID “${skill.runtime_id}” can be selected.`,
      );
      return;
    }
    onSkillIDsChange([...skillIDs, skill.id]);
  };

  const addKnowledge = () => {
    setCapabilityMessage("");
    const source = normalizeKnowledgeSource(knowledgeURL, knowledgeLabel);
    if (!source) {
      setCapabilityMessage("Enter a supported HTTPS Feishu or Lark document URL.");
      return;
    }
    const sourceKey = agentKnowledgeSourceKey(source);
    if (
      knowledgeSources.length >= 10 ||
      knowledgeSources.some((item) => agentKnowledgeSourceKey(item) === sourceKey)
    ) {
      setCapabilityMessage(
        knowledgeSources.length >= 10
          ? "An Agent can have up to 10 Knowledge sources."
          : "This Knowledge source is already configured.",
      );
      return;
    }

    const requiredRuntimeIDs = REQUIRED_KNOWLEDGE_SKILLS[source.type];
    if (skillIDs.length === 0) {
      const missing = requiredRuntimeIDs.filter(
        (runtimeID) =>
          !skills.some(
            (skill) => skill.runtime_id === runtimeID && skill.available && skill.default_enabled,
          ),
      );
      if (missing.length > 0) {
        setCapabilityMessage(
          `This source requires ${missing.join(", ")}, but it is not available in your default skills.`,
        );
        return;
      }
    } else {
      const nextIDs = [...skillIDs];
      const selectedSkills = skillIDs.flatMap((id) => {
        const skill = catalogByID.get(id);
        return skill ? [skill] : [];
      });
      const selectedRuntimeIDs = new Set(
        selectedSkills.filter((skill) => skill.available).map((skill) => skill.runtime_id),
      );
      for (const runtimeID of requiredRuntimeIDs) {
        if (selectedRuntimeIDs.has(runtimeID)) continue;
        if (selectedSkills.some((skill) => skill.runtime_id === runtimeID && !skill.available)) {
          setCapabilityMessage(
            `This source requires ${runtimeID}, but the selected Skill is unavailable. Remove it before choosing a replacement.`,
          );
          return;
        }
        const candidate =
          skills.find(
            (skill) =>
              skill.runtime_id === runtimeID && skill.available && skill.source === "personal",
          ) ?? skills.find((skill) => skill.runtime_id === runtimeID && skill.available);
        if (!candidate) {
          setCapabilityMessage(
            `This source requires ${runtimeID}, but an administrator has made it unavailable.`,
          );
          return;
        }
        nextIDs.push(candidate.id);
        selectedRuntimeIDs.add(runtimeID);
      }
      if (nextIDs.length > 32) {
        setCapabilityMessage("An Agent can select up to 32 Skills.");
        return;
      }
      onSkillIDsChange(nextIDs);
    }

    onKnowledgeSourcesChange([...knowledgeSources, source]);
    setKnowledgeURL("");
    setKnowledgeLabel("");
    setCapabilityMessage(
      skillIDs.length > 0
        ? "Required Skills were added to this Agent’s custom skill set. Save to check access."
        : "Knowledge added. Save to check access.",
    );
  };

  const openWikiPicker = async () => {
    if (wikiPickerOpen) {
      setWikiPickerOpen(false);
      return;
    }
    setWikiPickerOpen(true);
    if (wikiNodes.length > 0 || wikiLoading) return;
    setWikiLoading(true);
    setWikiError("");
    try {
      const response = await fetch("/api/wiki/tree", { cache: "no-store" });
      if (!response.ok) throw new Error("Could not load Cocola Wiki.");
      const body = (await response.json()) as { nodes?: AgentWikiNode[] };
      setWikiNodes(Array.isArray(body.nodes) ? body.nodes : []);
    } catch (cause) {
      setWikiError(cause instanceof Error ? cause.message : "Could not load Cocola Wiki.");
    } finally {
      setWikiLoading(false);
    }
  };

  const addCocolaWikiKnowledge = (node: AgentWikiNode) => {
    setCapabilityMessage("");
    const source: AgentKnowledgeSource = {
      type: "cocola_wiki",
      label: node.name,
      node_id: node.id,
    };
    const key = agentKnowledgeSourceKey(source);
    if (
      knowledgeSources.length >= 10 ||
      knowledgeSources.some((item) => agentKnowledgeSourceKey(item) === key)
    ) {
      setCapabilityMessage(
        knowledgeSources.length >= 10
          ? "An Agent can have up to 10 Knowledge sources."
          : "This Knowledge source is already configured.",
      );
      return;
    }
    onKnowledgeSourcesChange([...knowledgeSources, source]);
    setWikiPickerOpen(false);
    setWikiQuery("");
    setCapabilityMessage("Cocola Wiki file added. Save to check access.");
  };

  return (
    <>
      <section className="rounded-2xl border border-border bg-card p-5 shadow-sm">
        <h2 className="text-sm font-semibold">Skills (Optional)</h2>
        <p className="mt-1 text-sm leading-6 text-muted-foreground">
          Leave empty to inherit your default skills. Selecting any skill switches this Agent to a
          custom skill set.
        </p>
        <div className="mt-4 rounded-xl border border-border bg-muted/20 p-3">
          <p className="text-sm font-medium">
            {skillIDs.length === 0 ? "Using default skills" : "Using a custom skill set"}
          </p>
          <p className="mt-1 text-xs leading-5 text-muted-foreground">
            {skillIDs.length === 0
              ? "All skills enabled by default will be available to this Agent."
              : `Only the ${skillIDs.length} selected skills will be available to this Agent.`}
          </p>
        </div>
        <div className="mt-4 grid gap-2 sm:grid-cols-2">
          {displayedSkills.map((skill) => {
            const selected = selectedIDs.has(skill.id);
            return (
              <button
                key={skill.id}
                type="button"
                aria-pressed={selected}
                onClick={() => toggleSkill(skill)}
                className={cn(
                  "flex min-w-0 items-start gap-3 rounded-xl border p-3 text-left transition-colors",
                  selected
                    ? "border-blue-500/35 bg-blue-500/[0.06]"
                    : "border-border hover:bg-muted/40",
                  !skill.available && !selected && "cursor-not-allowed opacity-65",
                )}
              >
                <span
                  className={cn(
                    "mt-0.5 grid size-4 shrink-0 place-items-center rounded border",
                    selected
                      ? "border-blue-600 bg-blue-600 text-white"
                      : "border-input bg-background",
                  )}
                >
                  {selected ? <Check className="size-3" /> : null}
                </span>
                <span className="min-w-0">
                  <span className="flex flex-wrap items-center gap-2 text-sm font-medium">
                    <span className="truncate">{skill.name}</span>
                    {!skill.available ? (
                      <span className="rounded-md bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-amber-700">
                        Unavailable
                      </span>
                    ) : null}
                  </span>
                  <span className="mt-0.5 line-clamp-2 block text-xs leading-5 text-muted-foreground">
                    {!skill.available
                      ? "This skill was disabled by an administrator and will not be available to the Agent."
                      : skill.description ||
                        `${skill.source === "personal" ? "Personal" : "Shared"} Skill`}
                  </span>
                </span>
              </button>
            );
          })}
        </div>
      </section>

      <section className="rounded-2xl border border-border bg-card p-5 shadow-sm">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">Knowledge (Optional)</h2>
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              Add Cocola Wiki files or remote Feishu references. The Agent reads them only when
              relevant.
            </p>
          </div>
          <button
            type="button"
            onClick={onCheckKnowledge}
            disabled={checkingKnowledge || knowledgeCheckDisabled || knowledgeSources.length === 0}
            title={
              knowledgeCheckDisabled ? "Save changes before checking Knowledge access." : undefined
            }
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-2.5 text-xs font-medium hover:bg-muted disabled:opacity-50"
          >
            {checkingKnowledge ? <Loader2 className="size-3.5 animate-spin" /> : null}
            Check access
          </button>
        </div>
        <div className="mt-4 grid gap-3 sm:grid-cols-[1fr_11rem_auto]">
          <Input
            value={knowledgeURL}
            onChange={(event) => setKnowledgeURL(event.target.value)}
            placeholder="https://example.feishu.cn/docx/..."
          />
          <Input
            value={knowledgeLabel}
            onChange={(event) => setKnowledgeLabel(event.target.value)}
            maxLength={100}
            placeholder="Label (optional)"
          />
          <button
            type="button"
            onClick={addKnowledge}
            className="inline-flex h-9 items-center justify-center gap-1.5 rounded-xl bg-foreground px-3 text-sm font-medium text-background hover:opacity-90"
          >
            <Plus className="size-4" /> Add
          </button>
        </div>
        <div className="mt-3">
          <button
            type="button"
            onClick={() => void openWikiPicker()}
            className="inline-flex h-9 items-center gap-2 rounded-xl border border-border px-3 text-sm font-medium hover:bg-muted"
          >
            <BookOpenText className="size-4" />
            Add from Cocola Wiki
          </button>
          {wikiPickerOpen ? (
            <div className="mt-3 overflow-hidden rounded-xl border border-border bg-background">
              <div className="relative border-b border-border p-3">
                <Search className="pointer-events-none absolute left-6 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={wikiQuery}
                  onChange={(event) => setWikiQuery(event.target.value)}
                  placeholder="Search Cocola Wiki files"
                  className="pl-9"
                />
              </div>
              <div className="max-h-56 overflow-y-auto p-2">
                {wikiLoading ? (
                  <div className="flex items-center gap-2 px-2 py-3 text-sm text-muted-foreground">
                    <Loader2 className="size-4 animate-spin" /> Loading Wiki…
                  </div>
                ) : wikiError ? (
                  <p className="px-2 py-3 text-sm text-red-600">{wikiError}</p>
                ) : filteredWikiFiles.length === 0 ? (
                  <p className="px-2 py-3 text-sm text-muted-foreground">No matching Wiki files.</p>
                ) : (
                  filteredWikiFiles.map((node) => (
                    <button
                      key={node.id}
                      type="button"
                      onClick={() => addCocolaWikiKnowledge(node)}
                      className="flex w-full min-w-0 items-center gap-3 rounded-lg px-2.5 py-2 text-left hover:bg-muted"
                    >
                      <BookOpenText className="size-4 shrink-0 text-muted-foreground" />
                      <span className="min-w-0">
                        <span className="block truncate text-sm font-medium">{node.name}</span>
                        <span className="block truncate text-xs text-muted-foreground">
                          {node.logical_path || node.name}
                        </span>
                      </span>
                    </button>
                  ))
                )}
              </div>
            </div>
          ) : null}
        </div>
        {knowledgeSources.length > 0 ? (
          <div className="mt-4 divide-y divide-border rounded-xl border border-border">
            {knowledgeSources.map((source) => {
              const sourceKey = agentKnowledgeSourceKey(source);
              const status = knowledgeStatuses[sourceKey];
              return (
                <div key={sourceKey} className="flex min-w-0 items-center gap-3 p-3">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">{source.label}</span>
                    <span className="block truncate text-xs text-muted-foreground">
                      {source.type === "cocola_wiki" ? "Cocola Wiki" : source.url}
                    </span>
                  </span>
                  <span
                    className={cn(
                      "shrink-0 text-xs font-medium",
                      status === "ready" ? "text-emerald-600" : "text-muted-foreground",
                    )}
                  >
                    {status ? KNOWLEDGE_STATUS_LABELS[status] : "Not checked"}
                  </span>
                  <button
                    type="button"
                    aria-label={`Remove ${source.label}`}
                    onClick={() =>
                      onKnowledgeSourcesChange(
                        knowledgeSources.filter(
                          (item) => agentKnowledgeSourceKey(item) !== sourceKey,
                        ),
                      )
                    }
                    className="grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground hover:bg-muted hover:text-foreground"
                  >
                    <Trash2 className="size-4" />
                  </button>
                </div>
              );
            })}
          </div>
        ) : null}
      </section>

      <section className="rounded-2xl border border-border bg-card p-5 shadow-sm">
        <h2 className="text-sm font-semibold">Suggested prompts (Optional)</h2>
        <p className="mt-1 text-sm leading-6 text-muted-foreground">
          These starters fill the chat input. The user always decides whether to send them.
        </p>
        <div className="mt-4 space-y-3">
          {suggestedPrompts.map((suggestion, index) => (
            <div key={index} className="rounded-xl border border-border p-3">
              <div className="flex items-center gap-2">
                <Label htmlFor={`suggested-title-${index}`} className="sr-only">
                  Prompt title
                </Label>
                <Input
                  id={`suggested-title-${index}`}
                  value={suggestion.title}
                  maxLength={80}
                  onChange={(event) =>
                    onSuggestedPromptsChange(
                      suggestedPrompts.map((item, itemIndex) =>
                        itemIndex === index ? { ...item, title: event.target.value } : item,
                      ),
                    )
                  }
                  placeholder="Analyze a report"
                />
                <button
                  type="button"
                  aria-label="Remove suggested prompt"
                  onClick={() =>
                    onSuggestedPromptsChange(
                      suggestedPrompts.filter((_, itemIndex) => itemIndex !== index),
                    )
                  }
                  className="grid size-9 shrink-0 place-items-center rounded-lg text-muted-foreground hover:bg-muted hover:text-foreground"
                >
                  <Trash2 className="size-4" />
                </button>
              </div>
              <textarea
                value={suggestion.prompt}
                onChange={(event) =>
                  onSuggestedPromptsChange(
                    suggestedPrompts.map((item, itemIndex) =>
                      itemIndex === index ? { ...item, prompt: event.target.value } : item,
                    ),
                  )
                }
                maxLength={4096}
                placeholder="Analyze this report and summarize the most important findings."
                className="mt-2 min-h-20 w-full resize-y rounded-lg border border-input bg-background px-3 py-2 text-sm outline-none focus:border-foreground/30 focus:ring-2 focus:ring-blue-500/20"
              />
            </div>
          ))}
          {suggestedPrompts.length < 4 ? (
            <button
              type="button"
              onClick={() =>
                onSuggestedPromptsChange([...suggestedPrompts, { title: "", prompt: "" }])
              }
              className="inline-flex h-9 items-center gap-1.5 rounded-xl border border-border px-3 text-sm font-medium hover:bg-muted"
            >
              <Plus className="size-4" /> Add suggested prompt
            </button>
          ) : null}
        </div>
      </section>

      {capabilityMessage ? (
        <div className="flex items-start gap-2 rounded-xl border border-amber-500/20 bg-amber-500/[0.06] px-3.5 py-2.5 text-sm text-amber-800">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          <span>{capabilityMessage}</span>
        </div>
      ) : null}
    </>
  );
}

function normalizeKnowledgeSource(rawURL: string, rawLabel: string): AgentKnowledgeSource | null {
  type FeishuKnowledgeType = Exclude<AgentKnowledgeSource["type"], "cocola_wiki">;
  try {
    const parsed = new URL(rawURL.trim());
    const host = parsed.hostname.toLowerCase();
    const hostAllowed = ["feishu.cn", "larksuite.com"].some(
      (suffix) => host === suffix || host.endsWith(`.${suffix}`),
    );
    const parts = parsed.pathname.split("/").filter(Boolean);
    const root = parts[0];
    const token = parts[1];
    if (
      parsed.protocol !== "https:" ||
      parsed.username ||
      parsed.password ||
      parsed.port ||
      !hostAllowed ||
      !root ||
      !token ||
      !/^[A-Za-z0-9_-]{1,256}$/.test(token)
    ) {
      return null;
    }
    const typeByRoot: Record<string, FeishuKnowledgeType> = {
      docx: "feishu_doc",
      wiki: "feishu_wiki",
      sheets: "feishu_sheet",
      base: "feishu_base",
      bitable: "feishu_base",
    };
    const normalizedRoot = root.toLowerCase();
    const type = typeByRoot[normalizedRoot];
    if (!type) return null;
    const normalizedURL = `https://${host}/${normalizedRoot}/${token}`;
    const defaultLabels: Record<FeishuKnowledgeType, string> = {
      feishu_doc: "Feishu document",
      feishu_wiki: "Feishu Wiki",
      feishu_sheet: "Feishu Sheet",
      feishu_base: "Feishu Base",
    };
    return {
      type,
      label: rawLabel.trim() || defaultLabels[type],
      url: normalizedURL,
    };
  } catch {
    return null;
  }
}
