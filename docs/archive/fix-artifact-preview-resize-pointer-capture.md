# fix: prevent artifact preview resizing from getting stuck

- Change time: 2026-08-22 12:55 (+08:00)

## Reason

When a user enlarged the right-side HTML artifact preview and then dragged the divider back across the preview, resizing could stop and the page cursor could remain in the resize state. The preview iframe received the pointer after the cursor crossed into it, so the parent page no longer received the pointer movement or completion events needed to update the width and restore global styles.

## Changes

- `apps/web/lib/dock-resize.mjs`: capture the active pointer and clean up resizing on pointer completion, cancellation, focus loss, capture loss, or component teardown.
- `apps/web/app/page.tsx`: use the shared resize lifecycle and run its cleanup when the workspace unmounts or a new drag begins.
- `apps/web/lib/dock-resize.test.mjs`: cover resizing across iframe content and every fallback cleanup path.
- `apps/web/lib/project-task-ui.test.mjs`: keep the project workspace minimum-width contract aligned with the shared resize helper.
- The existing width bounds and visual design are unchanged; the fix only makes the interaction lifecycle reliable.
