#!/bin/sh
# Build on the host's native architecture, with the same pinned C/libc as the Dockerfile.
set -eu
task_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
case "$(uname -m)" in
  x86_64) task_arch=amd64;;
  aarch64|arm64) task_arch=arm64;;
  *) echo 'Release builds require native Linux amd64 or arm64' >&2; exit 1;;
esac
test "$(uname -s)" = Linux
task_image=$(sh "$task_root/scripts/dockerfile-arg.sh" GO_IMAGE)
task_cache=${REDAPP_NATIVE_CACHE:-${TMPDIR:-/tmp}/redapp-native-go-$(id -u)}
mkdir -p "$task_cache/mod" "$task_cache/build"
task_cache=$(CDPATH= cd -- "$task_cache" && pwd)
docker run --rm --platform "linux/$task_arch" --user "$(id -u):$(id -g)" \
  -e HOME=/tmp -e GOCACHE=/go-cache/build -e GOMODCACHE=/go-cache/mod \
  -e GOAMD64=v1 -e GOARM64=v8.0 \
  -v "$task_root:/src" -v "$task_cache:/go-cache" -w /src \
  "$task_image" \
  sh scripts/build-binary.sh bin/redapp "${1:-dev}" "${2:-unknown}"
