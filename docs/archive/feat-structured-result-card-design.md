# feat: Refine Structured Result Card design

- Change time: 2026-07-25 16:40 (+08:00)

## Reason

Structured Result Cards used one generic inset-panel treatment for every
renderer. Table results appeared as a rounded table nested inside a larger
rounded card, while Summary, List, and Metrics results exposed structured data
as repetitive boxes or raw JSON strings. The visual hierarchy made large
results feel heavy and reduced scanning efficiency.

## Changes

- `apps/web/components/assistant-ui/rich-message-parts.tsx`: keeps one shared
  outer Card Shell, adds renderer-specific icons and labels, and gives each
  renderer its own information layout.
- Table results now extend directly to the Card edges, retain keyboard
  horizontal scrolling, and keep the identifier column visible while scrolling.
- Summary results use a lead, optional status, and flat definition rows. List
  results promote object titles and expose remaining fields. Metrics use an open
  data grid with units and trends instead of nested tiles.
- `apps/web/lib/structured-result-view.ts`: adds pure, type-safe view-model
  helpers that preserve unknown historical data while normalizing known
  renderer shapes.
- `apps/web/lib/structured-result-view.test.mjs`: covers readable labels,
  noncanonical historical Summary data, object Lists, Metrics metadata, and both
  supported Table column contracts.

The message protocol, persisted payload, Runtime tools, and unknown-renderer JSON
fallback remain unchanged.
