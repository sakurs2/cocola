"use client";

import { CircleAlert, LoaderCircle, Save, ScrollText } from "lucide-react";
import { Button, Card, Label, Switch, TextArea, TextField } from "@heroui/react";
import { useTranslations } from "next-intl";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AdminAlert, AdminDrawer } from "@/components/admin/admin-ui";
import { ToolboxCard } from "./toolbox-card";

type AgentPrompt = {
  content: string;
  enabled: boolean;
  version: number;
};

const EMPTY_PROMPT: AgentPrompt = {
  content: "",
  enabled: false,
  version: 0,
};

export function SystemPromptTool({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useTranslations("admin.toolboxPage.systemPrompt");
  const [prompt, setPrompt] = useState<AgentPrompt>(EMPTY_PROMPT);
  const [draft, setDraft] = useState("");
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [saveError, setSaveError] = useState("");

  const dirty = useMemo(
    () => draft !== prompt.content || enabled !== prompt.enabled,
    [draft, enabled, prompt.content, prompt.enabled],
  );

  const applyPrompt = useCallback((nextPrompt: AgentPrompt) => {
    setPrompt(nextPrompt);
    setDraft(nextPrompt.content ?? "");
    setEnabled(Boolean(nextPrompt.enabled));
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError("");
    try {
      const response = await fetch("/api/admin/agent-prompts/global", { cache: "no-store" });
      if (!response.ok) throw new Error(await readError(response));
      applyPrompt((await response.json()) as AgentPrompt);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : String(error));
    } finally {
      setLoading(false);
    }
  }, [applyPrompt]);

  useEffect(() => {
    void load();
  }, [load]);

  const setOpen = (nextOpen: boolean) => {
    if (saving) return;
    setDraft(prompt.content);
    setEnabled(prompt.enabled);
    setSaveError("");
    onOpenChange(nextOpen);
  };

  const save = async () => {
    if (!dirty || loading) return;
    setSaving(true);
    setSaveError("");
    try {
      const response = await fetch("/api/admin/agent-prompts/global", {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ content: draft, enabled }),
      });
      if (!response.ok) throw new Error(await readError(response));
      applyPrompt((await response.json()) as AgentPrompt);
      onOpenChange(false);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : String(error));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <ToolboxCard
        icon={ScrollText}
        iconClassName="bg-cyan-100 text-cyan-600 dark:bg-cyan-950 dark:text-cyan-300"
        status={promptStatusLabel(loading, Boolean(loadError), prompt.enabled, t)}
        summary={t("summary")}
        title={t("title")}
        onPress={() => setOpen(true)}
      />

      <AdminDrawer
        className="admin-theme-cyan"
        open={open}
        onOpenChange={setOpen}
        title={t("title")}
        description={t("summary")}
        size="lg"
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button variant="outline" isDisabled={saving} onPress={() => setOpen(false)}>
              {t("cancel")}
            </Button>
            <Button
              className="min-w-32 gap-2"
              isDisabled={saving || loading || !dirty}
              onPress={() => void save()}
            >
              {saving ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Save className="size-4" />
              )}
              {saving ? t("saving") : t("save")}
            </Button>
          </div>
        }
      >
        <div className="space-y-5">
          {loading ? (
            <div className="flex min-h-48 items-center justify-center text-sm text-muted">
              <LoaderCircle className="mr-2 size-4 animate-spin" />
              {t("loadingPrompt")}
            </div>
          ) : (
            <>
              {loadError ? (
                <AdminAlert tone="error" icon={<CircleAlert className="size-4" />}>
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span>{loadError}</span>
                    <Button variant="outline" size="sm" onPress={() => void load()}>
                      {t("retry")}
                    </Button>
                  </div>
                </AdminAlert>
              ) : null}
              {saveError ? (
                <AdminAlert tone="error" icon={<CircleAlert className="size-4" />}>
                  {saveError}
                </AdminAlert>
              ) : null}

              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <div className="text-sm font-semibold text-foreground">{t("globalPrompt")}</div>
                  <p className="mt-1 max-w-xl text-xs leading-5 text-muted">
                    {t("globalPromptDescription")}
                  </p>
                </div>
                <Switch
                  isDisabled={saving || Boolean(loadError)}
                  isSelected={enabled}
                  onChange={setEnabled}
                >
                  <Switch.Content>
                    <Switch.Control>
                      <Switch.Thumb />
                    </Switch.Control>
                    {enabled ? t("enabled") : t("disabled")}
                  </Switch.Content>
                </Switch>
              </div>

              <TextField
                isDisabled={saving || Boolean(loadError)}
                value={draft}
                variant="secondary"
                onChange={setDraft}
              >
                <Label className="sr-only">{t("globalPrompt")}</Label>
                <TextArea
                  className="min-h-80 text-sm leading-6"
                  placeholder={t("placeholder")}
                  spellCheck={false}
                />
              </TextField>

              <div className="grid gap-3 sm:grid-cols-2">
                <PromptMeta label={t("version")} value={prompt.version || 0} />
                <PromptMeta label={t("characters")} value={draft.length} />
              </div>
              <div className="text-xs text-muted">
                {dirty ? t("unsavedChanges") : t("upToDate")}
              </div>
            </>
          )}
        </div>
      </AdminDrawer>
    </>
  );
}

function promptStatusLabel(
  loading: boolean,
  error: boolean,
  enabled: boolean,
  t: ReturnType<typeof useTranslations<"admin.toolboxPage.systemPrompt">>,
) {
  if (loading) return t("loading");
  if (error) return t("unavailable");
  return enabled ? t("enabled") : t("disabled");
}

function PromptMeta({ label, value }: { label: string; value: string | number }) {
  return (
    <Card className="p-3">
      <div className="text-muted text-xs">{label}</div>
      <div className="mt-1 font-mono text-sm font-semibold tabular-nums text-foreground">
        {value}
      </div>
    </Card>
  );
}

async function readError(response: Response) {
  try {
    const data = await response.json();
    if (typeof data?.error === "string") return data.error;
    if (typeof data?.message === "string") return data.message;
  } catch {
    // fall through
  }
  return `${response.status} ${response.statusText}`;
}
