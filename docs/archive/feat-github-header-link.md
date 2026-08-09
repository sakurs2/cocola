# feat: add a global Cocola GitHub header link

- Change time: 2026-08-09 17:50 (+08:00)

## Reason

Users need a direct, predictable way to reach Cocola's public GitHub repository without adding a new navigation section or explanatory copy.

## Changes

- `apps/web/components/assistant-ui/workspace-header-actions.tsx`: added a compact HeroUI GitHub icon link beside the existing Light/Dark toggle with tooltip, keyboard focus, and safe new-window behavior.
- `apps/web/app/page.tsx`, `apps/web/components/assistant-ui/workspace-shell.tsx`, `apps/web/lib/workspace-routes.ts`, and `apps/web/components/admin/admin-shell.tsx`: reused the same action group across chat, workspace, Project, and Admin headers while ensuring Project tasks render it only in the outer workspace header through one shared route classifier.
- `apps/web/lib/`: added source-contract coverage for the shared placement, Project de-duplication, hover copy, and external-link safety attributes.
