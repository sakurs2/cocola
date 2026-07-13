#!/bin/sh
set -eu

REPOSITORY="${COCOLA_INSTALL_REPOSITORY:-sakurs2/cocola}"
CLI_VERSION="latest"
INSTALL_DIR="${COCOLA_INSTALL_DIR:-$HOME/.local/bin}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --cli-version)
      [ "$#" -ge 2 ] || { echo "--cli-version requires a value" >&2; exit 2; }
      CLI_VERSION="$2"
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || { echo "--install-dir requires a value" >&2; exit 2; }
      INSTALL_DIR="$2"
      shift 2
      ;;
    --)
      shift
      break
      ;;
    *) break ;;
  esac
done

case "$(uname -s)" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *) echo "unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

archive="cocola_${os}_${arch}.tar.gz"
if [ "$CLI_VERSION" = "latest" ]; then
  release_url="https://github.com/${REPOSITORY}/releases/latest/download"
else
  case "$CLI_VERSION" in v*) tag="$CLI_VERSION" ;; *) tag="v$CLI_VERSION" ;; esac
  release_url="https://github.com/${REPOSITORY}/releases/download/${tag}"
fi

temporary="$(mktemp -d 2>/dev/null || mktemp -d -t cocola-install)"
staged=""
trap '[ -z "$staged" ] || rm -f "$staged"; rm -rf "$temporary"' EXIT INT TERM

echo "==> Downloading Cocola CLI (${os}/${arch})"
curl -fsSL "$release_url/$archive" -o "$temporary/$archive"
curl -fsSL "$release_url/checksums.txt" -o "$temporary/checksums.txt"

expected="$(awk -v name="$archive" '$2 == name {print $1}' "$temporary/checksums.txt")"
[ -n "$expected" ] || { echo "checksum missing for $archive" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$temporary/$archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$temporary/$archive" | awk '{print $1}')"
fi
[ "$actual" = "$expected" ] || { echo "checksum verification failed" >&2; exit 1; }

tar -xzf "$temporary/$archive" -C "$temporary" cocola
mkdir -p "$INSTALL_DIR"
staged="$INSTALL_DIR/.cocola-new-$$"
cp "$temporary/cocola" "$staged"
chmod 0755 "$staged"
mv "$staged" "$INSTALL_DIR/cocola"
echo "==> Installed $INSTALL_DIR/cocola"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "    Add $INSTALL_DIR to PATH to run cocola directly." ;;
esac

if [ "$#" -eq 0 ]; then
  set -- install
fi
if [ -r /dev/tty ] && [ -w /dev/tty ]; then
  "$INSTALL_DIR/cocola" "$@" </dev/tty
else
  "$INSTALL_DIR/cocola" "$@"
fi
