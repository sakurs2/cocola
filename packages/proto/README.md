# proto

Source of truth for all gRPC service contracts. Generated stubs are NOT committed
(see `.gitignore` → `gen/`); CI runs `make proto-gen` and publishes artifacts.

## Layout

```
cocola/
  common/v1/   # shared messages (Error, Pagination, AuditMeta, …)
  sandbox/v1/  # SandboxManager service — sandbox-manager
  agent/v1/    # AgentRuntime service — agent-runtime
```

## Conventions

- Always `v1`, `v2`, … never unversioned.
- `service` names are PascalCase noun + "Service".
- One `.proto` file per top-level service to keep generated code paths predictable.
- Breaking changes guarded by `make proto-breaking` in CI.
