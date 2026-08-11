---
name: cocola-sandbox-artifacts
description: Use Cocola's built-in Sandbox Artifacts contract when a task must deliver files for the user to download or preview, including images, PDFs, Markdown, code, data files, and interactive HTML. Trigger when the final result should include a generated file; do not use outputs for temporary working files.
---

# Cocola Sandbox Artifacts

Use `/workspace/outputs` only for files that should become user-visible after
the current Agent turn. Cocola publishes each changed regular file from this
directory as a downloadable Artifact; temporary files belong elsewhere in the
Workspace.

1. Check the output contract before creating deliverables:

   ```bash
   cocola-sandbox artifact status --json
   ```

2. Write final files beneath `/workspace/outputs`. Nested directories are
   allowed. Use clear filenames and avoid symbolic links; links and other
   non-regular files are not published.

3. Do not call `Read` on a generated binary deliverable merely to preview or
   attach it. Inline image, audio/video, PDF, archive, office-document, and
   executable content can exceed the Agent transport limit. Verify final files
   with bounded metadata commands such as `file`, dimensions, page count, or a
   checksum. If visual inspection is essential, create and `Read` a small
   temporary preview outside `outputs`; keep the original deliverable in
   `outputs`.

4. HTML Artifacts may use inline JavaScript and external CDN resources. Keep
   relative assets together under `/workspace/outputs` when the document needs
   them, and prefer versioned dependency URLs for reproducible previews.

5. For interactive behavior or rendered-page verification, serve the HTML
   temporarily over loopback HTTP and use the separate `cocola-sandbox browser`
   capability. Do not place a long-running preview server or its logs under
   `outputs`.

6. Confirm the final inventory before responding:

   ```bash
   cocola-sandbox artifact list --json
   ```

Mention the Artifact filenames in the final response. Publication happens
after the turn, so do not invent download URLs or attempt to call Cocola's
control-plane APIs from inside the Sandbox.
