# fix: stabilize workspace layout and development runtime

- Change time: 2026-08-20 23:51 (+08:00)

## Reason

Wide desktop viewports caused workspace page titles, core content, and catalog cards to use different horizontal anchors. Shared fixed-width card CSS also produced the wrong column count for several user and admin catalogs, while the Wiki header intentionally needed to remain flush with its full-width workspace. In development, importing the HeroUI barrel without package optimization increased incremental compilation memory, and project timestamps called `next-intl` relative-time formatting without an explicit clock, which surfaced an `ENVIRONMENT_FALLBACK` warning through the Next.js development indicator.

## Changes

- `apps/web/components/heroui-workspace/workspace-ui.tsx`: center normal workspace content in a stable `max-w-7xl` frame and use a responsive three-column default catalog grid.
- `apps/web/components/assistant-ui/workspace-shell.tsx`: align normal page titles with the shared content frame, keep global actions pinned to the viewport edge, and preserve the Wiki and immersive full-width header behavior.
- `apps/web/app/{agents,mcps,skills,projects}/` and `apps/web/app/admin/`: replace the generic fixed-width card rule with explicit page-appropriate responsive column counts.
- `apps/web/app/globals.css`: remove the obsolete shared fixed-card sizing rule.
- `apps/web/next.config.mjs`: optimize `@heroui/react` package imports to bound development compilation memory.
- `apps/web/app/projects/page.tsx`: use a shared minute-updating `useNow` value for relative-time calculation and formatting.
- `apps/web/lib/*.test.mjs`: update layout assertions and add regression coverage for HeroUI import optimization and explicit relative-time clocks.
- Tradeoff: core workspace pages keep a fixed maximum content width on wide screens, while grids still collapse responsively on narrower screens; Wiki remains deliberately full-width because its navigation pane is part of the workspace canvas.
