#!/bin/sh
# Print the default value of a global ARG in the root Dockerfile (single source of base images).
set -eu
task_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
task_name=${1:?usage: dockerfile-arg.sh ARG_NAME}
task_value=$(sed -n "s/^ARG $task_name=//p" "$task_root/Dockerfile")
if [ -z "$task_value" ] || [ "$(printf '%s\n' "$task_value" | wc -l)" -ne 1 ]; then
  echo "Dockerfile must define exactly one non-empty ARG $task_name=" >&2
  exit 1
fi
printf '%s\n' "$task_value"
