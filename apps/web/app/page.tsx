"use client";

// Product chat page.
//
// Composes the assistant-ui Thread on top of the cocola ExternalStore runtime
// adapter, inside an Open WebUI style shell: a static left sidebar (chat
// history / folders — decorative until multi-thread persistence lands) and a
// main column with a slim top bar (model pill + sandbox status + collapsible
// dev inputs) over the Thread. The runtime owns message state and the SSE
// plumbing (app/runtime-provider.tsx); this page only renders chrome. The raw
// event-log debug tool lives at /raw.
//
// The main column is a flex row so a future Artifacts canvas can sit beside the
// Thread without restructuring.

import { useState } from "react";
import { ChevronDown, Settings2 } from "lucide-react";
import { CocolaRuntimeProvider, useCocola } from "@/app/runtime-provider";
import { AppSidebar } from "@/components/assistant-ui/app-sidebar";
import { Thread } from "@/components/assistant-ui/thread";

export default function Home() {
  return (
    <CocolaRuntimeProvider>
      <div className="flex h-screen bg-background text-foreground">
        <AppSidebar />
        <div className="flex min-w-0 flex-1 flex-col">
          <TopBar />
          {/* flex row: Thread now, Artifacts canvas can slot in beside it later */}
          <div className="flex min-h-0 flex-1">
            <div className="min-w-0 flex-1">
              <Thread />
            </div>
          </div>
        </div>
      </div>
    </CocolaRuntimeProvider>
  );
}

// Slim top bar: model pill (static placeholder) + sandbox status + a
// collapsible dev panel for Bearer token / session id (kept so a developer can
// drive the real backend before a proper auth flow exists).
function TopBar() {
  const { token, setToken, sessionId, setSessionId, sandbox } = useCocola();
  const [showDev, setShowDev] = useState(false);

  return (
    <header className="flex flex-col border-b border-border">
      <div className="flex h-14 items-center gap-3 px-4">
        <button
          type="button"
          className="flex items-center gap-1.5 rounded-lg px-2 py-1 text-sm font-medium hover:bg-muted"
        >
          gpt-4.1-nano
          <ChevronDown className="size-4 text-muted-foreground" />
        </button>

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
          <button
            type="button"
            onClick={() => setShowDev((v) => !v)}
            aria-label="Developer settings"
            title="Developer settings"
            className="flex size-8 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <Settings2 className="size-4" />
          </button>
        </div>
      </div>

      {showDev && (
        <div className="flex flex-wrap items-center gap-2 border-t border-border px-4 py-2">
          <input
            className="w-56 rounded border border-input bg-background px-2 py-1 font-mono text-xs outline-none focus:border-ring"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Bearer token (blank if auth off)"
            aria-label="Bearer token"
          />
          <input
            className="w-24 rounded border border-input bg-background px-2 py-1 font-mono text-xs outline-none focus:border-ring"
            value={sessionId}
            onChange={(e) => setSessionId(e.target.value)}
            placeholder="session id"
            aria-label="Session ID"
          />
        </div>
      )}
    </header>
  );
}
