#!/usr/bin/env bash
# Install the image-managed Code Server extension set from its exact lock.
set -euo pipefail

LOCK_PATH="${1:-/opt/cocola/code-server-extensions.lock.json}"
EXTENSIONS_DIR="${2:-/opt/cocola/code-server/extensions}"
if [ ! -s "$LOCK_PATH" ]; then
  echo "extension lock not found: $LOCK_PATH" >&2
  exit 1
fi

for command in code-server curl jq sha256sum unzip; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "required extension installer command missing: $command" >&2
    exit 1
  fi
done

curl_args=(
  --fail
  --location
  --retry 3
  --retry-all-errors
  --connect-timeout 15
  --max-time 120
  --retry-max-time 180
  --silent
  --show-error
)

locked_code_server="$(jq -er '.code_server_version' "$LOCK_PATH")"
locked_code="$(jq -er '.code_version' "$LOCK_PATH")"
version_output="$(code-server --version)"
actual_code_server="$(printf '%s\n' "$version_output" | awk '$1 ~ /^[0-9]/ { print $1; exit }')"
actual_code="$(printf '%s\n' "$version_output" | sed -n 's/.* with Code \([^ ]*\).*/\1/p' | head -n1)"
if [ "$actual_code_server" != "$locked_code_server" ] || [ "$actual_code" != "$locked_code" ]; then
  echo "editor version mismatch: lock=$locked_code_server/Code-$locked_code image=$actual_code_server/Code-$actual_code" >&2
  exit 1
fi

work_dir="$(mktemp -d)"
install_home="$work_dir/home"
mkdir -p "$EXTENSIONS_DIR" "$install_home"
trap 'rm -rf "$work_dir"' EXIT

install_vsix() {
  local id="$1"
  local version="$2"
  local install_mode="$3"
  local url="$4"
  local sha256="$5"
  local artifact="$work_dir/${id}-${version}.vsix"
  local unpacked="$work_dir/${id}-${version}"
  local destination="$EXTENSIONS_DIR/${id}-${version}"

  if [[ "$sha256" == https://* ]]; then
    sha256="$(curl "${curl_args[@]}" "$sha256")"
  fi
  curl "${curl_args[@]}" --output "$artifact" "$url"
  printf '%s  %s\n' "$sha256" "$artifact" | sha256sum --check --status
  mkdir -p "$unpacked"
  unzip -q "$artifact" 'extension/*' -d "$unpacked"

  local manifest="$unpacked/extension/package.json"
  if [ ! -s "$manifest" ]; then
    echo "VSIX does not contain extension/package.json: $id@$version" >&2
    exit 1
  fi

  local manifest_id manifest_version
  manifest_id="$(jq -er '.publisher + "." + .name' "$manifest")"
  manifest_version="$(jq -er '.version' "$manifest")"
  if [ "${manifest_id,,}" != "${id,,}" ] || [ "$manifest_version" != "$version" ]; then
    echo "VSIX manifest mismatch: expected $id@$version, got $manifest_id@$manifest_version" >&2
    exit 1
  fi

  if [ "$install_mode" = "vsix-unpacked" ]; then
    # ms-python.python declares an extension pack. Installing that VSIX via the
    # CLI pulls the newest debugpy/environment extensions and makes this image
    # non-reproducible; direct extraction installs only the audited extension.
    mkdir -p "$destination"
    cp -a "$unpacked/extension/." "$destination/"
  else
    HOME="$install_home" code-server --extensions-dir "$EXTENSIONS_DIR" \
      --install-extension "$artifact" --force
  fi
}

while IFS=$'\t' read -r id version install url sha256; do
  case "$install" in
    vsix | vsix-unpacked)
      install_vsix "$id" "$version" "$install" "$url" "$sha256"
      ;;
    platform-vsix)
      case "$(dpkg --print-architecture)" in
        amd64) platform="linux-x64" ;;
        arm64) platform="linux-arm64" ;;
        *)
          echo "unsupported Ruff extension architecture: $(dpkg --print-architecture)" >&2
          exit 1
          ;;
      esac
      install_vsix "$id" "$version" "vsix" \
        "${url//\{platform\}/$platform}" "${sha256//\{platform\}/$platform}"
      ;;
    *)
      echo "unsupported extension install mode for $id: $install" >&2
      exit 1
      ;;
  esac
done < <(
  jq -er '.extensions[] | [.id, .version, .install, (.url // ""), (.sha256 // .sha256_url // "")] | @tsv' \
    "$LOCK_PATH"
)

expected="$work_dir/expected.txt"
actual="$work_dir/actual.txt"
jq -r '.extensions[] | ((.id | ascii_downcase) + "@" + .version)' "$LOCK_PATH" | sort > "$expected"
HOME="$install_home" code-server --extensions-dir "$EXTENSIONS_DIR" \
  --list-extensions --show-versions | tr '[:upper:]' '[:lower:]' | sed '/^$/d' | sort > "$actual"
if ! diff -u "$expected" "$actual"; then
  echo "installed extension inventory does not match the platform lock" >&2
  exit 1
fi

# Extensions execute as cocola but are updated only by publishing a new image.
chown -R root:root "$EXTENSIONS_DIR"
chmod -R u=rwX,go=rX "$EXTENSIONS_DIR"
