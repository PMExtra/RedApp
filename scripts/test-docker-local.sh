#!/bin/sh
# 未指定 REDAPP_TEST_IMAGE 时，用根 Dockerfile 的 prebuilt runtime 阶段离线打包 bin/redapp；不需要拉取外部镜像（需要 BuildKit）。
set -eu
task_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
task_temp=$(mktemp -d)
export DOCKER_CONFIG="$task_temp/docker-config"
mkdir -p "$DOCKER_CONFIG"
task_name="redapp-test-$$"
task_volume="$task_name-data"
task_image="${REDAPP_TEST_IMAGE:-$task_name:local}"
# Tests may select a manifest variant explicitly; ordinary runs use Docker's host selection.
docker_run() {
  if [ -n "${REDAPP_TEST_PLATFORM:-}" ]; then
    docker run --platform "$REDAPP_TEST_PLATFORM" "$@"
  else
    docker run "$@"
  fi
}
cleanup() {
  # Also remove image-declared anonymous volumes when REDAPP_DATA changes mounts.
  docker rm -fv "$task_name" "$task_name-second" >/dev/null 2>&1 || true
  docker volume rm "$task_volume" >/dev/null 2>&1 || true
  if [ -z "${REDAPP_TEST_IMAGE:-}" ]; then docker image rm "$task_image" >/dev/null 2>&1 || true; fi
  rm -rf "$task_temp"
}
trap cleanup EXIT INT TERM
if [ -z "${REDAPP_TEST_IMAGE:-}" ]; then
cp "$task_root/bin/redapp" "$task_temp/redapp"
cp /etc/ssl/certs/ca-certificates.crt "$task_temp/ca-certificates.crt"
mkdir "$task_temp/data"
chmod 0700 "$task_temp/data"
# A private DOCKER_CONFIG must still find user-installed CLI plugins such as buildx.
if [ -d "${HOME:-}/.docker/cli-plugins" ]; then ln -s "$HOME/.docker/cli-plugins" "$DOCKER_CONFIG/cli-plugins"; fi
DOCKER_BUILDKIT=1 docker build -f "$task_root/Dockerfile" --target runtime --build-arg RUNTIME_FILES=prebuilt \
  -t "$task_image" "$task_temp" >/dev/null
fi
# Verify actual /etc discovery and one-file selection without starting a service.
cat > "$task_temp/config.yaml" <<'CONFIG'
# A partial YAML file can override defaults.
listen: ':8181'
download_limits: {max_writers: 20, max_readers: 600, max_artifact_bytes: '4GiB'}
CONFIG
cat > "$task_temp/selected.json" <<'CONFIG'
{"listen":":8282","download_limits":{"max_writers":20,"max_readers":600,"max_artifact_bytes":"4gb"}}
CONFIG
printf '%s\n' 'listen: [' > "$task_temp/invalid.yaml"
chmod 0644 "$task_temp/config.yaml" "$task_temp/selected.json" "$task_temp/invalid.yaml"
docker_run --rm --network none --read-only -v "$task_temp/config.yaml:/etc/redapp/config.yaml:ro" "$task_image" config validate
# Invalid default YAML must not be read when an env path selects JSON.
docker_run --rm --network none --read-only -v "$task_temp/invalid.yaml:/etc/redapp/config.yaml:ro" -v "$task_temp/selected.json:/selected.json:ro" -e REDAPP_CONFIG=/selected.json "$task_image" config validate
# CLI selection overrides a missing env path as well as the invalid default.
docker_run --rm --network none --read-only -v "$task_temp/invalid.yaml:/etc/redapp/config.yaml:ro" -v "$task_temp/config.yaml:/selected.yaml:ro" -e REDAPP_CONFIG=/missing.json "$task_image" config validate --config /selected.yaml
if docker_run --rm --network none --read-only -v "$task_temp/invalid.yaml:/etc/redapp/config.yaml:ro" "$task_image" config validate >"$task_temp/invalid.log" 2>&1; then
  echo 'Invalid automatic YAML was ignored' >&2; exit 1
fi
grep -q 'invalid YAML' "$task_temp/invalid.log"
if docker_run --rm --network none --read-only -e REDAPP_CONFIG=/missing.yaml "$task_image" config validate >"$task_temp/missing.log" 2>&1; then
  echo 'Missing explicit configuration was ignored' >&2; exit 1
fi
grep -q '/missing.yaml' "$task_temp/missing.log"
docker volume create "$task_volume" >/dev/null
docker_run -d --network none --read-only --name "$task_name" -v "$task_volume:/var/lib/redapp" "$task_image" >/dev/null
task_try=0
while [ "$task_try" -lt 20 ]; do
  if docker exec "$task_name" /redapp healthcheck; then break; fi
  task_try=$((task_try+1))
  sleep 1
done
docker exec "$task_name" /redapp healthcheck
if [ -n "${REDAPP_TEST_PLATFORM:-}" ]; then
  test "$(docker inspect --format '{{.Os}}/{{.Architecture}}' "$(docker inspect --format '{{.Image}}' "$task_name")")" = "$REDAPP_TEST_PLATFORM"
fi
docker_run --rm --network none "$task_image" version
test "$(docker inspect --format '{{.Config.User}}' "$task_name")" = '65532:65532'
test "$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$task_name")" = 'none'
test "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$task_name")" = 'true'
test "$(docker inspect --format '{{len .Mounts}}' "$task_name")" = 1
test "$(docker inspect --format '{{range .Mounts}}{{if eq .Type "volume"}}{{.Destination}}{{end}}{{end}}' "$task_name")" = '/var/lib/redapp'
# Capture bootstrap logs privately; never print credentials to a report.
docker logs "$task_name" >"$task_temp/first.log" 2>&1
grep -q 'data directory /var/lib/redapp' "$task_temp/first.log"
test "$(grep -c 'Initial admin password' "$task_temp/first.log")" = 1
if docker_run --name "$task_name-second" --network none --read-only -v "$task_volume:/var/lib/redapp" "$task_image" >"$task_temp/second.log" 2>&1; then
  echo '第二实例错误地取得独占目录' >&2
  exit 1
fi
grep -q 'Data directory is already owned by another instance' "$task_temp/second.log"
docker kill "$task_name" >/dev/null
docker start "$task_name" >/dev/null
task_try=0
while [ "$task_try" -lt 20 ]; do
  if docker exec "$task_name" /redapp healthcheck; then break; fi
  task_try=$((task_try+1))
  sleep 1
done
docker exec "$task_name" /redapp healthcheck
docker logs "$task_name" >"$task_temp/after.log" 2>&1
test "$(grep -c 'Initial admin password' "$task_temp/after.log")" = 1
docker stop --time 20 "$task_name" >/dev/null
docker rm "$task_name" >/dev/null
# Recreate the same volume at a different path using environment only. Healthcheck
# must follow the changed listener, independently of PUBLIC_URL.
docker_run -d --network none --read-only --name "$task_name" -v "$task_volume:/state" -e REDAPP_DATA=/state -e REDAPP_LISTEN=:18081 -e REDAPP_MAX_WRITERS=20 -e REDAPP_MAX_READERS=600 -e REDAPP_MAX_ARTIFACT_BYTES=4GiB -e REDAPP_PUBLIC_URL=https://links.example "$task_image" >/dev/null
task_try=0
while [ "$task_try" -lt 20 ]; do
  if docker exec "$task_name" /redapp healthcheck; then break; fi
  task_try=$((task_try+1))
  sleep 1
done
docker exec "$task_name" /redapp healthcheck
docker logs "$task_name" >"$task_temp/recreated.log" 2>&1
grep -q 'listener :18081, data directory /state' "$task_temp/recreated.log"
if grep -q 'Initial admin password' "$task_temp/recreated.log"; then
  echo '重建容器后数据库未保持' >&2
  exit 1
fi
docker stop --time 20 "$task_name" >/dev/null
docker cp "$task_name:/state/state.sqlite" "$task_temp/state.sqlite"
python3 - "$task_temp/state.sqlite" <<'PY'
import sqlite3
import sys
with sqlite3.connect('file:' + sys.argv[1] + '?mode=ro&immutable=1', uri=True) as db:
    # Explicit check: assert would vanish under python -O / PYTHONOPTIMIZE.
    versions = db.execute('SELECT version FROM schema_version').fetchall()
    if versions != [(11,)]:
        sys.exit(f'Unexpected persisted schema versions: {versions}')
print('Fresh schema 11 persisted through runtime restart/recreation')
PY
echo "Docker runtime (${REDAPP_TEST_PLATFORM:-host})：无配置默认启动、YAML/JSON单文件选择/失败保护、新writer/reader字段和容量简写、环境变量改路径/端口、禁用网络、非 root、只读根、持久性/健康/实例锁/崩溃恢复通过。"
