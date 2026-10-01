#!/bin/sh
# 离线重建 Dockerfile runtime 阶段；不需要拉取外部镜像。
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
  docker rm -f "$task_name" "$task_name-second" >/dev/null 2>&1 || true
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
cat > "$task_temp/Dockerfile" <<'DOCKER'
FROM scratch
COPY redapp /redapp
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chown=65532:65532 data /var/lib/redapp
USER 65532:65532
ENV REDAPP_DATA=/var/lib/redapp REDAPP_LISTEN=:8080 REDAPP_PUBLIC_URL=http://localhost:8080
VOLUME ["/var/lib/redapp"]
EXPOSE 8080
HEALTHCHECK --interval=2s --timeout=5s --start-period=1s --retries=5 CMD ["/redapp", "healthcheck"]
ENTRYPOINT ["/redapp"]
DOCKER
docker build -t "$task_image" "$task_temp" >/dev/null
fi
docker volume create "$task_volume" >/dev/null
docker_run -d --read-only --name "$task_name" -v "$task_volume:/var/lib/redapp" "$task_image" >/dev/null
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
test "$(docker inspect --format '{{range .Mounts}}{{if eq .Type "volume"}}{{.Destination}}{{end}}{{end}}' "$task_name")" = '/var/lib/redapp'
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$task_name" | grep -qx 'REDAPP_DATA=/var/lib/redapp'
# Capture bootstrap logs privately; never print credentials to a report.
docker logs "$task_name" >"$task_temp/first.log" 2>&1
grep -q 'data directory /var/lib/redapp' "$task_temp/first.log"
test "$(grep -c 'Initial admin password' "$task_temp/first.log")" = 1
if docker_run --name "$task_name-second" --read-only -v "$task_volume:/var/lib/redapp" "$task_image" >"$task_temp/second.log" 2>&1; then
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
# Empty REDAPP_DATA verifies the binary default, independently of image ENV.
docker_run -d --read-only --name "$task_name" -e REDAPP_DATA= -v "$task_volume:/var/lib/redapp" "$task_image" >/dev/null
task_try=0
while [ "$task_try" -lt 20 ]; do
  if docker exec "$task_name" /redapp healthcheck; then break; fi
  task_try=$((task_try+1))
  sleep 1
done
docker exec "$task_name" /redapp healthcheck
docker logs "$task_name" >"$task_temp/recreated.log" 2>&1
if grep -q 'Initial admin password' "$task_temp/recreated.log"; then
  echo '重建容器后数据库未保持' >&2
  exit 1
fi
docker stop --time 20 "$task_name" >/dev/null
echo "Docker runtime (${REDAPP_TEST_PLATFORM:-host})：非 root、只读根、新空命名卷/重建持久性、健康检查、双实例拒绝、SIGKILL/正常停止通过。"
