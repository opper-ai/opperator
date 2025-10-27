#!/usr/bin/env bash

set -euo pipefail

IFS=$' \t\n'

log() {
  printf '[release] %s\n' "$*"
}

fail() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: ./scripts/release.sh [version]

Builds release artifacts for supported platforms and writes them to dist/<version>.
If no version is provided, the script will attempt to infer it from the current git tag
and fall back to "dev".

Set TARGETS to override the default list of OS/ARCH tuples
  TARGETS="linux/amd64 linux/arm64" ./scripts/release.sh v1.0.0
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if ! command -v go >/dev/null 2>&1; then
  fail "go toolchain is required"
fi

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)

version=${1:-}

if [[ -z "$version" ]]; then
  if command -v git >/dev/null 2>&1 && git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    version=$(git -C "$repo_root" describe --tags --dirty --always 2>/dev/null || true)
  fi
  version=${version:-dev}
fi

dist_root="$repo_root/dist"
target_dir="$dist_root/$version"

mkdir -p "$dist_root"
rm -rf "$target_dir"
mkdir -p "$target_dir"

default_targets=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
)

targets_string=${TARGETS:-"${default_targets[*]}"}
read -r -a targets <<<"$targets_string"

if [[ ${#targets[@]} -eq 0 ]]; then
  fail "no build targets configured"
fi

artifacts=()

log "Building Opperator $version"

for target in "${targets[@]}"; do
  IFS='/ ' read -r os arch <<<"$target"
  if [[ -z "$os" || -z "$arch" ]]; then
    fail "invalid target entry: $target"
  fi

  artifact_base="opperator-${version}-${os}-${arch}"
  binary_name="$artifact_base"
  if [[ "$os" == "windows" ]]; then
    binary_name+=".exe"
  fi

  log "Building for $os/$arch"
  GOOS="$os" \
  GOARCH="$arch" \
  CGO_ENABLED=0 \
    go build \
      -trimpath \
      -mod=readonly \
      -ldflags "-s -w -X opperator/version.Version=$version" \
      -o "$target_dir/$binary_name" \
      "$repo_root/cmd/app"

  if [[ "$os" == "windows" ]]; then
    archive_name="${artifact_base}.zip"
    (cd "$target_dir" && zip -q "$archive_name" "$binary_name")
  else
    archive_name="${artifact_base}.tar.gz"
    (cd "$target_dir" && tar -czf "$archive_name" "$binary_name")
  fi

  rm "$target_dir/$binary_name"
  artifacts+=("$archive_name")
done

checksum_tool=""
if command -v shasum >/dev/null 2>&1; then
  checksum_tool="shasum -a 256"
elif command -v sha256sum >/dev/null 2>&1; then
  checksum_tool="sha256sum"
fi

if [[ -n "$checksum_tool" ]]; then
  log "Computing checksums"
  (
    cd "$target_dir"
    $checksum_tool "${artifacts[@]}" > SHA256SUMS
  )
else
  log "Skipping checksums (missing shasum/sha256sum)"
fi

log "Artifacts written to $target_dir"
