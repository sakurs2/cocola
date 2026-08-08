"use client";

// The approved HeroUI Demo owns the chat layout; Cocola's runtime supplies the
// real conversation state, streaming events, session panel, and workspace dock.

import { useThread } from "@assistant-ui/react";
import { Button, Tooltip } from "@heroui/react";
import { useCocola, type EnvironmentStatus } from "@/app/runtime-provider";
import {
  SessionStatusButton,
  SessionStatusPanel,
} from "@/components/assistant-ui/session-status-panel";
import { Thread } from "@/components/assistant-ui/thread";
import { ConversationMinimap } from "@/components/assistant-ui/conversation-minimap";
import { WorkspaceDock } from "@/components/assistant-ui/workspace-panel";
import { WorkspaceThemeToggle } from "@/components/assistant-ui/workspace-theme-toggle";
import { useWorkspaceToast } from "@/components/assistant-ui/workspace-toast";
import { cn } from "@/lib/utils";
import { AnimatePresence, motion } from "framer-motion";
import { PanelRight } from "lucide-react";
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
  const {
    loadConversation,
    selectedArtifact,
    closeArtifact,
    environmentStatus,
    activeSessionId,
    conversations,
    agents,
    agentsLoaded,
    setSelectedAgentID,
  } = useCocola();
  const { showError } = useWorkspaceToast();
  const router = useRouter();
  const hasMessages = useThread((thread) => thread.messages.length > 0);
  const [workspaceWidth, setWorkspaceWidth] = useState(480);
  const [dockView, setDockView] = useState<"status" | "workspace">("status");
  const [statusOpen, setStatusOpen] = useState(false);
  const [workspaceOpen, setWorkspaceOpen] = useState(false);
  const activeConversation = conversations.find((item) => item.id === activeSessionId);

  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("conversation")?.trim();
    if (!id) return;
    void loadConversation(id);
    router.replace("/");
  }, [loadConversation, router]);

  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("agent")?.trim();
    if (!id || !agentsLoaded) return;
    if (agents.some((agent) => agent.id === id)) {
      setSelectedAgentID(id);
    } else {
      setSelectedAgentID(null);
      showError("This Agent is unavailable. Standard chat is ready instead.");
    }
    router.replace("/");
  }, [agents, agentsLoaded, router, setSelectedAgentID, showError]);

  useEffect(() => {
    if (!selectedArtifact) return;
    setDockView("workspace");
    setWorkspaceOpen(true);
  }, [selectedArtifact]);

  useEffect(() => {
    if (!hasMessages || !activeSessionId || !environmentStatus || selectedArtifact) return;
    if (environmentStatus.phase === "preparing" || environmentStatus.phase === "degraded") {
      setDockView("status");
      setStatusOpen(true);
    }
  }, [activeSessionId, environmentStatus, hasMessages, selectedArtifact]);

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
          <ConversationMinimap />
        </div>
        <AnimatePresence initial={false}>
          {activeSessionId && workspaceOpen && dockView === "workspace" ? (
            <>
              <div
                role="separator"
                aria-label="Resize side panel"
                aria-orientation="vertical"
                title="Resize side panel"
                onPointerDown={startWorkspaceResize}
                className="group relative z-10 hidden w-3 shrink-0 cursor-col-resize touch-none md:block"
              >
                <div className="absolute inset-y-0 right-0 w-px bg-border transition-colors group-hover:bg-accent/70" />
                <div className="absolute inset-y-0 right-0 w-1 bg-transparent transition-colors group-hover:bg-accent/20" />
              </div>
              <motion.aside
                key={`workspace-${activeSessionId}`}
                initial={{ opacity: 0, x: 28 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: 28 }}
                transition={{ duration: 0.18, ease: "easeOut" }}
                className="fixed inset-x-2 bottom-2 top-14 z-30 w-auto overflow-hidden bg-surface md:static md:inset-auto md:z-auto md:w-[var(--workspace-width)] md:shrink-0"
                style={{ ["--workspace-width" as string]: `${workspaceWidth}px` }}
              >
                <WorkspaceDock
                  sessionID={activeSessionId}
                  projectTask={Boolean(activeConversation?.project_id)}
                  artifact={selectedArtifact}
                  onArtifactClose={closeArtifact}
                  onClose={() => setWorkspaceOpen(false)}
                />
              </motion.aside>
            </>
          ) : hasMessages &&
            activeSessionId &&
            environmentStatus &&
            statusOpen &&
            dockView === "status" ? (
            <motion.aside
              key="session-status"
              initial={{ opacity: 0, x: 24 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 24 }}
              transition={{ duration: 0.18, ease: "easeOut" }}
              className="fixed inset-x-2 top-14 z-30 max-h-[calc(100svh-4rem)] overflow-hidden rounded-2xl border border-border bg-surface/95 shadow-xl backdrop-blur-xl md:static md:inset-auto md:z-auto md:m-2 md:max-h-[min(36rem,calc(100svh-5rem))] md:w-80 md:shrink-0 md:self-start"
            >
              <SessionStatusPanel
                status={environmentStatus}
                artifactName={selectedArtifact?.filename}
                onOpenArtifact={() => {
                  setDockView("workspace");
                  setWorkspaceOpen(true);
                }}
                onClose={() => {
                  setStatusOpen(false);
                  if (selectedArtifact) {
                    setDockView("workspace");
                    setWorkspaceOpen(true);
                  }
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
  // The empty/welcome state is chrome-free (matches the reference): the status
  // bar and its Share control only appear once a conversation is under way.
  const hasMessages = useThread((t) => t.messages.length > 0);
  const canShare = conversations.some((conversation) => conversation.id === activeSessionId);

  return (
    <div className="pointer-events-none absolute right-0 top-0 z-20">
      <div className="flex items-center gap-3 px-4 py-2">
        <div className="pointer-events-auto ml-auto flex items-center gap-2">
          {hasMessages && environmentStatus ? (
            <SessionStatusButton status={environmentStatus} onClick={onOpenStatus} />
          ) : null}
          {hasMessages ? (
            <Tooltip>
              <Tooltip.Trigger>
                <Button
                  isIconOnly
                  aria-label={
                    canShare ? "Open workspace" : "Start a conversation to browse its workspace"
                  }
                  aria-pressed={workspaceOpen}
                  isDisabled={!canShare}
                  onPress={onOpenWorkspace}
                  variant="ghost"
                  className={cn(
                    "size-8 min-w-8 rounded-full",
                    workspaceOpen
                      ? "bg-accent/10 text-accent"
                      : "text-muted hover:bg-surface-secondary hover:text-foreground",
                  )}
                >
                  <PanelRight className="size-4" />
                </Button>
              </Tooltip.Trigger>
              <Tooltip.Content>
                {canShare ? "Open workspace" : "Start a conversation to browse its workspace"}
              </Tooltip.Content>
            </Tooltip>
          ) : null}
          <WorkspaceThemeToggle />
        </div>
      </div>
    </div>
  );
}
