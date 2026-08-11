# fix: make Admin operational surfaces compact and consistent

- Change time: 2026-08-12 02:49 (+08:00)

## Reason

Several Admin pages mixed fonts, colors, button densities, truncated values,
and multi-line table cells. Sandbox and storage rows exposed diagnostic details
directly in the main table, while missing copy affordances, inconsistent loading
alignment, and redundant controls made routine operations harder to scan.

## Changes

- `apps/web/components/admin/` and shared dialog controls: align navigation,
  truncated-value copy actions, pagination, feedback, and confirmation loading
  states while preserving each Admin page's established accent color.
- Admin operational pages: simplify visible columns, keep one-line summaries,
  move sandbox diagnostics into a centered details modal, restore safe Session
  Storage batch operations, and make measurement feedback compact.
- Model, node, settings, user, audit, scheduled-task, MCP, log, architecture,
  token-usage, and storage surfaces now use consistent typography, disclosure
  indicators, alignment, and reusable Admin controls.
- `apps/admin-api/cmd/storage-probe/`: make storage measurement output and tests
  match the restored measurement workflow.
- Frontend regression tests cover operational columns, centered dialogs,
  truncated values and copy controls, pagination, page accents, and loading
  alignment.

Detailed diagnostics remain available through secondary actions rather than
being duplicated in every table row.
