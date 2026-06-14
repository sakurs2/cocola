#!/usr/bin/env bash
# cocola sandbox container entrypoint (Route A).
#
# Runs as root at container start so it can install the egress firewall BEFORE
# any user `docker exec` lands; user execs are pinned to the non-root cocola
# user by the provider, so they cannot alter the rules. When no egress policy is
# configured (COCOLA_EGRESS_ALLOWLIST unset) the firewall step is skipped and
# the container behaves exactly as the legacy keep-alive image.
set -uo pipefail

if [ -n "${COCOLA_EGRESS_ALLOWLIST+x}" ]; then
  if /opt/cocola/init-firewall.sh; then
    echo "[entrypoint] egress firewall installed"
  else
    # Fail-closed intent: if the firewall cannot be installed we still keep the
    # container up but log loudly. Operators should treat this as a hard error.
    echo "[entrypoint] WARNING: egress firewall FAILED to install" >&2
  fi
fi

# Keep the session-lived container alive; the shim/user code arrive via exec.
trap : TERM INT
sleep infinity &
wait
