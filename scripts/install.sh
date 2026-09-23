#!/bin/sh
# Instala a CLI da Vertra Cloud no Linux e no macOS.
#   curl -fsSL https://cli.vertracloud.app/install | sh
# VERTRA_VERSION=vX.Y.Z fixa a versão; VERTRA_INSTALL_DIR troca a pasta (padrão ~/.vertracloud/bin, junto da configuração).
set -eu

repo="https://github.com/vertracloud/cli"
dir="${VERTRA_INSTALL_DIR:-$HOME/.vertracloud/bin}"

fail() { echo "vertra: $1" >&2; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported OS $(uname -s); on Windows run: irm https://cli.vertracloud.app/install | iex" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac

version="${VERTRA_VERSION:-}"
if [ -z "$version" ]; then
  # /releases/latest redireciona para /releases/tag/<versão>.
  version="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$repo/releases/latest")"
  version="${version##*/}"
fi
case "$version" in v[0-9]*) ;; *) fail "could not resolve the latest version" ;; esac

asset="vertra_${version}_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading vertra $version ($os/$arch)..."
curl -fsSL "$repo/releases/download/$version/$asset" -o "$tmp/$asset"
curl -fsSL "$repo/releases/download/$version/checksums.txt" -o "$tmp/checksums.txt"

expected="$(awk -v a="$asset" '$2 == a { print $1 }' "$tmp/checksums.txt")"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{ print $1 }')"
else
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
fi
[ -n "$expected" ] && [ "$expected" = "$actual" ] || fail "checksum mismatch for $asset"

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$dir"
mv "$tmp/vertra" "$dir/vertra"
chmod +x "$dir/vertra"
echo "Installed $dir/vertra"

case ":$PATH:" in
  *":$dir:"*) ;;
  *)
    case "${SHELL:-}" in
      */zsh) rc="$HOME/.zshrc" ;;
      */bash) rc="$HOME/.bashrc" ;;
      *) rc="$HOME/.profile" ;;
    esac
    line="export PATH=\"$dir:\$PATH\""
    grep -qsF "$line" "$rc" || printf '\n%s\n' "$line" >>"$rc"
    echo "Added $dir to PATH in $rc. Open a new terminal or run: $line"
    ;;
esac

echo "Run 'vertra --help' to get started."
