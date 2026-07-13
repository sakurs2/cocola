"""Agent runtime — dispatches built-in Agent Runtime sessions.

Layering (intentional, do not violate):
    cli / grpc server  →  agent_provider (Protocol) → concrete provider
                                                  ↓
                                          sandbox_client (gRPC) → sandbox-manager

M0 only defines the Protocol and ships a no-op echo provider so the server
boots end-to-end without external dependencies.
"""
