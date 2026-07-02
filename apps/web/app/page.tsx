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
