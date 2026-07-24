# fix: Move Plan Mode into the composer slash menu

- Change time: 2026-07-24 12:44 (+08:00)

## Reason

Plan Mode was exposed as a permanent Execute/Plan picker in the composer, which added visual weight to every conversation and did not match the intended command-driven workflow. Users expect to discover Plan Mode by typing `/`, see its active state beside the selected model, and be able to leave the mode before sending another message. The initial slash-menu revision also hid the Skills category when no skills were configured, reused the Memory icon, and left clarification responses visually indistinguishable from normal execution turns.

## Changes

- `apps/web/components/assistant-ui/thread.tsx`: moves Plan Mode into the existing slash menu, adds persistent Commands and Skills tabs with an explicit empty Skills state, shows the active mode after the model picker, and adds an explicit exit action.
- `apps/web/components/assistant-ui/thread.tsx`: gives Plan Mode a distinct indigo map icon in the command, composer state, assistant response header, and Plan Card; every Plan response now shows `Plan mode · Read-only`, including clarification turns.
- `apps/web/app/runtime-provider.tsx`: clears an unsent plan-revision intent when the user exits Plan Mode.
- `apps/web/lib/plan-mode.mjs`: centralizes the English command, active-state, cancellation, and slash-menu copy.
- `apps/web/lib/plan-mode.test.mjs`: locks the new English product copy and command identity.

## Notes

- The Plan Mode API, Plan Card lifecycle, approval flow, and default Execute behavior are unchanged.
- Plan Mode remains available only for Claude Code conversations.
