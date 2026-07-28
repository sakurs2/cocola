#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <bundle-version> <anthropics/skills commit SHA>" >&2
  exit 2
fi

version="${1#v}"
upstream_ref="$2"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "bundle version must be semver, for example 1.0.0" >&2
  exit 2
fi
if [[ ! "$upstream_ref" =~ ^[0-9a-f]{40}$ ]]; then
  echo "upstream ref must be a full 40-character commit SHA" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
asset_dir="${repo_root}/apps/admin-api/internal/defaultskills/assets"
archive_name="community-core-skills-v${version}.zip"
archive_path="${asset_dir}/${archive_name}"
manifest_path="${asset_dir}/community-core-manifest.json"
license_path="${asset_dir}/ANTHROPIC_FRONTEND_DESIGN_LICENSE"
temp_dir="$(mktemp -d)"
temp_archive="${temp_dir}/${archive_name}"
trap 'rm -rf "${temp_dir}"' EXIT

curl -fL \
  "https://codeload.github.com/anthropics/skills/tar.gz/${upstream_ref}" \
  -o "${temp_dir}/anthropics-skills.tar.gz"
tar -xzf "${temp_dir}/anthropics-skills.tar.gz" -C "${temp_dir}"
source_root="${temp_dir}/skills-${upstream_ref}"
source_skill="${source_root}/skills/frontend-design"
test -s "${source_skill}/SKILL.md"
test -s "${source_skill}/LICENSE.txt"

mkdir -p "${asset_dir}" "${temp_dir}/bundle/skills/frontend-design"
cp "${source_skill}/SKILL.md" "${temp_dir}/bundle/skills/frontend-design/SKILL.md"
cp "${source_skill}/LICENSE.txt" "${temp_dir}/bundle/skills/frontend-design/LICENSE.txt"
cp "${source_skill}/LICENSE.txt" "${license_path}"
touch -t 198001010000 \
  "${temp_dir}/bundle/skills/frontend-design/SKILL.md" \
  "${temp_dir}/bundle/skills/frontend-design/LICENSE.txt"
(
  cd "${temp_dir}/bundle"
  find skills -type f -print |
    LC_ALL=C sort |
    zip -X -q "${temp_archive}" -@
)
mv "${temp_archive}" "${archive_path}"

if command -v sha256sum >/dev/null 2>&1; then
  archive_sha="$(sha256sum "${archive_path}" | awk '{print $1}')"
else
  archive_sha="$(shasum -a 256 "${archive_path}" | awk '{print $1}')"
fi

jq -n \
  --arg name "community-core" \
  --arg version "${version}" \
  --arg upstream_url "https://github.com/anthropics/skills" \
  --arg upstream_ref "${upstream_ref}" \
  --arg archive "${archive_name}" \
  --arg archive_sha256 "${archive_sha}" \
  '{
    name: $name,
    version: $version,
    upstream_url: $upstream_url,
    upstream_ref: $upstream_ref,
    archive: $archive,
    archive_sha256: $archive_sha256,
    skill_ids: ["frontend-design"]
  }' >"${manifest_path}"

echo "updated ${archive_path}"
echo "updated ${manifest_path}"
echo "updated ${license_path}"
