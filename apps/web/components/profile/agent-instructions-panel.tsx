"use client";

import { Button, Card, Chip, Label, TextArea, TextField } from "@heroui/react";
import { AlertCircle, BookOpen, CheckCircle2, RotateCcw } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useState } from "react";
import { useTranslations } from "next-intl";

const MAX_CONTENT_BYTES = 32 * 1024;
type AgentInstructionsRecord = { content: string; version: number };
type Notice = { tone: "success" | "error"; message: string } | null;

export function AgentInstructionsPanel() {
  const t = useTranslations("profile.instructions");
  const [content, setContent] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const contentBytes = useMemo(() => new TextEncoder().encode(content).byteLength, [content]);
  const tooLarge = contentBytes > MAX_CONTENT_BYTES;
  const changed = content !== savedContent;

  useEffect(() => {
    const controller = new AbortController();
    void fetch("/api/account/agent-instructions", { cache: "no-store", signal: controller.signal })
      .then(async (response) => {
        const body = await response.json();
        if (!response.ok) throw new Error(instructionsError(body, t("loadFailed")));
        const record = body as AgentInstructionsRecord;
        setContent(record.content || "");
        setSavedContent(record.content || "");
      })
      .catch((error) => {
        if (!controller.signal.aborted)
          setNotice({
            tone: "error",
            message: error instanceof Error ? error.message : t("loadFailed"),
          });
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [t]);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!changed || tooLarge || saving) return;
    setNotice(null);
    setSaving(true);
    try {
      const response = await fetch("/api/account/agent-instructions", {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ content }),
      });
      const body = await response.json();
      if (!response.ok) throw new Error(instructionsError(body, t("saveFailed")));
      const record = body as AgentInstructionsRecord;
      setContent(record.content || "");
      setSavedContent(record.content || "");
      setNotice({ tone: "success", message: t("saved") });
    } catch (error) {
      setNotice({
        tone: "error",
        message: error instanceof Error ? error.message : t("saveFailed"),
      });
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card className="p-5">
      <form onSubmit={save}>
        <Card.Header className="flex-row items-start justify-between gap-4 p-0">
          <span>
            <Card.Title>{t("title")}</Card.Title>
            <Card.Description>{t("description")}</Card.Description>
          </span>
          <Chip color={tooLarge ? "danger" : "accent"} size="sm" variant="soft">
            {formatBytes(contentBytes)} / 32 KB
          </Chip>
        </Card.Header>
        <Card.Content className="mt-5 grid gap-4 p-0">
          <TextField
            isDisabled={loading || saving}
            value={content}
            variant="secondary"
            onChange={(value) => {
              setContent(value);
              setNotice(null);
            }}
          >
            <Label className="sr-only">AGENTS.md</Label>
            <TextArea
              className="min-h-56 font-mono text-sm"
              placeholder={loading ? t("loading") : t("placeholder")}
              spellCheck={false}
            />
          </TextField>
          <p className="text-muted text-xs leading-5">{t("priority")}</p>
          {tooLarge ? (
            <div className="text-danger flex items-start gap-2 text-sm">
              <AlertCircle className="mt-0.5 size-4" />
              {t("tooLarge")}
            </div>
          ) : null}
          {notice ? <NoticeLine notice={notice} /> : null}
        </Card.Content>
        <Card.Footer className="mt-4 justify-end gap-2 p-0">
          <Button
            isDisabled={loading || saving || !content}
            variant="outline"
            onPress={() => {
              setContent("");
              setNotice(null);
            }}
          >
            <RotateCcw className="size-4" />
            {t("clear")}
          </Button>
          <Button
            isDisabled={loading || saving || !changed || tooLarge}
            isPending={saving}
            type="submit"
          >
            <BookOpen className="size-4" />
            {t("save")}
          </Button>
        </Card.Footer>
      </form>
    </Card>
  );
}

function NoticeLine({ notice }: { notice: Exclude<Notice, null> }) {
  const Icon = notice.tone === "success" ? CheckCircle2 : AlertCircle;
  return (
    <div
      role={notice.tone === "error" ? "alert" : "status"}
      className={`${notice.tone === "success" ? "text-success" : "text-danger"} flex items-start gap-2 text-sm`}
    >
      <Icon className="mt-0.5 size-4" />
      <span>{notice.message}</span>
    </div>
  );
}
function instructionsError(body: unknown, fallback: string) {
  return (body as { error?: { message?: string } })?.error?.message || fallback;
}
function formatBytes(value: number) {
  return value < 1024 ? `${value} B` : `${(value / 1024).toFixed(value < 10 * 1024 ? 1 : 0)} KB`;
}
