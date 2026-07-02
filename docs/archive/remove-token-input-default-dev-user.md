# Remove the Bearer-token input — default anonymous → dev-user

Date: 2026-07-02

## Why

The top-bar debug panel exposed a Bearer-token `<input>`. Typing a token there
silently switched the effective user (the gateway scopes conversations by the
verified identity). That is not a real auth flow — "change the token in the page
= become a different user" is unreasonable, and auth is a later concern. For now
every request should go out anonymous and let the gateway resolve it to the
shared `dev-user` (`COCOLA_AUTH_ALLOW_ANON=1`), giving a stable user_id across
refreshes and tabs during development.

This also removes the localStorage token-persistence side effect introduced by
an earlier fix, which had made a previously-minted `emp-42` token sticky and
caused the "no history after refresh / no new sidebar record" confusion (the
read identity silently differed from the write identity).

## What changed

- `apps/web/app/page.tsx`
  - Removed the `Settings2` dev-settings toggle button and the collapsible dev
    panel that held the token input.
  - `TopBar` now consumes only `{ sandbox }` from `useCocola()`.
  - Dropped the now-unused `useState` and `Settings2` imports.

- `apps/web/app/runtime-provider.tsx`
  - Removed `token` / `setToken` from `CocolaContextValue`, the provider state,
    the `tokenRef`, and the context value memo (and their deps).
  - Removed the localStorage read (lazy init) and the mirror `useEffect`.
  - All three fetches now send **no** `authorization` header:
    `refreshConversations`, the `onNew` chat POST, and `loadConversation`.
  - Sidebar list `useEffect` no longer depends on `token`.

## Scope note

The standalone raw event-log debug tool at `/raw`
(`apps/web/app/(debug)/raw/page.tsx`) keeps its own independent token box. It is
a deliberately bare developer tool and does not use the `useCocola` context, so
it is left untouched.

## Validation

- `npx tsc --noEmit` — clean
- `npm run lint` (next lint) — no warnings or errors
- `npm run build` (next build) — compiled successfully
