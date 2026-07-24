# fix: Replace Plan output heuristics with a structured control protocol

- Change time: 2026-07-24 20:17 (+08:00)

## Reason

Claude Plan runs could finish without a Plan Card because Cocola depended on
free-form `<cocola_plan>` output and the availability of Claude-native
`ExitPlanMode` or `AskUserQuestion` tools. Tool failures also lost their cause
at the Shim boundary, so a Plan permission denial was rendered as a generic
command failure. Resumed turns used a separate one-shot SDK path that did not
explicitly switch the live Claude session permission mode.

## Changes

- `deploy/sandbox-runtime/shim/agent_shim.py`: adds a trusted in-process Cocola
  control server with typed plan submission, clarification, and runtime
  information tools; rejects Plan runs without a structured terminal event;
  disables native completion, write, and subagent tools; records structured
  tool outcomes; and uses `ClaudeSDKClient` with an explicit permission-mode
  switch for both fresh and resumed turns.
- `apps/agent-runtime/`: updates the English Plan instructions and forwards
  structured clarification and tool outcome events without exposing internal
  control tool calls.
- `apps/gateway/`: persists tool outcomes, stores clarification as ordinary
  Assistant Text, and sends approved plans as a JSON payload instead of
  tag-delimited prompt text.
- `apps/web/`: carries tool outcomes through assistant-ui's supported
  `artifact` field and renders permission, availability, failure, and timeout
  states without parsing tool error messages.
- `scripts/sandbox-runtime-verify.sh`: aligns the real Sandbox verification
  prompt with the JSON approval payload.
- Tests cover the control server allowlist, terminal validation, clarification,
  fixed-argv runtime inspection, resumed-session permission switching, protocol
  persistence, and outcome-specific English UI labels.

## Design decisions

- The control server is created inside the Shim process. It is not a user MCP
  server, does not use the network, and cannot be configured from a request.
- Runtime version inspection uses fixed argument vectors with
  `create_subprocess_exec`; no model input becomes a command and no shell is
  involved.
- Plan completion never reads Claude plan files, parses markup, matches command
  strings, or infers failure causes from error text.
- No feature flag, product configuration, telemetry, or local image build was
  added.
