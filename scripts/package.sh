#!/usr/bin/env bash
set -euo pipefail

version="${1:?usage: package.sh vX.Y.Z}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "version must be vX.Y.Z" >&2
  exit 1
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="$root/dist"
tmp="$(mktemp -d)"
trap 'rm -r "$tmp"' EXIT

rm -rf "$output"
mkdir -p "$output"
cd "$root"
# Release builds from the published SDK pinned in go.mod, never from a local go.work.
export GOWORK=off
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  name="vertra_${version}_${os}_${arch}"
  bin="vertra"
  [[ "$os" == windows ]] && bin="vertra.exe"
  mkdir -p "$tmp/$name"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$version" -o "$tmp/$name/$bin" ./cmd/vertra
  if [[ "$os" == windows ]]; then
    (cd "$tmp/$name" && zip -q "$output/$name.zip" "$bin")
  else
    tar -C "$tmp/$name" -czf "$output/$name.tar.gz" "$bin"
  fi
done
(
  cd "$output"
  shasum -a 256 vertra_* > checksums.txt
)
