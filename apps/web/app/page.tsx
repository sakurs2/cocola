"use client";

// Product chat page.
//
// Composes the assistant-ui Thread on top of the cocola ExternalStore runtime
// adapter, inside an Open WebUI style shell: a static left sidebar (chat
// history / folders) and a main column with a slim status bar over the Thread.
// The runtime owns message state and the SSE
// plumbing (app/runtime-provider.tsx); this page only renders chrome. The raw
// event-log debug tool lives at /raw.
//
// Identity is intentionally NOT configurable here: every request goes out
// anonymous and the gateway resolves it to the shared dev-user (auth is a later
// concern). There is no token input — changing a token in the page would amount
// to silently switching users, which is not a real auth flow.
//
// The main column is a flex row so a future Artifacts canvas can sit beside the
// Thread without restructuring.

import { useThread } from "@assistant-ui/react";
import { useCocola, type EnvironmentStatus } from "@/app/runtime-provider";
import { MarkdownContent } from "@/components/assistant-ui/markdown-text";
import {
  SessionStatusButton,
  SessionStatusPanel,
} from "@/components/assistant-ui/session-status-panel";
import { Thread } from "@/components/assistant-ui/thread";
import { AnimatePresence, motion } from "framer-motion";
import { Check, Code2, Download, Eye, FileQuestion, Share2, X } from "lucide-react";
import dynamic from "next/dynamic";
import { useRouter } from "next/navigation";
import { type PointerEvent, useCallback, useEffect, useState } from "react";

const MonacoEditor = dynamic(() => import("@monaco-editor/react"), { ssr: false });

export default function Home() {
  return <Workspace />;
}

function Workspace() {
  const { loadConversation, selectedArtifact, environmentStatus } = useCocola();
  const router = useRouter();
  const [previewWidth, setPreviewWidth] = useState(448);
  const [dockView, setDockView] = useState<"status" | "artifact">("status");
  const [statusOpen, setStatusOpen] = useState(false);

  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("conversation")?.trim();
    if (!id) return;
    void loadConversation(id);
    router.replace("/");
  }, [loadConversation, router]);

  useEffect(() => {
    if (selectedArtifact) setDockView("artifact");
  }, [selectedArtifact]);

  useEffect(() => {
    if (!environmentStatus || selectedArtifact) return;
    if (
      (environmentStatus.phase === "preparing" && environmentStatus.components.length === 0) ||
      environmentStatus.phase === "degraded"
    ) {
      setDockView("status");
      setStatusOpen(true);
    }
  }, [environmentStatus, selectedArtifact]);

  const startPreviewResize = useCallback(
    (event: PointerEvent<HTMLDivElement>) => {
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = previewWidth;
      const previousCursor = document.body.style.cursor;
      const previousUserSelect = document.body.style.userSelect;
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";

      const clampPreviewWidth = (value: number) => {
        const max = Math.max(352, Math.min(window.innerWidth * 0.55, 760));
        return Math.min(Math.max(value, 352), max);
      };
      const onPointerMove = (moveEvent: globalThis.PointerEvent) => {
        setPreviewWidth(clampPreviewWidth(startWidth - (moveEvent.clientX - startX)));
      };
      const onPointerUp = () => {
        document.body.style.cursor = previousCursor;
        document.body.style.userSelect = previousUserSelect;
        window.removeEventListener("pointermove", onPointerMove);
        window.removeEventListener("pointerup", onPointerUp);
        window.removeEventListener("pointercancel", onPointerUp);
      };

      window.addEventListener("pointermove", onPointerMove);
      window.addEventListener("pointerup", onPointerUp);
      window.addEventListener("pointercancel", onPointerUp);
    },
    [previewWidth],
  );

  return (
    <div className="relative flex h-full min-w-0 flex-1 flex-col">
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1">
          <TopBar
            environmentStatus={environmentStatus}
            onOpenStatus={() => {
              setDockView("status");
              setStatusOpen(true);
            }}
          />
          <Thread />
        </div>
        <AnimatePresence initial={false}>
          {selectedArtifact && dockView === "artifact" ? (
            <>
              <div
                role="separator"
                aria-label="Resize file preview"
                aria-orientation="vertical"
                title="Resize preview"
                onPointerDown={startPreviewResize}
                className="group relative z-10 w-3 shrink-0 cursor-col-resize touch-none"
              >
                <div className="absolute inset-y-4 left-1/2 w-px -translate-x-1/2 rounded-full bg-border transition-colors group-hover:bg-primary/70" />
                <div className="absolute inset-y-4 left-1/2 w-1 -translate-x-1/2 rounded-full bg-transparent transition-colors group-hover:bg-primary/20" />
              </div>
              <motion.aside
                key="artifact-preview"
                initial={{ opacity: 0, x: 28 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: 28 }}
                transition={{ duration: 0.18, ease: "easeOut" }}
                className="m-2 ml-0 shrink-0 overflow-hidden rounded-2xl border border-border bg-card shadow-xl"
                style={{ width: `${previewWidth}px` }}
              >
                <ArtifactPreviewPanel />
              </motion.aside>
            </>
          ) : environmentStatus && statusOpen && dockView === "status" ? (
            <motion.aside
              key="session-status"
              initial={{ opacity: 0, x: 24 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 24 }}
              transition={{ duration: 0.18, ease: "easeOut" }}
              className="fixed inset-x-2 bottom-2 top-14 z-30 overflow-hidden rounded-2xl border border-border bg-card/95 shadow-xl backdrop-blur-xl md:static md:inset-auto md:z-auto md:m-2 md:ml-0 md:w-80 md:shrink-0"
            >
              <SessionStatusPanel
                status={environmentStatus}
                artifactName={selectedArtifact?.filename}
                onOpenArtifact={() => setDockView("artifact")}
                onClose={() => {
                  setStatusOpen(false);
                  if (selectedArtifact) setDockView("artifact");
                }}
              />
            </motion.aside>
          ) : null}
        </AnimatePresence>
      </div>
    </div>
  );
}

// Slim status bar: model selection now lives inside the composer, matching the
// input-first chat layout. Keep sandbox state visible without competing with the
// conversation controls.
function TopBar({
  environmentStatus,
  onOpenStatus,
}: {
  environmentStatus: EnvironmentStatus | null;
  onOpenStatus: () => void;
}) {
  const { activeSessionId, conversations } = useCocola();
  const [copied, setCopied] = useState(false);
  // The empty/welcome state is chrome-free (matches the reference): the status
  // bar and its Share control only appear once a conversation is under way.
  const hasMessages = useThread((t) => t.messages.length > 0);
  const canShare = conversations.some((conversation) => conversation.id === activeSessionId);

  const copyShareLink = useCallback(async () => {
    if (!activeSessionId || typeof window === "undefined") return;
    const url = `${window.location.origin}/conversations/${encodeURIComponent(activeSessionId)}`;
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  }, [activeSessionId]);

  if (!hasMessages) return null;

  return (
    <div className="pointer-events-none absolute right-0 top-0 z-20">
      <div className="flex items-center gap-3 px-4 py-2">
        <div className="pointer-events-auto ml-auto flex items-center gap-2">
          {environmentStatus ? (
            <SessionStatusButton status={environmentStatus} onClick={onOpenStatus} />
          ) : null}
          <button
            type="button"
            title={
              canShare
                ? copied
                  ? "Link copied"
                  : "Copy share link"
                : "Start a conversation to share"
            }
            aria-label={canShare ? "Copy share link" : "Start a conversation to share"}
            disabled={!canShare}
            onClick={() => void copyShareLink()}
            className="inline-flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-40"
          >
            {copied ? <Check className="size-4 text-emerald-600" /> : <Share2 className="size-4" />}
          </button>
        </div>
      </div>
    </div>
  );
}

function ArtifactPreviewPanel() {
  const { selectedArtifact, closeArtifact } = useCocola();
  const [text, setText] = useState<string>("");
  const [error, setError] = useState<string>("");
  const [htmlSourceMode, setHtmlSourceMode] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setText("");
    setError("");
    setHtmlSourceMode(false);
    if (!selectedArtifact || !isTextPreview(selectedArtifact.mimeType, selectedArtifact.filename)) {
      return;
    }
    void (async () => {
      try {
        const res = await fetch(selectedArtifact.downloadUrl, { cache: "no-store" });
        if (!res.ok) throw new Error(`preview failed: ${res.status}`);
        const body = await res.text();
        if (!cancelled) setText(body);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [selectedArtifact]);

  if (!selectedArtifact) return null;

  const canText = isTextPreview(selectedArtifact.mimeType, selectedArtifact.filename);
  const canHtml = isHtmlPreview(selectedArtifact.mimeType, selectedArtifact.filename);
  const canImage = selectedArtifact.mimeType.startsWith("image/");
  const canPdf = selectedArtifact.mimeType === "application/pdf";
  const previewKind = getTextPreviewKind(selectedArtifact.mimeType, selectedArtifact.filename);
  const language = getCodeLanguage(selectedArtifact.mimeType, selectedArtifact.filename);

  return (
    <div className="flex h-full flex-col">
      <header className="flex min-h-14 items-center gap-3 border-b border-border bg-card px-4">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{selectedArtifact.filename}</div>
          <div className="truncate text-xs text-muted-foreground">
            {formatBytes(selectedArtifact.size)} · {selectedArtifact.mimeType}
          </div>
        </div>
        {canHtml ? (
          <button
            type="button"
            aria-label={htmlSourceMode ? "Preview HTML" : "View HTML source"}
            title={htmlSourceMode ? "Preview HTML" : "View source"}
            onClick={() => setHtmlSourceMode((value) => !value)}
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            {htmlSourceMode ? <Eye className="size-4" /> : <Code2 className="size-4" />}
          </button>
        ) : null}
        <a
          href={selectedArtifact.downloadUrl}
          download={selectedArtifact.filename}
          title="Download"
          aria-label="Download"
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <Download className="size-4" />
        </a>
        <button
          type="button"
          aria-label="Close preview"
          title="Close"
          onClick={closeArtifact}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <X className="size-4" />
        </button>
      </header>
      <div className="min-h-0 flex-1 overflow-auto bg-background">
        {canImage ? (
          <div className="flex min-h-full items-start justify-center p-4">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={selectedArtifact.downloadUrl}
              alt={selectedArtifact.filename}
              className="max-h-full max-w-full rounded-xl border border-border bg-card object-contain shadow-sm"
            />
          </div>
        ) : canPdf ? (
          <iframe
            title={selectedArtifact.filename}
            src={selectedArtifact.downloadUrl}
            className="h-full w-full"
          />
        ) : canHtml && !htmlSourceMode && !error && text ? (
          <iframe
            title={selectedArtifact.filename}
            srcDoc={text}
            sandbox="allow-forms allow-modals allow-popups allow-scripts"
            className="h-full w-full bg-white"
          />
        ) : canText ? (
          <TextArtifactPreview error={error} text={text} kind={previewKind} language={language} />
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">
            <FileQuestion className="size-8" />
            <p className="text-sm font-medium text-foreground">Preview not supported</p>
            <p className="text-xs">Download the file to open it locally.</p>
          </div>
        )}
      </div>
    </div>
  );
}

function TextArtifactPreview({
  error,
  text,
  kind,
  language,
}: {
  error: string;
  text: string;
  kind: "markdown" | "code" | "plain";
  language: string;
}) {
  if (error) {
    return (
      <pre className="min-h-full whitespace-pre-wrap break-words p-4 font-mono text-xs leading-5 text-destructive">
        {error}
      </pre>
    );
  }
  if (!text) {
    return <div className="p-4 text-sm text-muted-foreground">Loading preview...</div>;
  }
  if (kind === "markdown") {
    return <MarkdownContent value={text} className="p-4" />;
  }
  if (kind === "code") {
    return (
      <div className="h-full min-h-[420px]">
        <MonacoEditor
          language={toMonacoLanguage(language)}
          value={text}
          theme="vs"
          options={{
            readOnly: true,
            minimap: { enabled: false },
            fontSize: 12,
            lineHeight: 20,
            scrollBeyondLastLine: false,
            wordWrap: "on",
            renderLineHighlight: "none",
            overviewRulerBorder: false,
          }}
        />
      </div>
    );
  }
  return (
    <div className="h-full min-h-[420px]">
      <MonacoEditor
        language="plaintext"
        value={text}
        theme="vs"
        options={{
          readOnly: true,
          minimap: { enabled: false },
          fontSize: 12,
          lineHeight: 20,
          scrollBeyondLastLine: false,
          wordWrap: "on",
          renderLineHighlight: "none",
          overviewRulerBorder: false,
        }}
      />
    </div>
  );
}

function toMonacoLanguage(language: string): string {
  if (language === "shell") return "shell";
  if (language === "text") return "plaintext";
  return language;
}

function isHtmlPreview(mime: string, filename: string): boolean {
  const ext = getKnownTextExtension(filename);
  return mime === "text/html" || ext === "html" || ext === "htm";
}

function isTextPreview(mime: string, filename: string): boolean {
  if (mime.startsWith("text/")) return true;
  if (["application/json", "application/xml", "application/javascript"].includes(mime)) return true;
  return getKnownTextExtension(filename) !== "";
}

function getTextPreviewKind(mime: string, filename: string): "markdown" | "code" | "plain" {
  const ext = getKnownTextExtension(filename);
  if (mime === "text/markdown" || ext === "md" || ext === "markdown") return "markdown";
  if (getCodeLanguage(mime, filename) !== "text") return "code";
  return "plain";
}

function getCodeLanguage(mime: string, filename: string): string {
  if (mime === "application/json") return "json";
  if (mime === "application/xml") return "xml";
  if (mime === "application/javascript") return "javascript";

  const ext = getKnownTextExtension(filename);
  const languages: Record<string, string> = {
    bash: "shell",
    c: "c",
    cpp: "cpp",
    css: "css",
    csv: "text",
    diff: "diff",
    go: "go",
    h: "c",
    htm: "html",
    html: "html",
    java: "java",
    js: "javascript",
    jsx: "javascript",
    json: "json",
    kt: "kotlin",
    md: "markdown",
    patch: "diff",
    py: "python",
    rs: "rust",
    sh: "shell",
    toml: "toml",
    ts: "typescript",
    tsx: "typescript",
    xml: "xml",
    yaml: "yaml",
    yml: "yaml",
    zsh: "shell",
  };
  return languages[ext] ?? "text";
}

function getKnownTextExtension(filename: string): string {
  const match = /\.([a-z0-9]+)$/i.exec(filename);
  const ext = match?.[1]?.toLowerCase() ?? "";
  const known = new Set([
    "bash",
    "c",
    "cpp",
    "css",
    "csv",
    "diff",
    "go",
    "h",
    "htm",
    "html",
    "java",
    "js",
    "jsx",
    "json",
    "kt",
    "markdown",
    "md",
    "patch",
    "py",
    "rs",
    "sh",
    "toml",
    "ts",
    "tsx",
    "txt",
    "xml",
    "yaml",
    "yml",
    "zsh",
  ]);
  return known.has(ext) ? ext : "";
}

function formatBytes(bytes: number): string {
  if (!bytes) return "Unknown size";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}
