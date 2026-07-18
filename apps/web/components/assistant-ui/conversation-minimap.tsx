"use client";

// ConversationMinimap — a slim vertical rail pinned to the left edge of the
// chat column. Each tick maps to one user turn; hovering reveals the turn's
// timestamp + a truncated preview of the question, and clicking scrolls the
// matching message into view.
//
// Data comes entirely from the thread store (useThread) — no backend, no new
// state. The click target is the `data-message-id` anchor that UserMessage
// renders in thread.tsx; we resolve it lazily at click time so it keeps working
// regardless of how the list is mounted.

import * as Tooltip from "@radix-ui/react-tooltip";
import { useThread } from "@assistant-ui/react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { cn } from "@/lib/utils";

const PREVIEW_MAX = 200;

type MinimapEntry = {
  id: string;
  index: number; // 1-based user-turn ordinal
  time: number | null;
  text: string;
};

function extractText(content: readonly { type: string; text?: string }[]): string {
  return content
    .filter((part) => part.type === "text" && typeof part.text === "string")
    .map((part) => part.text as string)
    .join("")
    .trim();
}

function formatTime(time: number | null): string {
  if (!time) return "";
  const d = new Date(time);
  const now = new Date();
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  const hm = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  if (sameDay) return hm;
  const md = d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  return `${md} ${hm}`;
}

export function ConversationMinimap() {
  // Select the store's stable `messages` reference — do NOT build a derived
  // array inside the selector, or its fresh reference on every render triggers
  // an infinite update loop. Derive the minimap entries with useMemo instead.
  const messages = useThread((t) => t.messages);
  const entries = useMemo<MinimapEntry[]>(() => {
    let index = 0;
    const out: MinimapEntry[] = [];
    for (const m of messages) {
      if (m.role !== "user") continue;
      index += 1;
      const text = extractText(m.content as { type: string; text?: string }[]);
      if (!text) continue;
      out.push({
        id: m.id,
        index,
        time: m.createdAt ? new Date(m.createdAt).getTime() : null,
        text: text.length > PREVIEW_MAX ? `${text.slice(0, PREVIEW_MAX)}…` : text,
      });
    }
    return out;
  }, [messages]);

  const [activeId, setActiveId] = useState<string | null>(null);
  const observerRef = useRef<IntersectionObserver | null>(null);

  const scrollTo = useCallback((id: string) => {
    const el = document.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(id)}"]`);
    if (!el) return;
    el.scrollIntoView({ behavior: "smooth", block: "start" });
    setActiveId(id);
  }, []);

  // Highlight the tick whose message is currently near the top of the viewport.
  useEffect(() => {
    if (entries.length === 0) return;
    const io = new IntersectionObserver(
      (records) => {
        const visible = records
          .filter((r) => r.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        const first = visible[0]?.target as HTMLElement | undefined;
        const id = first?.getAttribute("data-message-id");
        if (id) setActiveId(id);
      },
      { rootMargin: "0px 0px -70% 0px", threshold: 0 },
    );
    observerRef.current = io;
    for (const entry of entries) {
      const el = document.querySelector<HTMLElement>(
        `[data-message-id="${CSS.escape(entry.id)}"]`,
      );
      if (el) io.observe(el);
    }
    return () => io.disconnect();
  }, [entries]);

  if (entries.length <= 1) return null;

  return (
    <Tooltip.Provider delayDuration={80} skipDelayDuration={200}>
      <nav
        aria-label="Conversation history"
        className="group/minimap pointer-events-auto absolute left-2 top-1/2 z-20 flex max-h-[70vh] -translate-y-1/2 flex-col gap-1 overflow-y-auto overscroll-contain px-1.5 py-2 opacity-40 transition-opacity duration-200 hover:opacity-100 focus-within:opacity-100 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {entries.map((entry) => {
          const active = entry.id === activeId;
          return (
            <Tooltip.Root key={entry.id}>
              <Tooltip.Trigger asChild>
                <button
                  type="button"
                  onClick={() => scrollTo(entry.id)}
                  aria-label={`Jump to turn ${entry.index}`}
                  className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full"
                >
                  <span
                    className={cn(
                      "h-1.5 rounded-full bg-muted-foreground/40 transition-all duration-150 group-hover/minimap:w-4",
                      active
                        ? "w-4 bg-primary"
                        : "w-2.5 hover:w-4 hover:bg-muted-foreground/70",
                    )}
                  />
                </button>
              </Tooltip.Trigger>
              <Tooltip.Portal>
                <Tooltip.Content
                  side="right"
                  sideOffset={10}
                  align="center"
                  collisionPadding={12}
                  className="z-50 max-w-xs rounded-md border border-neutral-200 bg-white px-3 py-2 text-xs text-neutral-900 shadow-lg"
                >
                  <div className="whitespace-pre-wrap break-words leading-5">
                    {entry.text}
                  </div>
                  {entry.time !== null && (
                    <div className="mt-1 font-medium text-neutral-400">
                      {formatTime(entry.time)}
                    </div>
                  )}
                  <Tooltip.Arrow className="fill-white" />
                </Tooltip.Content>
              </Tooltip.Portal>
            </Tooltip.Root>
          );
        })}
      </nav>
    </Tooltip.Provider>
  );
}
