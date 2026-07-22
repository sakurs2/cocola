---
name: cocola-sandbox-preview
description: Use Cocola's managed Preview process when building, running, or verifying a local web app or HTTP service that the user should continue opening from the Workspace Preview tab after the Agent turn ends. Do not use ordinary shell background jobs for user-facing preview servers.
---

# Cocola Sandbox Preview

Use `cocola-sandbox preview` for a development server that must remain available
after the current Agent turn. The guest CLI launches a detached, session-scoped
process, verifies that its port is reachable through the container network, and
keeps bounded status and logs under the Sandbox runtime state directory.

## Start and verify

1. Choose an unoccupied port and make the server listen on `0.0.0.0`. Framework
   flags differ, so pass the explicit host flag supported by the project:

   ```bash
   cocola-sandbox preview start --port 3000 --json -- \
     npm run dev -- --hostname 0.0.0.0
   ```

   The command defaults to the active Agent working directory: `/workspace/project`
   for Project runs and `/workspace` for ordinary conversations. Pass `--cwd`
   only when the application lives in a subdirectory.

   For Vite, use `npm run dev -- --host 0.0.0.0`. The CLI also exports `HOST`,
   `HOSTNAME`, and `PORT`, but an explicit framework flag is preferred.

2. Treat a successful `state: ready` response as the authority that Cocola's
   Preview proxy can reach the process. Do not claim that the server is ready
   based only on a framework log line.

3. Inspect the page from inside the Sandbox when visual verification is useful:

   ```bash
   cocola-sandbox browser inspect http://127.0.0.1:3000 --json
   cocola-sandbox browser screenshot http://127.0.0.1:3000 --output preview.png --json
   ```

4. Tell the user which port to enter in the Workspace Preview tab. Leave the
   managed process running when the user is expected to inspect it afterward.

## Diagnose and stop

```bash
cocola-sandbox preview status --port 3000 --json
cocola-sandbox preview logs --port 3000 --lines 100
cocola-sandbox preview stop --port 3000 --json
```

Stop the process before restarting the same managed port. Stop temporary
servers when no later user preview is expected.

## Boundaries

- Do not use Bash `run_in_background`, `&`, `nohup`, or an ad-hoc PID file for a
  user-facing preview. Those processes are tied to Agent Runtime cleanup or
  lack Cocola's readiness and lifecycle checks.
- The command working directory must remain under `/workspace`.
- Managed processes survive an Agent turn, but not loss of the Sandbox compute
  instance. A later Agent turn can start the server again from the persistent
  workspace.
- Run-scoped model, SCM, and broker credentials are removed from the detached
  process environment.
