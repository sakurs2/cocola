#!/usr/bin/env sh
set -eu

REAL_GH="${COCOLA_REAL_GH:-/opt/cocola/gh/current/bin/gh}"

case "${1:-}" in
  --version|version|--help|-h|help)
    exec "$REAL_GH" "$@"
    ;;
  auth)
    case "${2:-}" in
      login|logout|refresh|setup-git|switch|token)
        echo "gh: persistent authentication is disabled in Cocola; connect GitHub in Connectors" >&2
        exit 2
        ;;
    esac
    ;;
esac

if [ -z "${COCOLA_PROJECT_BROKER_URL:-}" ] || [ -z "${COCOLA_PROJECT_CREDENTIAL:-}" ]; then
  echo "gh: this Project is not connected to GitHub; connect GitHub and publish or import a repository first" >&2
  exit 2
fi

exec cocola-sandbox github gh -- "$@"
