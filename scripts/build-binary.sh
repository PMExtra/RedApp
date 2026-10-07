#!/bin/sh
# Shared native CGO/static build for local, Docker and CI artifact builds.
set -eu
output=${1:-bin/redapp}
version=${2:-dev}
revision=${3:-unknown}
case "$version:$revision" in *[!a-zA-Z0-9._:+-]*) echo 'Unsafe version or revision' >&2; exit 1;; esac
mkdir -p "$(dirname "$output")"
CGO_ENABLED=1 go build -tags netgo,osusergo,sqlite_omit_load_extension -trimpath \
  -ldflags="-linkmode external -extldflags -static -X main.version=$version -X main.revision=$revision" \
  -o "$output" ./cmd/redapp
