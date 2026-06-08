"""Configuration primitives. Concrete loaders arrive in M3."""
from __future__ import annotations

from enum import Enum

from pydantic import BaseModel, Field


class Env(str, Enum):
    DEV = "dev"
    STAGING = "staging"
    PROD = "prod"


class CommonSettings(BaseModel):
    """Embedded into every service-specific settings model."""

    env: Env = Field(default=Env.DEV)
    service_name: str = Field(default="cocola-service")
    log_level: str = Field(default="INFO")
