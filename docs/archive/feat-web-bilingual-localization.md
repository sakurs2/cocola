# feat: add bilingual Web localization

- Change time: 2026-08-12 14:11 (+08:00)

## Reason

Cocola's complete Web experience, including Admin, only exposed English copy. Users need a consistent English and Simplified Chinese interface without changing existing URLs, authentication, or backend contracts. During visual verification, the model icon picker also exposed inconsistent control heights and a preview alignment issue inside portalled dialogs.

## Changes

- `apps/web/i18n/`, `apps/web/messages/`, and `apps/web/components/i18n/`: add typed `en` and `zh-CN` message catalogs, locale resolution, a deterministic time zone, React Aria locale synchronization, and the shared language menu.
- `apps/web/app/api/preferences/locale/route.ts`: persist the selected locale in a strict one-year cookie and derive its `Secure` flag from the actual request protocol so HTTP self-hosted deployments remain usable.
- `apps/web/app/`, `apps/web/components/`, and related helpers: migrate fixed user-facing Web and Admin copy, status labels, dates, and number formatting while preserving backend values and user content.
- `apps/web/app/admin/models/page.tsx`: keep the original composer guidance and align the model icon preview, source selector, and image upload control to the same 44 px baseline.
- `apps/web/lib/*.test.mjs`: update UI contracts and add locale, dictionary, coverage, time-zone, cookie, and model-icon alignment regression tests.
- `apps/web/package.json`, `apps/web/next.config.mjs`, and `pnpm-lock.yaml`: integrate `next-intl` without adding a locale URL prefix.

The implementation keeps locale state browser-local, adds no database or backend localization layer, and leaves backend errors, CLI output, external integrations, Agent answers, and user-authored content unchanged.
