# fix: keep the Agent Runtime SDK fixture aligned with production hooks

- Change time: 2026-08-11 20:58 (+08:00)
- Related workflow: https://github.com/sakurs2/cocola/actions/runs/31491665743

## Reason

The `v0.1.28` Release workflow failed in the Agent Runtime test suite after the
Artifact binary-read guard added a production `PreToolUse` hook. The MCP option
translation test supplied a minimal fake Claude Agent SDK that exposed
`ClaudeAgentOptions` but not `HookMatcher`, so option construction failed before
the assertions ran. The real pinned SDK provides `HookMatcher`; the defect was
limited to the stale test fixture.

## Changes

- `apps/agent-runtime/tests/test_agent_shim_mcp.py`: add a typed-enough fake
  `HookMatcher` to the local SDK fixture and assert that the Read guard is
  registered exactly once for the `Read` tool.

Production runtime behavior is unchanged. The fix keeps the test seam explicit
instead of weakening the runtime hook when a fake SDK is incomplete.
