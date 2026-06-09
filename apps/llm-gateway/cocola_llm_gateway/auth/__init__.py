"""Authentication: identity-as-signed-token + verification.

Public surface:
- Identity        — resolved caller (user_id, tenant_id).
- AuthConfig      — secret/issuer/ttl/dev knobs.
- Issuer/Verifier — mint and verify tokens.
- JWTError        — raised on any invalid/expired token.

The token cocola issues IS the ANTHROPIC_API_KEY the Claude Agent SDK presents;
the gateway verifies it offline (shared HS256 secret) and attributes usage +
quota to the resolved user. See ADR-0005.
"""
from cocola_llm_gateway.auth.identity import AuthConfig, Issuer, Verifier
from cocola_llm_gateway.auth.jwt import Identity, JWTError

__all__ = ["Identity", "JWTError", "AuthConfig", "Issuer", "Verifier"]
