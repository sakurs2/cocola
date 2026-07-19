#!/usr/bin/env bash
# Compatibility wrapper for images or deployment manifests that still invoke
# the pre-runtime-contract entrypoint path.
#
# Runs as root at container start so it can install the egress firewall BEFORE
# any user `docker exec` lands; user execs are pinned to the non-root cocola
# user by the provider, so they cannot alter the rules. When no egress policy is
# configured (COCOLA_EGRESS_ALLOWLIST unset) the firewall step is skipped and
# the container behaves exactly as the legacy keep-alive image.
set -euo pipefail

exec /opt/cocola/runtime-entrypoint.sh
