#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <version, e.g. 1.0.77>" >&2
  exit 2
fi

version="${1#v}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
asset_dir="${repo_root}/apps/admin-api/internal/defaultskills/assets"
archive_name="lark-cli-skills-v${version}.zip"
archive_path="${asset_dir}/${archive_name}"
manifest_path="${asset_dir}/lark-cli-manifest.json"
license_path="${asset_dir}/LARK_CLI_LICENSE"
temp_dir="$(mktemp -d)"
temp_archive="${temp_dir}/${archive_name}"
trap 'rm -rf "${temp_dir}"' EXIT

curl -fL \
  "https://github.com/larksuite/cli/archive/refs/tags/v${version}.tar.gz" \
  -o "${temp_dir}/lark-cli.tar.gz"
tar -xzf "${temp_dir}/lark-cli.tar.gz" -C "${temp_dir}"
source_root="${temp_dir}/cli-${version}"
test -d "${source_root}/skills"

mkdir -p "${asset_dir}"
cp "${source_root}/LICENSE" "${license_path}"
(
  cd "${source_root}"
  {
    printf '%s\n' LICENSE
    find skills -type f -print
  } |
    LC_ALL=C sort |
    zip -X -q "${temp_archive}" -@
)
mv "${temp_archive}" "${archive_path}"

if command -v sha256sum >/dev/null 2>&1; then
  archive_sha="$(sha256sum "${archive_path}" | awk '{print $1}')"
else
  archive_sha="$(shasum -a 256 "${archive_path}" | awk '{print $1}')"
fi
skill_ids="$(
  find "${source_root}/skills" -mindepth 1 -maxdepth 1 -type d -exec basename {} \; |
    LC_ALL=C sort |
    jq -R . |
    jq -s .
)"

jq -n \
  --arg name "lark-cli" \
  --arg version "${version}" \
  --arg upstream_url "https://github.com/larksuite/cli" \
  --arg upstream_ref "v${version}" \
  --arg archive "${archive_name}" \
  --arg archive_sha256 "${archive_sha}" \
  --argjson skill_ids "${skill_ids}" \
  '{
    name: $name,
    version: $version,
    upstream_url: $upstream_url,
    upstream_ref: $upstream_ref,
    archive: $archive,
    archive_sha256: $archive_sha256,
    skill_ids: $skill_ids
  }' >"${manifest_path}"

echo "updated ${archive_path}"
echo "updated ${manifest_path}"
echo "updated ${license_path}"
