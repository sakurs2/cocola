# fix: align Tavily preset with Web CI contracts

- Change time: 2026-08-24 14:45 (+08:00)

## Reason

The Web CI job failed two source-contract tests after the Admin MCP page gained an always-visible, unconfigured Tavily preset card. The empty-state assertion still described the previous page behavior, and the preset rendered the Tavily brand name as an unregistered visible JSX literal.

## Changes

- `apps/web/app/admin/mcps/page.tsx`: read the visible Tavily name from the localized message contract for both rendering and search matching.
- `apps/web/messages/en/admin.json` and `apps/web/messages/zh-CN/admin.json`: add the shared Tavily brand-name key.
- `apps/web/lib/admin-operational-ui.test.mjs`: cover the preset-card state and the no-match empty state instead of the removed all-empty state.
- Keep the Tavily configuration, encrypted credential storage, and runtime behavior unchanged.
