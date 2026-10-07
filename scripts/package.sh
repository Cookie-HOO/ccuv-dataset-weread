#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
target_os=${1:-darwin}
target_arch=${2:-arm64}
out=${3:-"$root/dist/ccuv-dataset-weread-$target_os-$target_arch.tar.gz"}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$(dirname "$out")"
cd "$root"
entrypoint=bin/ccuv-dataset-weread
binary=$work/ccuv-dataset-weread
if [ "$target_os" = windows ]; then
  entrypoint=$entrypoint.exe
  binary=$binary.exe
fi
sed \
  -e "s/\"os\": \"darwin\", \"arch\": \"arm64\"/\"os\": \"$target_os\", \"arch\": \"$target_arch\"/" \
  -e "s#\"entrypoint\": \"bin/ccuv-dataset-weread\"#\"entrypoint\": \"$entrypoint\"#" \
  manifest.json > "$work/manifest.json"
CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -ldflags='-s -w -buildid=' -o "$binary" ./cmd/ccuv-dataset-weread
go run ./cmd/package \
  --out "$out" \
  --manifest "$work/manifest.json" \
  --executable "$binary" \
  --entrypoint "$entrypoint"
go run ./cmd/archiveverify "$out"
