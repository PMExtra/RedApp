#!/usr/bin/env python3
"""Check that toolchain versions and CI pinning come from their single sources.

- Go: the ``go`` directive in go.mod (with patch version) is the only Go version;
  workflows use ``go-version-file: go.mod`` and the Dockerfile GO_IMAGE tag matches.
- Node.js: frontend/.node-version; workflows use ``node-version-file`` and the
  Dockerfile NODE_IMAGE tag matches.
- Container base images are pinned by digest; workflows pin actions by commit SHA,
  never use pull_request_target and never pull floating images during builds.
- Every actions/checkout step sets ``persist-credentials: false``. Privileged
  helper images are pinned: setup-qemu-action names an ``image:`` by digest and
  setup-buildx-action uses the ``docker`` driver or pins its BuildKit image by digest.
"""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
DIGEST = r"@sha256:[0-9a-f]{64}"
VERSION = r"\d+\.\d+\.\d+"


def dockerfile_arg(text, name):
    """Return the default of a global ``ARG name=value`` line."""
    values = re.findall(rf"^ARG {name}=(\S+)$", text, re.MULTILINE)
    if len(values) != 1:
        raise ValueError(f"Dockerfile must define ARG {name} exactly once")
    return values[0]


def workflow_steps(text):
    """Yield ``(action, step text)`` for each list item of block YAML that has ``uses:``.

    An item extends over the following lines that are blank or indented deeper
    than its ``-``.
    """
    lines = text.splitlines()
    for index, line in enumerate(lines):
        start = re.match(r"(\s*)-\s+\S", line)
        if not start:
            continue
        indent = len(start.group(1))
        end = index + 1
        while end < len(lines) and (not lines[end].strip() or len(lines[end]) - len(lines[end].lstrip()) > indent):
            end += 1
        step = "\n".join(lines[index:end])
        uses = re.search(rf"^(?: {{{indent}}}-\s+| {{{indent + 2}}})uses:\s*(\S+)", step, re.MULTILINE)
        if uses:
            yield uses.group(1), step


def step_option(step, key):
    """Return the unquoted value of ``key:`` inside a step, or None."""
    match = re.search(rf"^\s+{re.escape(key)}:\s*(.*?)\s*(?:#.*)?$", step, re.MULTILINE)
    return match.group(1).strip("'\"") if match else None


def check_steps(name, text):
    """Return the problems of individual action steps of one workflow."""
    problems = []
    for action, step in workflow_steps(text):
        if action.startswith("actions/checkout@"):
            if step_option(step, "persist-credentials") != "false":
                problems.append(f"{name}: actions/checkout must set persist-credentials: false")
        elif action.startswith("docker/setup-qemu-action@"):
            if not re.search(DIGEST + "$", step_option(step, "image") or ""):
                problems.append(f"{name}: docker/setup-qemu-action needs an image: pinned by digest")
        elif action.startswith("docker/setup-buildx-action@"):
            driver = step_option(step, "driver") or "docker-container"
            options = step_option(step, "driver-opts") or ""
            if driver != "docker" and not re.search(rf"image=moby/buildkit:[^\s,]+{DIGEST}", options):
                problems.append(
                    f"{name}: docker/setup-buildx-action must use driver: docker "
                    "or pin driver-opts image=moby/buildkit by digest"
                )
    return problems


def check(root):
    """Return a list of human-readable problems found below ``root``."""
    problems = []
    go_mod = (root / "go.mod").read_text()
    go_versions = re.findall(r"^go (\S+)$", go_mod, re.MULTILINE)
    if len(go_versions) != 1 or not re.fullmatch(VERSION, go_versions[0]):
        problems.append("go.mod needs one `go X.Y.Z` directive with a patch version")
        go_version = None
    else:
        go_version = go_versions[0]
    if re.search(r"^toolchain ", go_mod, re.MULTILINE):
        problems.append("go.mod must not declare a separate toolchain; the go directive is the version")
    node_file = root / "frontend" / ".node-version"
    node_version = node_file.read_text().strip() if node_file.is_file() else ""
    if not re.fullmatch(VERSION, node_version):
        problems.append("frontend/.node-version must contain one X.Y.Z version")

    dockerfile = (root / "Dockerfile").read_text()
    images = {
        "GO_IMAGE": (rf"golang:{re.escape(go_version or '?')}-trixie{DIGEST}", "go.mod"),
        "NODE_IMAGE": (rf"node:{re.escape(node_version or '?')}-trixie-slim{DIGEST}", "frontend/.node-version"),
    }
    for name, (pattern, source) in images.items():
        try:
            value = dockerfile_arg(dockerfile, name)
        except ValueError as error:
            problems.append(str(error))
            continue
        if not re.fullmatch(pattern, value):
            problems.append(f"Dockerfile {name}={value} must match {source} and be pinned by digest")
    for line in re.findall(r"^FROM\s+(\S+)", dockerfile, re.MULTILINE):
        if not line.startswith("$") and line != "scratch":
            problems.append(f"Dockerfile FROM {line} must use a pinned ARG image")

    for path in sorted((root / ".github").rglob("Dockerfile")):
        for image in re.findall(r"^FROM\s+(\S+)", path.read_text(), re.MULTILINE):
            if image != "scratch" and not re.search(DIGEST + "$", image):
                problems.append(f"{path.relative_to(root)}: FROM {image} is not pinned by digest")

    for path in sorted((root / ".github" / "workflows").glob("*.yml")):
        name = path.relative_to(root)
        text = path.read_text()
        if re.search(r"^\s*go-version:", text, re.MULTILINE):
            problems.append(f"{name}: use go-version-file: go.mod instead of go-version")
        if re.search(r"^\s*node-version:", text, re.MULTILINE):
            problems.append(f"{name}: use node-version-file: frontend/.node-version instead of node-version")
        if "pull_request_target" in text:
            problems.append(f"{name}: pull_request_target is not allowed")
        # A backslash-continued shell line is still the same command.
        commands = re.sub(r"\\\n\s*", " ", text)
        if re.search(r"docker (?:buildx )?build[^\n]*--pull", commands):
            problems.append(f"{name}: docker build --pull makes validation non-reproducible")
        for action in re.findall(r"^\s*(?:-\s+)?uses:\s*(\S+)", text, re.MULTILINE):
            if action.startswith("./"):
                continue
            if not re.fullmatch(r"[\w.-]+/[\w./-]+@[0-9a-f]{40}", action):
                problems.append(f"{name}: action {action} is not pinned to a commit SHA")
        problems.extend(check_steps(name, text))
    return problems


def main():
    """Print problems and exit non-zero when there are any."""
    problems = check(ROOT)
    for problem in problems:
        print(problem, file=sys.stderr)
    if problems:
        sys.exit(f"{len(problems)} toolchain/pinning problem(s)")
    print("Toolchain versions and CI pinning come from their single sources.")


if __name__ == "__main__":
    main()
