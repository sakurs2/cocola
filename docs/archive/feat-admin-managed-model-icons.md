# feat: expose managed model icons in the primary configuration flow

- Change time: 2026-08-12 02:49 (+08:00)

## Reason

Model and provider icon settings were hidden in an `Appearance` disclosure, so
administrators could easily miss them. The image option also required a public
URL, which made a basic visual configuration depend on external hosting and
introduced an unnecessarily technical setup step.

## Changes

- `apps/web/app/admin/models/page.tsx`: move icon selection into the primary
  model and provider forms, replace image URLs with local uploads, preserve
  existing icon choices while switching sources, and simplify adjacent model
  fields and disclosures.
- `apps/web/app/api/admin/model-icons/route.ts` and
  `apps/web/app/api/model-icons/[slug]/route.ts`: proxy authenticated uploads and
  serve content-addressed uploaded icons through the existing icon route.
- `apps/admin-api/internal/service/model_icons.go` and Admin API wiring: validate
  PNG, JPEG, and WebP files, enforce byte and dimension limits, and persist
  content-addressed icons in the existing object store.
- Model icon service, HTTP, and frontend regression tests cover upload,
  retrieval, validation, route reuse, and the visible configuration flow.

The implementation reuses the existing MinIO client and icon route instead of
adding another storage service or public image host.
