#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dist=$root/dist
mkdir -p "$dist"

for target in darwin/arm64 darwin/amd64 linux/amd64 windows/amd64; do
  os=${target%/*}
  arch=${target#*/}
  "$root/scripts/package.sh" "$os" "$arch" "$dist/ccuv-dataset-weread-$os-$arch.tar.gz"
done

cd "$dist"
shasum -a 256 ccuv-dataset-weread-*.tar.gz > checksums.txt
