"use client";

// Product chat page.
//
// Composes the assistant-ui Thread on top of the cocola ExternalStore runtime
// adapter. The runtime owns message state and the SSE plumbing
// (app/runtime-provider.tsx); this page only renders the chrome: a session
// shell (Bearer token + session_id, kept as local state until auth lands) and
// the sandbox-binding status banner. The raw event-log debug tool lives at
// /raw.

import { CocolaRuntimeProvider, useCocola } from "@/app/runtime-provider";
import { Thread } from "@/components/assistant-ui/thread";

export default function Home() {
  return (
    <CocolaRuntimeProvider>
      <div className="flex h-screen flex-col">
        <SessionBar />
        <div className="min-h-0 flex-1">
          <Thread />
        </div>
      </div>
    </CocolaRuntimeProvider>
  );
}

// Top chrome: branding + token/session inputs + sandbox banner. The inputs are
// deliberately compact; they exist so a developer can drive the real backend
// before a proper auth flow exists.
function SessionBar() {
  const { token, setToken, sessionId, setSessionId, sandbox } = useCocola();
  return (
    <header className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-neutral-200 bg-white px-4 py-2">
      <div className="flex items-center gap-2">
        <span className="text-sm font-semibold text-neutral-900">cocola</span>
        {sandbox ? (
          <span
            className="rounded-full bg-emerald-50 px-2 py-0.5 font-mono text-[11px] text-emerald-700"
            title={sandbox.endpoint}
          >
            sandbox {sandbox.sandboxId.slice(0, 8) || "—"} {sandbox.reused ? "(reused)" : "(new)"}
          </span>
        ) : (
          <span className="rounded-full bg-neutral-100 px-2 py-0.5 font-mono text-[11px] text-neutral-500">
            no sandbox yet
          </span>
        )}
      </div>

      <div className="ml-auto flex items-center gap-2">
        <input
          className="w-56 rounded border border-neutral-300 px-2 py-1 font-mono text-xs"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="Bearer token (blank if auth off)"
          aria-label="Bearer token"
        />
        <input
          className="w-24 rounded border border-neutral-300 px-2 py-1 font-mono text-xs"
          value={sessionId}
          onChange={(e) => setSessionId(e.target.value)}
          placeholder="session id"
          aria-label="Session ID"
        />
      </div>
    </header>
  );
}
