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

import { CocolaRuntimeProvider, useCocola } from "@/app/runtime-provider";
import { AppSidebar } from "@/components/assistant-ui/app-sidebar";
import { Thread } from "@/components/assistant-ui/thread";
import { Download, FileQuestion, X } from "lucide-react";
import { useEffect, useState } from "react";

export default function Home() {
  return (
    <CocolaRuntimeProvider>
      <Workspace />
    </CocolaRuntimeProvider>
  );
}

function Workspace() {
  const { selectedArtifact } = useCocola();

  return (
    <div className="flex h-screen bg-background text-foreground">
      <AppSidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar />
        <div className="flex min-h-0 flex-1">
          <div className="min-w-0 flex-1">
            <Thread />
          </div>
          {selectedArtifact ? (
            <aside className="w-[28rem] min-w-[22rem] max-w-[45vw] border-l border-border bg-background">
              <ArtifactPreviewPanel />
            </aside>
          ) : null}
        </div>
      </div>
    </div>
  );
}

// Slim status bar: model selection now lives inside the composer, matching the
// input-first chat layout. Keep sandbox state visible without competing with the
// conversation controls.
function TopBar() {
  const { sandbox } = useCocola();

  return (
    <header className="flex flex-col border-b border-border">
      <div className="flex h-14 items-center gap-3 px-4">
        <div className="ml-auto flex items-center gap-2">
          {sandbox ? (
            <span
              className="rounded-full bg-emerald-500/15 px-2 py-0.5 font-mono text-[11px] text-emerald-400"
              title={sandbox.endpoint}
            >
              sandbox {sandbox.sandboxId.slice(0, 8) || "—"} {sandbox.reused ? "(reused)" : "(new)"}
            </span>
          ) : (
            <span className="rounded-full bg-muted px-2 py-0.5 font-mono text-[11px] text-muted-foreground">
              no sandbox yet
            </span>
          )}
        </div>
      </div>
    </header>
  );
}

function ArtifactPreviewPanel() {
  const { selectedArtifact, closeArtifact } = useCocola();
  const [text, setText] = useState<string>("");
  const [error, setError] = useState<string>("");

  useEffect(() => {
    let cancelled = false;
    setText("");
    setError("");
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
  const canImage = selectedArtifact.mimeType.startsWith("image/");
  const canPdf = selectedArtifact.mimeType === "application/pdf";

  return (
    <div className="flex h-full flex-col">
      <header className="flex min-h-14 items-center gap-3 border-b border-border px-4">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{selectedArtifact.filename}</div>
          <div className="truncate text-xs text-muted-foreground">
            {formatBytes(selectedArtifact.size)} · {selectedArtifact.mimeType}
          </div>
        </div>
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
      <div className="min-h-0 flex-1 overflow-auto">
        {canImage ? (
          <div className="flex min-h-full items-start justify-center p-4">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={selectedArtifact.downloadUrl}
              alt={selectedArtifact.filename}
              className="max-h-full max-w-full rounded-lg border border-border object-contain"
            />
          </div>
        ) : canPdf ? (
          <iframe
            title={selectedArtifact.filename}
            src={selectedArtifact.downloadUrl}
            className="h-full w-full"
          />
        ) : canText ? (
          <pre className="min-h-full whitespace-pre-wrap break-words p-4 font-mono text-xs leading-5 text-foreground">
            {error || text || "Loading preview..."}
          </pre>
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

function isTextPreview(mime: string, filename: string): boolean {
  if (mime.startsWith("text/")) return true;
  if (["application/json", "application/xml", "application/javascript"].includes(mime)) return true;
  return /\.(md|markdown|json|csv|ts|tsx|js|jsx|py|go|rs|java|kt|css|html|xml|yaml|yml|toml|txt)$/i.test(
    filename,
  );
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
