"use client";

import { BrainCircuit, Construction } from "lucide-react";

export function MemoryPanel() {
  return (
    <section className="rounded-2xl border border-border bg-card shadow-card">
      <div className="flex items-center gap-3 border-b border-border px-4 py-3">
        <div className="grid size-8 place-items-center rounded-xl bg-emerald-500/10">
          <BrainCircuit className="size-4 text-emerald-600" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">Memory</h2>
          <p className="text-xs text-muted-foreground">Long-term memory for Cocola agents.</p>
        </div>
      </div>

      <div className="p-4">
        <div className="flex items-start gap-3 rounded-xl border border-amber-500/25 bg-amber-500/[0.06] px-4 py-3">
          <Construction className="mt-0.5 size-4 shrink-0 text-amber-600" />
          <div>
            <div className="text-sm font-medium">Memory is under development</div>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              This capability is not available in the current release.
            </p>
          </div>
        </div>
      </div>
    </section>
  );
}
