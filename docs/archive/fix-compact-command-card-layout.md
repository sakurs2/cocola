# fix: compact command execution cards

- Change time: 2026-08-12 23:16 (+08:00)

## Reason

Collapsed command cards always occupied the full conversation width and inherited an oversized rounded treatment, leaving large empty areas around short commands. When expanded, the HeroUI Card default child gap inserted an extra 12px between the header and content, making the command title appear vertically misaligned.

## Changes

- `apps/web/components/assistant-ui/rail.tsx`: size collapsed command cards to their content with responsive command limits, retain full width only for expanded details, use a compact rounded rectangle, and remove the inherited Card child gap so the header remains vertically centered.
- `apps/web/lib/long-running-command-ui.test.mjs`: add regression coverage for content-sized collapsed cards, bounded long commands, horizontal header layout, and the zero-gap expanded layout.
- The expanded command and output sections retain their existing internal spacing and full-width layout for readability.
