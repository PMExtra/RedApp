#!/usr/bin/env python3
"""Regression tests for scripts/check-toolchain.py on copies of the real files."""
import importlib.util
from pathlib import Path
import shutil
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("check_toolchain", ROOT / "scripts" / "check-toolchain.py")
check_toolchain = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(check_toolchain)


class CheckToolchainTest(unittest.TestCase):
    """Each single-source rule is enforced on a modified copy of the repository files."""

    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix="redapp-toolchain-")
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        for name in ["go.mod", "Dockerfile", "frontend/.node-version"]:
            (self.root / name).parent.mkdir(parents=True, exist_ok=True)
            shutil.copy(ROOT / name, self.root / name)
        shutil.copytree(ROOT / ".github", self.root / ".github")

    def edit(self, name, old, new):
        """Replace one occurrence of ``old`` in a copied file."""
        path = self.root / name
        text = path.read_text()
        self.assertIn(old, text, f"fixture text missing from {name}")
        path.write_text(text.replace(old, new, 1))

    def assert_problem(self, fragment):
        """Require that at least one reported problem mentions ``fragment``."""
        problems = check_toolchain.check(self.root)
        self.assertTrue(problems, "modified copy passed the check")
        self.assertTrue(any(fragment in problem for problem in problems), problems)

    def test_repository_passes(self):
        self.assertEqual(check_toolchain.check(self.root), [])

    def test_go_image_must_match_go_mod(self):
        go_version = (self.root / "go.mod").read_text().split("\ngo ")[1].split()[0]
        self.edit("go.mod", f"go {go_version}", "go 1.0.1")
        self.assert_problem("GO_IMAGE")

    def test_node_image_must_match_node_version_file(self):
        (self.root / "frontend/.node-version").write_text("1.2.3\n")
        self.assert_problem("NODE_IMAGE")

    def test_images_must_be_pinned_by_digest(self):
        self.edit("Dockerfile", "-trixie@sha256:", "-trixie-unpinned@sha256:")
        self.assert_problem("pinned by digest")
        self.edit(".github/installer-check/Dockerfile", "@sha256:", ":latest#")
        self.assert_problem("installer-check")

    def test_workflows_use_version_files(self):
        self.edit(".github/workflows/ci.yml", "go-version-file: go.mod", "go-version: '1.27.1'")
        self.assert_problem("go-version-file")

    def test_actions_must_be_pinned_to_commits(self):
        self.edit(".github/workflows/ci.yml", "actions/checkout@11d5960a326750d5838078e36cf38b85af677262", "actions/checkout@v4")
        self.assert_problem("actions/checkout@v4")

    def test_floating_pulls_and_pull_request_target_are_rejected(self):
        self.edit(".github/workflows/ci.yml", "docker build -t installer-validator", "docker build --pull -t installer-validator")
        self.assert_problem("--pull")
        self.edit(".github/workflows/ci.yml", "  pull_request:", "  pull_request_target:")
        self.assert_problem("pull_request_target")

    def test_pull_on_a_continuation_line_is_rejected(self):
        self.edit(
            ".github/workflows/ci.yml",
            "docker build -t installer-validator",
            "docker build \\\n            --pull -t installer-validator",
        )
        self.assert_problem("--pull")

    def test_checkout_must_not_persist_credentials(self):
        self.edit(".github/workflows/publish.yml", "persist-credentials: false", "persist-credentials: true")
        self.assert_problem("publish.yml: actions/checkout must set persist-credentials: false")
        self.edit(".github/workflows/installer-updates.yml", "          persist-credentials: false\n", "")
        self.assert_problem("installer-updates.yml: actions/checkout")

    def test_qemu_image_must_be_pinned_by_digest(self):
        path = self.root / ".github/workflows/publish.yml"
        text = path.read_text()
        image = next(line for line in text.splitlines() if "image: docker.io/tonistiigi/binfmt" in line)
        path.write_text(text.replace(image + "\n", ""))
        self.assert_problem("setup-qemu-action")

    def test_buildx_must_not_boot_a_floating_buildkit(self):
        self.edit(".github/workflows/verify-published.yml", "driver: docker", "driver: docker-container")
        self.assert_problem("verify-published.yml: docker/setup-buildx-action")
        self.edit(
            ".github/workflows/verify-published.yml",
            "driver: docker-container",
            "driver: docker-container\n          driver-opts: image=moby/buildkit:v0.1.0@sha256:" + "0" * 64,
        )
        self.assertEqual(check_toolchain.check(self.root), [])


if __name__ == "__main__":
    unittest.main()
