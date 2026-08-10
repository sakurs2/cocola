# fix: align the immersive workspace header with page content

- Change time: 2026-08-10 11:34 (+08:00)

## Reason

After entering immersive mode on Wiki, the navigation sidebar disappeared but
the workspace title continued using the normal centered maximum-width header.
The exit control and `Agent workspace / Wiki` title were consequently shifted
away from the left edge used by the full-width Wiki workspace.

## Changes

- `apps/web/components/assistant-ui/workspace-shell.tsx`: use a full-width,
  compact horizontal inset for immersive headers while preserving the existing
  centered maximum width in normal mode. The fix applies consistently to every
  workspace page instead of adding a Wiki-only offset.
- `apps/web/lib/workspace-immersive-header.test.mjs`: prevent immersive mode
  from regaining the normal-mode centered header constraint.
