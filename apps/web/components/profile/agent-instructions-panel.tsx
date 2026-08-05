"use client";

import { Button, Card, Chip, Label, TextArea, TextField } from "@heroui/react";
import { AlertCircle, BookOpen, CheckCircle2, RotateCcw } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useState } from "react";

const MAX_CONTENT_BYTES = 32 * 1024;
type AgentInstructionsRecord = { content: string; version: number };
type Notice = { tone: "success" | "error"; message: string } | null;

export function AgentInstructionsPanel() {
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
        if (!response.ok) throw new Error(instructionsError(body, "Could not load AGENTS.md."));
        const record = body as AgentInstructionsRecord;
        setContent(record.content || "");
        setSavedContent(record.content || "");
      })
      .catch((error) => {
        if (!controller.signal.aborted)
          setNotice({
            tone: "error",
            message: error instanceof Error ? error.message : "Could not load AGENTS.md.",
          });
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);

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
      if (!response.ok) throw new Error(instructionsError(body, "Could not save AGENTS.md."));
      const record = body as AgentInstructionsRecord;
      setContent(record.content || "");
      setSavedContent(record.content || "");
      setNotice({ tone: "success", message: "AGENTS.md saved." });
    } catch (error) {
      setNotice({
        tone: "error",
        message: error instanceof Error ? error.message : "Could not save AGENTS.md.",
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
            <Card.Title>Agent instructions</Card.Title>
            <Card.Description>AGENTS.md preferences applied to your sessions.</Card.Description>
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
              placeholder={
                loading
                  ? "Loading AGENTS.md…"
                  : "# Working preferences\n- Answer in Chinese.\n- Keep code changes small and reviewable."
              }
              spellCheck={false}
            />
          </TextField>
          <p className="text-muted text-xs leading-5">
            Your current request and repository-specific instructions can override these
            preferences. Administrator and safety policies always take priority.
          </p>
          {tooLarge ? (
            <div className="text-danger flex items-start gap-2 text-sm">
              <AlertCircle className="mt-0.5 size-4" />
              AGENTS.md must be 32 KB or smaller.
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
            Clear editor
          </Button>
          <Button
            isDisabled={loading || saving || !changed || tooLarge}
            isPending={saving}
            type="submit"
          >
            <BookOpen className="size-4" />
            Save
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
