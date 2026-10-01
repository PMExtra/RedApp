#!/bin/sh
# Verify immutable manifest variants without replacing a shared index in Docker's classic image store.
set -eu
: "${REDAPP_TEST_IMAGE:?index digest required}"
: "${RELEASE_VERSION:?release version required}"
: "${RELEASE_REVISION:?release revision required}"
task_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
task_manifest=$(mktemp)
task_variants=$(mktemp)
trap 'rm -f "$task_manifest" "$task_variants"' EXIT INT TERM
task_index="$REDAPP_TEST_IMAGE"
docker buildx imagetools inspect "$task_index" --raw > "$task_manifest"
python3 - "$task_manifest" > "$task_variants" <<'PY'
import json, re, sys
with open(sys.argv[1]) as file:
    manifest = json.load(file)
variants = [item for item in manifest['manifests'] if item['platform']['os'] != 'unknown']
platforms = sorted((item['platform']['os'], item['platform']['architecture']) for item in variants)
assert platforms == [('linux', 'amd64'), ('linux', 'arm64')], platforms
for item in variants:
    assert re.fullmatch(r'sha256:[0-9a-f]{64}', item['digest']), item['digest']
    print('linux/' + item['platform']['architecture'], item['digest'])
PY
while read -r platform digest; do
  if [ -n "${REDAPP_VERIFY_PLATFORM:-}" ] && [ "$REDAPP_VERIFY_PLATFORM" != "$platform" ]; then continue; fi
  export REDAPP_TEST_PLATFORM="$platform"
  export REDAPP_TEST_IMAGE="${task_index%@*}@$digest"
  docker pull --platform "$platform" "$REDAPP_TEST_IMAGE"
  test "$(docker run --platform "$platform" --rm --network none "$REDAPP_TEST_IMAGE" version)" = "RedApp $RELEASE_VERSION (commit $RELEASE_REVISION)"
  sh "$task_root/scripts/test-docker-local.sh"
  printf 'Verified %s: %s\n' "$platform" "$REDAPP_TEST_IMAGE"
done < "$task_variants"
