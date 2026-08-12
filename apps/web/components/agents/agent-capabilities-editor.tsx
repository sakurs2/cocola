"use client";

import {
  AlertTriangle,
  BookOpenText,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleCheck,
  FileText,
  Plus,
  Search,
  Trash2,
} from "lucide-react";
import { Button, Card, Chip, Input, Label, SearchField, TextField, Tooltip } from "@heroui/react";
import { ItemCard } from "@cocola/ui-compat/item-card";
import { ItemCardGroup } from "@cocola/ui-compat/item-card-group";
import { ListView } from "@cocola/ui-compat/list-view";
import { PressableFeedback } from "@cocola/ui-compat/pressable-feedback";
import { Sheet } from "@cocola/ui-compat/sheet";
import { useTranslations } from "next-intl";
import { useMemo, useState } from "react";
import { SkillIcon } from "@/components/ui/skill-icon";
import type { AgentKnowledgeSource, AgentSkillCatalogItem } from "@/lib/agents";
import { agentKnowledgeSourceKey } from "@/lib/agents";

type Props = {
  skills: AgentSkillCatalogItem[];
  skillIDs: string[];
  onSkillIDsChange: (value: string[]) => void;
  knowledgeSources: AgentKnowledgeSource[];
  onKnowledgeSourcesChange: (value: AgentKnowledgeSource[]) => void;
};

const REQUIRED_KNOWLEDGE_SKILLS: Record<AgentKnowledgeSource["type"], string[]> = {
  feishu_doc: ["lark-doc"],
  feishu_wiki: ["lark-wiki", "lark-doc"],
  feishu_sheet: ["lark-sheets"],
  feishu_base: ["lark-base"],
  cocola_wiki: [],
};

const SKILLS_PER_PAGE = 6;

type AgentWikiNode = {
  id: string;
  kind: "folder" | "file";
  name: string;
  logical_path?: string;
};

type KnowledgeNotice = {
  tone: "error" | "success";
  text: string;
};

export function AgentCapabilitiesEditor({
  skills,
  skillIDs,
  onSkillIDsChange,
  knowledgeSources,
  onKnowledgeSourcesChange,
}: Props) {
  const t = useTranslations("agents.capabilities");
  const [knowledgeURL, setKnowledgeURL] = useState("");
  const [knowledgeLabel, setKnowledgeLabel] = useState("");
  const [wikiPickerOpen, setWikiPickerOpen] = useState(false);
  const [wikiNodes, setWikiNodes] = useState<AgentWikiNode[]>([]);
  const [wikiQuery, setWikiQuery] = useState("");
  const [wikiLoading, setWikiLoading] = useState(false);
  const [wikiError, setWikiError] = useState("");
  const [skillMessage, setSkillMessage] = useState("");
  const [skillQuery, setSkillQuery] = useState("");
  const [skillPage, setSkillPage] = useState(1);
  const [knowledgeNotice, setKnowledgeNotice] = useState<KnowledgeNotice | null>(null);
  const selectedIDs = useMemo(() => new Set(skillIDs), [skillIDs]);
  const catalogByID = useMemo(() => new Map(skills.map((skill) => [skill.id, skill])), [skills]);
  const displayedSkills = useMemo(() => {
    const visibleCatalog = skills.filter((skill) => skill.available || selectedIDs.has(skill.id));
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
    return [...visibleCatalog, ...missing];
  }, [catalogByID, selectedIDs, skillIDs, skills]);
  const filteredSkills = useMemo(() => {
    const query = skillQuery.trim().toLowerCase();
    if (!query) return displayedSkills;
    return displayedSkills.filter((skill) => skill.name.toLowerCase().includes(query));
  }, [displayedSkills, skillQuery]);
  const skillPageCount = Math.max(1, Math.ceil(filteredSkills.length / SKILLS_PER_PAGE));
  const currentSkillPage = Math.min(skillPage, skillPageCount);
  const skillPageStart = (currentSkillPage - 1) * SKILLS_PER_PAGE;
  const paginatedSkills = filteredSkills.slice(skillPageStart, skillPageStart + SKILLS_PER_PAGE);
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
    setSkillMessage("");
    if (selectedIDs.has(skill.id)) {
      onSkillIDsChange(skillIDs.filter((id) => id !== skill.id));
      return;
    }
    if (!skill.available) return;
    if (skillIDs.length >= 32) {
      setSkillMessage(t("skillLimit"));
      return;
    }
    const duplicate = skillIDs.some((id) => catalogByID.get(id)?.runtime_id === skill.runtime_id);
    if (duplicate) {
      setSkillMessage(t("duplicateRuntime", { id: skill.runtime_id }));
      return;
    }
    onSkillIDsChange([...skillIDs, skill.id]);
  };

  const addKnowledge = () => {
    setKnowledgeNotice(null);
    const source = normalizeKnowledgeSource(knowledgeURL, knowledgeLabel, {
      feishu_doc: t("defaultLabels.document"),
      feishu_wiki: t("defaultLabels.wiki"),
      feishu_sheet: t("defaultLabels.sheet"),
      feishu_base: t("defaultLabels.base"),
    });
    if (!source) {
      setKnowledgeNotice({
        tone: "error",
        text: t("invalidUrl"),
      });
      return;
    }
    const sourceKey = agentKnowledgeSourceKey(source);
    if (
      knowledgeSources.length >= 10 ||
      knowledgeSources.some((item) => agentKnowledgeSourceKey(item) === sourceKey)
    ) {
      setKnowledgeNotice({
        tone: "error",
        text: knowledgeSources.length >= 10 ? t("knowledgeLimit") : t("duplicateKnowledge"),
      });
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
        setKnowledgeNotice({
          tone: "error",
          text: t("requiresDefault", { skills: missing.join(", ") }),
        });
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
          setKnowledgeNotice({
            tone: "error",
            text: t("requiresUnavailable", { skill: runtimeID }),
          });
          return;
        }
        const candidate =
          skills.find(
            (skill) =>
              skill.runtime_id === runtimeID && skill.available && skill.source === "personal",
          ) ?? skills.find((skill) => skill.runtime_id === runtimeID && skill.available);
        if (!candidate) {
          setKnowledgeNotice({
            tone: "error",
            text: t("requiresAdmin", { skill: runtimeID }),
          });
          return;
        }
        nextIDs.push(candidate.id);
        selectedRuntimeIDs.add(runtimeID);
      }
      if (nextIDs.length > 32) {
        setKnowledgeNotice({
          tone: "error",
          text: t("skillLimit"),
        });
        return;
      }
      onSkillIDsChange(nextIDs);
    }

    onKnowledgeSourcesChange([...knowledgeSources, source]);
    setKnowledgeURL("");
    setKnowledgeLabel("");
    setKnowledgeNotice({
      tone: "success",
      text: skillIDs.length > 0 ? t("customKnowledgeAdded") : t("knowledgeAdded"),
    });
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
      if (!response.ok) throw new Error(t("wikiLoadFailed"));
      const body = (await response.json()) as { nodes?: AgentWikiNode[] };
      setWikiNodes(Array.isArray(body.nodes) ? body.nodes : []);
    } catch (cause) {
      setWikiError(cause instanceof Error ? cause.message : t("wikiLoadFailed"));
    } finally {
      setWikiLoading(false);
    }
  };

  const addCocolaWikiKnowledge = (node: AgentWikiNode) => {
    setKnowledgeNotice(null);
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
      setKnowledgeNotice({
        tone: "error",
        text: knowledgeSources.length >= 10 ? t("knowledgeLimit") : t("duplicateKnowledge"),
      });
      return;
    }
    onKnowledgeSourcesChange([...knowledgeSources, source]);
    setWikiPickerOpen(false);
    setWikiQuery("");
    setKnowledgeNotice({
      tone: "success",
      text: t("wikiAdded"),
    });
  };

  return (
    <>
      <Card className="p-5">
        <Card.Header className="p-0">
          <Card.Title>{t("skills")}</Card.Title>
          <Card.Description>{t("skillsDescription")}</Card.Description>
        </Card.Header>
        <Card.Content className="p-0">
          <div className="bg-surface-secondary flex flex-wrap items-center justify-between gap-3 rounded-xl px-3 py-2.5">
            <span>
              <span className="block text-sm font-medium">
                {skillIDs.length === 0 ? t("usingDefault") : t("usingCustom")}
              </span>
              <span className="text-muted mt-1 block text-xs">
                {skillIDs.length === 0
                  ? t("defaultAvailable")
                  : t("selectedAvailable", { count: skillIDs.length })}
              </span>
            </span>
            <Chip color={skillIDs.length === 0 ? "accent" : "success"} size="sm" variant="soft">
              {skillIDs.length === 0 ? t("default") : t("selected", { count: skillIDs.length })}
            </Chip>
          </div>
          <SearchField
            aria-label={t("searchSkills")}
            className="mt-4 w-full"
            value={skillQuery}
            variant="secondary"
            onChange={(value) => {
              setSkillQuery(value);
              setSkillPage(1);
            }}
          >
            <SearchField.Group>
              <SearchField.SearchIcon>
                <Search className="size-4" />
              </SearchField.SearchIcon>
              <SearchField.Input placeholder={t("searchSkills")} />
              <SearchField.ClearButton />
            </SearchField.Group>
          </SearchField>
          <ItemCardGroup className="cocola-web-agent-skill-grid mt-4" columns={2} layout="grid">
            {paginatedSkills.map((skill) => {
              const selected = selectedIDs.has(skill.id);
              const disabled = !skill.available && !selected;
              const unavailable = !skill.available;
              const description = unavailable
                ? unavailableSkillDescription(skill, t)
                : skill.description ||
                  t("skillFallback", {
                    source: skill.source === "personal" ? t("personal") : t("shared"),
                  });
              return (
                <ItemCard<"button">
                  key={skill.id}
                  className={`relative min-h-[9.5rem] w-full overflow-hidden ${selected && !unavailable ? "ring-accent bg-accent-soft ring-2" : ""} ${unavailable ? "cocola-web-agent-skill-unavailable opacity-55" : "cursor-pointer"}`}
                  render={(props) => (
                    <button
                      {...props}
                      aria-label={
                        unavailable && selected
                          ? t("removeUnavailable", { name: skill.name })
                          : undefined
                      }
                      aria-pressed={selected}
                      disabled={disabled}
                      type="button"
                      onClick={() => toggleSkill(skill)}
                    />
                  )}
                >
                  {unavailable ? null : <PressableFeedback.Highlight />}
                  <ItemCard.Icon className="bg-transparent p-0">
                    <SkillIcon name={skill.name || skill.runtime_id} />
                  </ItemCard.Icon>
                  <ItemCard.Content>
                    <ItemCard.Title>{skill.name}</ItemCard.Title>
                    <ItemCard.Description>{description}</ItemCard.Description>
                    <span className="mt-3 flex flex-wrap gap-1.5">
                      <Chip size="sm" variant="soft">
                        {skill.source === "personal" ? t("personal") : t("shared")}
                      </Chip>
                      {unavailable ? (
                        <Chip color="warning" size="sm" variant="soft">
                          {skill.unavailable_reason === "disabled_by_administrator"
                            ? t("adminDisabled")
                            : t("unavailable")}
                        </Chip>
                      ) : null}
                    </span>
                  </ItemCard.Content>
                  <ItemCard.Action>
                    {unavailable && selected ? (
                      <span
                        className="bg-danger-soft text-danger grid size-7 place-items-center rounded-lg"
                        title={t("removeUnavailable", { name: skill.name })}
                      >
                        <Trash2 className="size-3.5" />
                      </span>
                    ) : (
                      <span
                        className={`grid size-6 place-items-center rounded-lg border ${selected ? "border-accent bg-accent text-white" : "border-separator text-transparent"}`}
                      >
                        <Check className="size-3.5" />
                      </span>
                    )}
                  </ItemCard.Action>
                </ItemCard>
              );
            })}
          </ItemCardGroup>
          {paginatedSkills.length === 0 ? (
            <div className="border-separator text-muted mt-4 flex min-h-40 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed text-center">
              <Search className="size-5" />
              <span className="text-sm">{t("noSkills")}</span>
            </div>
          ) : null}
          <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
            <span className="text-muted text-xs tabular-nums">
              {t("showing", {
                start: filteredSkills.length === 0 ? 0 : skillPageStart + 1,
                end: Math.min(skillPageStart + SKILLS_PER_PAGE, filteredSkills.length),
                total: filteredSkills.length,
              })}
            </span>
            <span className="flex items-center gap-2">
              <Tooltip delay={0}>
                <Button
                  isIconOnly
                  aria-label={t("previousPage")}
                  isDisabled={currentSkillPage === 1}
                  size="sm"
                  variant="outline"
                  onPress={() => setSkillPage((page) => Math.max(1, page - 1))}
                >
                  <ChevronLeft className="size-4" />
                </Button>
                <Tooltip.Content>{t("previous")}</Tooltip.Content>
              </Tooltip>
              <span className="text-muted min-w-14 text-center text-xs tabular-nums">
                {currentSkillPage} / {skillPageCount}
              </span>
              <Tooltip delay={0}>
                <Button
                  isIconOnly
                  aria-label={t("nextPage")}
                  isDisabled={currentSkillPage === skillPageCount}
                  size="sm"
                  variant="outline"
                  onPress={() => setSkillPage((page) => Math.min(skillPageCount, page + 1))}
                >
                  <ChevronRight className="size-4" />
                </Button>
                <Tooltip.Content>{t("next")}</Tooltip.Content>
              </Tooltip>
            </span>
          </div>
          {skillMessage ? <CapabilityFeedback text={skillMessage} tone="warning" /> : null}
        </Card.Content>
      </Card>

      <Card className="p-5">
        <Card.Header className="p-0">
          <Card.Title>{t("knowledge")}</Card.Title>
          <Card.Description>{t("knowledgeDescription")}</Card.Description>
        </Card.Header>
        <Card.Content className="p-0">
          <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_12rem_auto] lg:items-end">
            <TextField
              value={knowledgeURL}
              variant="secondary"
              onChange={(value) => {
                setKnowledgeURL(value);
                setKnowledgeNotice(null);
              }}
            >
              <Label>{t("feishuUrl")}</Label>
              <Input placeholder="https://example.feishu.cn/docx/..." />
            </TextField>
            <TextField
              value={knowledgeLabel}
              variant="secondary"
              onChange={(value) => {
                setKnowledgeLabel(value);
                setKnowledgeNotice(null);
              }}
            >
              <Label>{t("label")}</Label>
              <Input maxLength={100} placeholder={t("optional")} />
            </TextField>
            <Button className="cocola-web-page-primary-action" onPress={addKnowledge}>
              <Plus className="size-4" />
              {t("add")}
            </Button>
          </div>
          <Button className="mt-3" variant="outline" onPress={() => void openWikiPicker()}>
            <BookOpenText className="size-4" />
            {t("addFromWiki")}
          </Button>
          {knowledgeNotice ? (
            <CapabilityFeedback
              text={knowledgeNotice.text}
              tone={knowledgeNotice.tone === "error" ? "danger" : "success"}
            />
          ) : null}
          {knowledgeSources.length > 0 ? (
            <ListView
              aria-label={t("sourcesAria")}
              className="mt-4"
              items={knowledgeSources.map((source) => ({
                ...source,
                key: agentKnowledgeSourceKey(source),
              }))}
              selectionMode="none"
              variant="primary"
            >
              {(source) => (
                <ListView.Item id={source.key} textValue={source.label}>
                  <ListView.ItemContent>
                    <span className="bg-blue-500/15 text-blue-600 flex size-9 shrink-0 items-center justify-center rounded-xl dark:text-blue-300">
                      <BookOpenText className="size-4" />
                    </span>
                    <div className="flex min-w-0 flex-col">
                      <ListView.Title>{source.label}</ListView.Title>
                      <ListView.Description>
                        {source.type === "cocola_wiki" ? "Cocola Wiki" : source.url}
                      </ListView.Description>
                    </div>
                  </ListView.ItemContent>
                  <ListView.ItemAction>
                    <Tooltip delay={0}>
                      <Button
                        isIconOnly
                        aria-label={t("removeSource", { name: source.label })}
                        size="sm"
                        variant="ghost"
                        onPress={() =>
                          onKnowledgeSourcesChange(
                            knowledgeSources.filter(
                              (item) => agentKnowledgeSourceKey(item) !== source.key,
                            ),
                          )
                        }
                      >
                        <Trash2 className="size-4" />
                      </Button>
                      <Tooltip.Content>{t("removeSourceTooltip")}</Tooltip.Content>
                    </Tooltip>
                  </ListView.ItemAction>
                </ListView.Item>
              )}
            </ListView>
          ) : null}
          <div
            className={`mt-4 flex flex-wrap items-center justify-between gap-3 ${knowledgeSources.length === 0 ? "border-separator border-t pt-3" : ""}`}
          >
            {knowledgeSources.length === 0 ? (
              <span className="text-muted flex min-w-0 items-center gap-2 text-sm">
                <BookOpenText className="size-4 shrink-0" />
                {t("emptyKnowledge")}
              </span>
            ) : null}
            <p className="text-muted ml-auto text-xs tabular-nums">
              {t("sourceCount", { count: knowledgeSources.length })}
            </p>
          </div>
        </Card.Content>
      </Card>

      <Sheet
        isOpen={wikiPickerOpen}
        placement="right"
        onOpenChange={(open) => {
          if (!open) setWikiPickerOpen(false);
        }}
      >
        <Sheet.Backdrop>
          <Sheet.Content className="w-full md:w-[460px]">
            <Sheet.Dialog>
              <Sheet.CloseTrigger aria-label={t("closeWiki")} />
              <Sheet.Header>
                <Sheet.Heading>{t("addFromWiki")}</Sheet.Heading>
                <p className="text-muted text-sm">{t("wikiDescription")}</p>
              </Sheet.Header>
              <Sheet.Body>
                <SearchField
                  aria-label={t("searchWiki")}
                  value={wikiQuery}
                  variant="secondary"
                  onChange={setWikiQuery}
                >
                  <SearchField.Group>
                    <SearchField.SearchIcon>
                      <Search className="size-4" />
                    </SearchField.SearchIcon>
                    <SearchField.Input placeholder={t("searchWiki")} />
                    <SearchField.ClearButton />
                  </SearchField.Group>
                </SearchField>
                {wikiLoading ? (
                  <div className="text-muted mt-4 text-sm">{t("loadingWiki")}</div>
                ) : null}
                {wikiError ? (
                  <div className="bg-danger/10 text-danger mt-4 rounded-2xl px-4 py-3 text-sm">
                    {wikiError}
                  </div>
                ) : null}
                {!wikiLoading && !wikiError ? (
                  <ListView
                    aria-label={t("wikiFiles")}
                    className="mt-4"
                    items={filteredWikiFiles}
                    selectionMode="none"
                    variant="primary"
                    onAction={(key) => {
                      const node = filteredWikiFiles.find((item) => item.id === String(key));
                      if (node) addCocolaWikiKnowledge(node);
                    }}
                  >
                    {(node) => (
                      <ListView.Item
                        id={node.id}
                        textValue={`${node.name} ${node.logical_path ?? ""}`}
                      >
                        <ListView.ItemContent>
                          <span className="bg-blue-500/15 text-blue-600 flex size-9 shrink-0 items-center justify-center rounded-xl dark:text-blue-300">
                            <FileText className="size-4" />
                          </span>
                          <div className="flex min-w-0 flex-col">
                            <ListView.Title>{node.name}</ListView.Title>
                            <ListView.Description>
                              {node.logical_path || node.name}
                            </ListView.Description>
                          </div>
                        </ListView.ItemContent>
                        <ListView.ItemAction>
                          <Plus className="text-muted size-4" />
                        </ListView.ItemAction>
                      </ListView.Item>
                    )}
                  </ListView>
                ) : null}
              </Sheet.Body>
            </Sheet.Dialog>
          </Sheet.Content>
        </Sheet.Backdrop>
      </Sheet>
    </>
  );
}

type CapabilityTranslations = ReturnType<typeof useTranslations<"agents.capabilities">>;

function unavailableSkillDescription(
  skill: AgentSkillCatalogItem,
  t: CapabilityTranslations,
): string {
  if (skill.unavailable_reason === "disabled_by_administrator") {
    return t("disabledDescription");
  }
  if (skill.unavailable_reason === "missing") {
    return t("missingDescription");
  }
  return t("unavailableDescription");
}

function CapabilityFeedback({
  text,
  tone,
}: {
  text: string;
  tone: "danger" | "success" | "warning";
}) {
  const Icon = tone === "success" ? CircleCheck : AlertTriangle;
  const className =
    tone === "success"
      ? "bg-success/10 text-success"
      : tone === "warning"
        ? "bg-warning/10 text-warning"
        : "bg-danger/10 text-danger";
  return (
    <div className={`${className} mt-4 flex items-start gap-2 rounded-2xl px-4 py-3 text-sm`}>
      <Icon className="mt-0.5 size-4 shrink-0" />
      <span>{text}</span>
    </div>
  );
}

function normalizeKnowledgeSource(
  rawURL: string,
  rawLabel: string,
  defaultLabels: Record<Exclude<AgentKnowledgeSource["type"], "cocola_wiki">, string>,
): AgentKnowledgeSource | null {
  type FeishuKnowledgeType = Exclude<AgentKnowledgeSource["type"], "cocola_wiki">;
  try {
    const parsed = new URL(rawURL.trim());
    const host = parsed.hostname.toLowerCase();
    const hostAllowed = ["feishu.cn", "larkoffice.com", "larksuite.com"].some(
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
    return {
      type,
      label: rawLabel.trim() || defaultLabels[type],
      url: normalizedURL,
    };
  } catch {
    return null;
  }
}
