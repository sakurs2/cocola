# fix: make workspace layouts responsive and restore Compose storage access

- Change time: 2026-08-20 19:48 (+08:00)

## Reason

Wide user-workspace pages shared one fixed maximum width, so data surfaces left excessive whitespace while
form pages and catalog cards had different width needs. The global header actions followed the content cap,
Wiki simple creation forms used a side sheet, and Admin route changes shifted when the vertical scrollbar
appeared.

On the Compose deployment, Sandbox Nodes and Session Storage returned 500 because the CLI created the
host sandbox root with mode `0700`, while Host Agent dropped all Linux capabilities. A live server trace
confirmed `openat("/storage/users") = EACCES`. The Host Agent health check only inspected Docker, so the
container remained healthy despite being unable to read its storage mount. The Nodes API also treated this
optional usage enrichment as a requirement and failed the complete response.

## Changes

- `apps/web/components/heroui-workspace/`, workspace pages, and `apps/web/app/globals.css`: split workspace
  frames into fluid operational canvases and bounded content pages, keep header actions aligned to the shell,
  and keep every user and Admin resource-catalog card at a stable `20rem` width. Catalogs wrap from the left,
  add cards per row as space becomes available, and only shrink cards when the container is narrower.
- `apps/web/components/wiki/wiki-workspace.tsx`: move simple folder, Markdown, and rename forms to a centered
  HeroUI Modal; consolidate folder, page, and upload actions into a contextual create menu beside the current
  folder path, remove the fixed bottom action bar, expose manual refresh progress, and reserve the resizable
  sidebar width for desktop so the narrow-screen file list fills its canvas. Render directory paths with a
  filesystem-style root, and replace oversized folder cards with compact fixed-width tiles on a fluid canvas.
- `apps/web/app/admin/`: reserve scrollbar space, preserve node data when usage enrichment fails, and load
  node filesystems and Session Storage independently with localized section errors.
- `apps/cli/internal/assets/compose.yaml`: keep `cap_drop: ALL` and add only `DAC_OVERRIDE`, which Host Agent
  needs to inspect and clean up Session files created by other UIDs without weakening host directory modes.
- `apps/admin-api/cmd/storage-probe/`: include storage-root readability in health and log the failing scan path
  with its original filesystem error.
- `apps/admin-api/internal/service/`: wrap Host Agent and database usage failures with their stage, log the
  degraded Nodes enrichment once per request, and keep destructive node operations fail-closed.
- Go and Web regression tests cover the Compose capability contract, storage health and diagnostics, graceful
  Admin degradation, centered Wiki forms, stable Admin width, and responsive workspace layouts.

## Tradeoffs

- The Host Agent is not privileged and receives no default capability set; `DAC_OVERRIDE` is the single added
  capability because its existing API includes both cross-UID reads and orphan deletion. Changing the host
  root to `0755` would expose more metadata and still fail on legitimate `0700/0600` Session contents.
- Data pages use the available canvas, while profile and project creation remain bounded for readability.
  This avoids viewport-specific maximum widths and JavaScript resize logic.
- Fixed-width catalog cards deliberately leave any incomplete final row aligned with the page title instead of
  centering it or redistributing unused width; empty states remain full-width and centered independently.
- Direct catalog cards no longer carry the Grid-era `h-full` utility. Flex rows still align peer cards to the
  tallest content in that row without resolving percentage height against the available page canvas.
- Architecture health behavior is unchanged because its endpoint already reports component-level status and
  was verified independently; the fix is limited to the confirmed storage failure chain.
