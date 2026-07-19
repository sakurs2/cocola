---
name: cocola-sandbox-browser
description: Use Cocola's built-in Sandbox Browser when a task needs rendered HTTP(S) page inspection, client-side text or link extraction, UI verification, a webpage screenshot, or PDF rendering. Trigger for local web-app checks and local HTML that must be served over loopback HTTP; do not use it for source-only file reading or general web research that does not require browser rendering.
---

# Cocola Sandbox Browser

Use the versioned `cocola-sandbox browser` guest CLI instead of inventing a
browser daemon or an ad-hoc output convention. It runs a headless Playwright
browser on demand, keeps browser profile state in the Session Volume, and puts
rendered artifacts under `/workspace/outputs/browser`.

## Workflow

1. Check availability before the first browser operation:

   ```bash
   cocola-sandbox browser status --json
   ```

   If it reports `disabled`, explain that the active Sandbox Profile or operator
   policy disables Browser. Do not try to bypass that policy with another
   browser binary.

2. Inspect a rendered page before taking heavier artifacts when page text and
   links are enough:

   ```bash
   cocola-sandbox browser inspect 'https://example.com' --json
   ```

3. Capture a screenshot when visual layout matters:

   ```bash
   cocola-sandbox browser screenshot 'https://example.com' --json
   cocola-sandbox browser screenshot 'https://example.com' --full-page --output homepage.png --json
   ```

4. Render a PDF only when the user needs a printable document:

   ```bash
   cocola-sandbox browser pdf 'https://example.com' --output homepage.pdf --json
   ```

5. Report the logical `/workspace/...` output paths returned by the CLI. Files
   under `./outputs/` can be published to the user by Cocola.

## Local pages

The Browser accepts only explicit `http://` or `https://` URLs. To render a
local HTML file or a locally developed site, start it on an unoccupied loopback
port, navigate to `http://127.0.0.1:<port>/...`, and stop the temporary server
when it is no longer needed. Never use `file://`, `data:`, or a host-machine
address.

## Boundaries

- Existing Sandbox egress policy still controls reachable hosts. Treat a
  navigation failure as a policy or reachability issue; do not weaken the
  firewall.
- The CLI supports navigation, rendered inspection, screenshots, and PDFs. It
  does not expose a resident CDP endpoint or a general click/form automation
  session.
- Browser profile state can survive command and Sandbox compute restarts. Do
  not run concurrent Browser commands against the shared profile.
- Avoid pages or output names containing secrets. Do not copy cookies, tokens,
  or authenticated page contents into chat unless the user explicitly needs
  that content.
