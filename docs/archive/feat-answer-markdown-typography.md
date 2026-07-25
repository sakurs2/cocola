# feat: Improve Assistant Answer Markdown typography

- Change time: 2026-07-25 14:02 (+08:00)

## Reason

Assistant Answers supported GitHub-Flavored Markdown, but their visual hierarchy remained close to browser-default document rendering. Long explanations, nested lists, quotations, and wide tables were harder to scan, while the live and read-only conversation surfaces used different Markdown implementations.

## Changes

- `apps/web/components/assistant-ui/markdown-text.tsx`: adds an Answer-only editorial typography system for paragraphs, headings, lists, task items, quotations, links, inline code, tables, dividers, and images.
- Adds a keyboard-accessible horizontal table container and keeps the existing code highlighting and copy controls.
- Adds `AnswerMarkdownContent` so persisted Answers use the same GFM and presentation contract as live streaming Answers.
- `apps/web/components/conversation-readonly.tsx`: switches only Assistant text parts to the shared Answer renderer; Plan, file preview, Tool, and other rich nodes remain unchanged.
- `apps/web/package.json` and `pnpm-lock.yaml`: declare the already-used `react-markdown` package as a direct Web dependency.

## Notes

- Agent output remains untrusted Markdown. The renderer does not enable MDX, raw HTML, JavaScript, imports, or content-based UI guessing.
- No Prompt, Sandbox image, Tool UI, or message protocol changes are included.
