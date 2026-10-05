#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out=${1:-"$root/dist/ccuv-dataset-weread-darwin-arm64.tar.gz"}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$(dirname "$out")"
cd "$root"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='-s -w -buildid=' -o "$work/ccuv-dataset-weread" ./cmd/ccuv-dataset-weread
go run ./cmd/package \
  --out "$out" \
  --manifest manifest.json \
  --executable "$work/ccuv-dataset-weread"
go run ./cmd/archiveverify "$out"
