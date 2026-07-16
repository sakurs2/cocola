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
import {
  ReadonlyFilePreview,
  formatBytes,
  isHtmlPreview,
  type PreviewFile,
} from "@/components/assistant-ui/file-preview";
import {
  SessionStatusButton,
  SessionStatusPanel,
} from "@/components/assistant-ui/session-status-panel";
import { Thread } from "@/components/assistant-ui/thread";
import { WorkspacePanel } from "@/components/assistant-ui/workspace-panel";
import { cn } from "@/lib/utils";
import { AnimatePresence, motion } from "framer-motion";
import { Check, Code2, Download, Eye, FolderOpen, Share2, X } from "lucide-react";
import { useRouter } from "next/navigation";
import {
  type Dispatch,
  type PointerEvent,
  type SetStateAction,
  useCallback,
  useEffect,
  useState,
} from "react";

export default function Home() {
  return <Workspace />;
}

function Workspace() {
  const { loadConversation, selectedArtifact, environmentStatus, activeSessionId } = useCocola();
  const router = useRouter();
  const [previewWidth, setPreviewWidth] = useState(448);
  const [workspaceWidth, setWorkspaceWidth] = useState(640);
  const [dockView, setDockView] = useState<"status" | "artifact" | "workspace">("status");
  const [statusOpen, setStatusOpen] = useState(false);
  const [workspaceOpen, setWorkspaceOpen] = useState(false);

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
    if (environmentStatus.phase === "preparing" || environmentStatus.phase === "degraded") {
      setDockView("status");
      setStatusOpen(true);
    }
  }, [environmentStatus, selectedArtifact]);

  const startPreviewResize = useCallback(
    (event: PointerEvent<HTMLDivElement>) => {
      beginDockResize(event, previewWidth, 352, setPreviewWidth);
    },
    [previewWidth],
  );
  const startWorkspaceResize = useCallback(
    (event: PointerEvent<HTMLDivElement>) => {
      beginDockResize(event, workspaceWidth, 480, setWorkspaceWidth);
    },
    [workspaceWidth],
  );

  return (
    <div className="relative flex h-full min-w-0 flex-1 flex-col">
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1">
          <TopBar
            environmentStatus={environmentStatus}
            workspaceOpen={workspaceOpen && dockView === "workspace"}
            onOpenStatus={() => {
              setDockView("status");
              setStatusOpen(true);
            }}
            onOpenWorkspace={() => {
              if (workspaceOpen && dockView === "workspace") {
                setWorkspaceOpen(false);
                return;
              }
              setDockView("workspace");
              setWorkspaceOpen(true);
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
          ) : activeSessionId && workspaceOpen && dockView === "workspace" ? (
            <>
              <div
                role="separator"
                aria-label="Resize workspace browser"
                aria-orientation="vertical"
                title="Resize workspace browser"
                onPointerDown={startWorkspaceResize}
                className="group relative z-10 hidden w-3 shrink-0 cursor-col-resize touch-none md:block"
              >
                <div className="absolute inset-y-4 left-1/2 w-px -translate-x-1/2 rounded-full bg-border transition-colors group-hover:bg-primary/70" />
                <div className="absolute inset-y-4 left-1/2 w-1 -translate-x-1/2 rounded-full bg-transparent transition-colors group-hover:bg-primary/20" />
              </div>
              <motion.aside
                key={`workspace-${activeSessionId}`}
                initial={{ opacity: 0, x: 28 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: 28 }}
                transition={{ duration: 0.18, ease: "easeOut" }}
                className="fixed inset-x-2 bottom-2 top-14 z-30 w-auto overflow-hidden rounded-2xl border border-border bg-card/95 shadow-xl backdrop-blur-xl md:static md:inset-auto md:z-auto md:m-2 md:ml-0 md:w-[var(--workspace-width)] md:shrink-0"
                style={{ ["--workspace-width" as string]: `${workspaceWidth}px` }}
              >
                <WorkspacePanel
                  sessionID={activeSessionId}
                  onClose={() => setWorkspaceOpen(false)}
                />
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

function beginDockResize(
  event: PointerEvent<HTMLDivElement>,
  currentWidth: number,
  minWidth: number,
  setWidth: Dispatch<SetStateAction<number>>,
) {
  event.preventDefault();
  const startX = event.clientX;
  const previousCursor = document.body.style.cursor;
  const previousUserSelect = document.body.style.userSelect;
  document.body.style.cursor = "col-resize";
  document.body.style.userSelect = "none";
  const maxWidth = Math.max(minWidth, Math.min(window.innerWidth * 0.62, 760));
  const onPointerMove = (moveEvent: globalThis.PointerEvent) => {
    setWidth(Math.min(Math.max(currentWidth - (moveEvent.clientX - startX), minWidth), maxWidth));
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
}

// Slim status bar: model selection now lives inside the composer, matching the
// input-first chat layout. Keep sandbox state visible without competing with the
// conversation controls.
function TopBar({
  environmentStatus,
  onOpenStatus,
  onOpenWorkspace,
  workspaceOpen,
}: {
  environmentStatus: EnvironmentStatus | null;
  onOpenStatus: () => void;
  onOpenWorkspace: () => void;
  workspaceOpen: boolean;
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
            title={canShare ? "Open workspace" : "Start a conversation to browse its workspace"}
            aria-label={
              canShare ? "Open workspace" : "Start a conversation to browse its workspace"
            }
            aria-pressed={workspaceOpen}
            disabled={!canShare}
            onClick={onOpenWorkspace}
            className={cn(
              "inline-flex size-8 items-center justify-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-40",
              workspaceOpen
                ? "bg-primary/10 text-primary"
                : "text-muted-foreground hover:bg-muted hover:text-foreground",
            )}
          >
            <FolderOpen className="size-4" />
          </button>
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
  const [htmlSourceMode, setHtmlSourceMode] = useState(false);

  useEffect(() => {
    setHtmlSourceMode(false);
  }, [selectedArtifact]);

  if (!selectedArtifact) return null;

  const canHtml = isHtmlPreview(selectedArtifact.mimeType, selectedArtifact.filename);
  const previewFile: PreviewFile = {
    filename: selectedArtifact.filename,
    size: selectedArtifact.size,
    mimeType: selectedArtifact.mimeType,
    url: selectedArtifact.downloadUrl,
  };

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
        <ReadonlyFilePreview
          file={previewFile}
          renderHtml={canHtml && !htmlSourceMode}
          unsupportedMessage="Download the file to open it locally."
        />
      </div>
    </div>
  );
}
