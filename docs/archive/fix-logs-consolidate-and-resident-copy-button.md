# Fix: consolidate stray dev logs + always-visible chat copy button

## Context
Two local-dev UX papercuts reported during hybrid-stack debugging:

1. Ad-hoc debug logs accumulated at the repo **root** (`.acceptchk.log`,
   `.chat-dbg.log`, `.chat-e2e{,2,3}.log`, `.chat-env.log`, `.execd-raw.log`,
   `.rebuild-ar.log`, `.stack-up.log`, `.warm.log`). They were caught by the
   `*.log` ignore rule so they never risked being committed, but they cluttered
   the working tree and were scattered rather than living in one place.
2. The per-turn **Copy** button under each assistant message only appeared on
   hover, so users could not tell it was there.

## Change
1. **Logs** — moved all stray root `*.log` debug artifacts into the existing
   gitignored unified folder `.run-logs/` (already the home of run-stack.sh's
   per-service logs; `.gitignore` line 71). Repo root is now free of stray logs.
   No script writes to the root — these were manual debug captures — so no code
   change was needed, only relocation. `.run-logs/` stays fully ignored.
2. **Copy button** — `apps/web/components/assistant-ui/thread.tsx`
   `AssistantActionBar`: changed `<ActionBarPrimitive.Root autohide="not-last">`
   to `autohide="never"` so the action bar (Copy) is always resident under every
   assistant turn instead of hover-gated. `hideWhenRunning` is retained so it
   does not flash mid-stream and settles in once the turn completes.

## Validation
- `pnpm exec tsc --noEmit` clean; eslint on the file clean.
- `git status` shows only the tracked `thread.tsx` edit; the log moves produce no
  tracked delta (both source and destination are ignored).

## Rollback
Revert the `autohide` value to `"not-last"`; the relocated logs can stay in
`.run-logs/` (or be deleted — they are disposable debug output).
