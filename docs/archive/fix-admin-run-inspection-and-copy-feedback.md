# fix: improve run inspection and copy feedback

- Change time: 2026-08-21 17:30 (+08:00)

## Reason

The Agent Runs table compressed conversation metadata inconsistently, so titles and IDs alternated between one and two lines and truncated values could not reliably reveal their full content. The trace timeline also misaligned its summary, event icons, and connecting rail. In chat code blocks, copying could silently fail on HTTP deployments where the modern Clipboard API is unavailable or rejected, and successful copies lacked clear visual confirmation.

## Changes

- `apps/web/app/admin/audit/page.tsx`: keep conversation metadata on two deterministic rows, widen that column to 360 pixels, preserve compact single-line fields, and rebuild localized table cells after a language change.
- `apps/web/components/admin/admin-ui.tsx`: show full values on hover or keyboard focus only when the displayed value is actually truncated, while preserving navigation and copy actions.
- `apps/web/app/admin/traces/[traceId]/page.tsx`: align the timeline summary, event icons, and continuous connector rail.
- `apps/web/components/assistant-ui/markdown-text.tsx`: add an HTTP-compatible copy fallback, restore focus after fallback copying, clean up copy-state timers, and show a check icon after success.
- `apps/web/lib/admin-operational-ui.test.mjs` and `apps/web/lib/chat-message-layout.test.mjs`: add regression coverage for the corrected table, tooltip, trace, and copy behavior.
- The 360-pixel width is scoped to the Agent Runs conversation column instead of changing the shared data grid, avoiding unrelated Admin layout changes.
