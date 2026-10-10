#!/usr/bin/env python3
"""Configuration discovery, explicit failures and refusal to touch old data directories."""
import hashlib
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

from cli_test_support import BINARY, ROOT, main, run_cli

BASE_CONFIG_LIMITS = {"max_writers": 16, "max_readers": 512}


def fingerprint(directory):
    """Map every file below ``directory`` to its SHA-256."""
    return {
        str(path.relative_to(directory)): hashlib.sha256(path.read_bytes()).hexdigest()
        for path in directory.rglob("*")
        if path.is_file()
    }


class DataDirectoryCLITest(unittest.TestCase):
    """``redapp config validate`` / ``serve`` against configuration files and data directories."""

    def setUp(self):
        if not BINARY.is_file():
            self.fail(f"{BINARY} is missing; run `make binary` first")
        temp = tempfile.TemporaryDirectory(prefix="redapp-data-cli-")
        self.addCleanup(temp.cleanup)
        self.directory = Path(temp.name)
        self.data = self.directory / "new-data"
        self.config_path = self.directory / "config.json"
        self.config = {
            "schema_version": 1,
            "data_dir": str(self.data),
            "download_limits": dict(BASE_CONFIG_LIMITS),
        }
        self.write_config(self.config)

    def write_config(self, config):
        """Write ``config`` as the JSON configuration file."""
        self.config_path.write_text(json.dumps(config))

    def invoke(self, args, env=None):
        """Run the binary in the temporary directory."""
        return run_cli(args, self.directory, env)

    def validate(self, *args, env=None):
        """Run ``config validate --config <file>`` with extra arguments."""
        return self.invoke(["config", "validate", "--config", str(self.config_path), *args], env)

    def assert_exit(self, result, code, label):
        """Require an exit code and that no data directory was created."""
        self.assertEqual(result.returncode, code, f"{label}: stderr={result.stderr!r}")
        self.assertFalse(self.data.exists(), f"{label}: validation created the data directory")

    def test_validate_never_creates_the_data_directory(self):
        self.assert_exit(self.validate(), 0, "file")
        result = self.invoke(["config", "validate"], {"REDAPP_DATA": str(self.data)})
        self.assert_exit(result, 0, "REDAPP_DATA")

    def test_artifact_size_limits_from_flags_and_environment(self):
        cases = [
            (["--max-artifact-bytes", "4GiB"], {}, 0),
            ([], {"REDAPP_MAX_ARTIFACT_BYTES": "4gb"}, 0),
            (["--max-artifact-bytes", "2TiB"], {}, 1),
            (["--max-artifact-bytes", "4GiB"], {"REDAPP_MAX_ARTIFACT_BYTES": "2TiB"}, 1),
        ]
        for args, env, code in cases:
            with self.subTest(args=args, env=env):
                result = self.validate(*args, env=env)
                self.assert_exit(result, code, f"{args} {env}")
                if code:
                    self.assertIn("max_artifact_bytes", result.stderr)

    def test_writer_and_reader_limits_and_removed_flags(self):
        cases = [
            (["--max-writers", "24", "--max-readers", "768"], {}, 0),
            ([], {"REDAPP_MAX_WRITERS": "24", "REDAPP_MAX_READERS": "768"}, 0),
            (["--max-writers", "1025"], {}, 1),
            (["--max-active-writers", "24"], {}, 1),
            (["--allowed-hosts", "example.com"], {}, 1),
        ]
        for args, env, code in cases:
            with self.subTest(args=args, env=env):
                self.assert_exit(self.validate(*args, env=env), code, f"{args} {env}")

    def test_removed_configuration_fields_are_unknown(self):
        removed_fields = [
            {"allowed_hosts": ["example.com"]},
            {"download_limits": {"max_active_writers": 24}},
        ]
        for removed in removed_fields:
            with self.subTest(removed=removed):
                self.write_config({**self.config, **removed})
                result = self.validate()
                self.assert_exit(result, 1, str(removed))
                self.assertIn("unknown", result.stderr)

    def test_explicit_config_flag_wins_over_missing_environment_path(self):
        missing = str(self.directory / "missing.yaml")
        self.assert_exit(self.validate(env={"REDAPP_CONFIG": missing}), 0, "flag over env")

    def test_missing_explicit_configuration_fails(self):
        missing = str(self.directory / "missing.yaml")
        for args, env in [(["serve", "--config", missing], {}), ([], {"REDAPP_CONFIG": missing})]:
            with self.subTest(args=args, env=env):
                result = self.invoke(args, env)
                self.assert_exit(result, 1, f"{args} {env}")
                self.assertIn("missing.yaml", result.stderr)

    def test_public_url_with_path_is_rejected(self):
        result = self.validate(env={"REDAPP_PUBLIC_URL": "https://example.test/invalid"})
        self.assert_exit(result, 1, "PUBLIC_URL with path")

    def test_duplicate_json_keys_are_rejected(self):
        self.config_path.write_text(json.dumps(self.config)[:-1] + ',"schema_version":1}')
        result = self.validate()
        self.assert_exit(result, 1, "duplicate key")
        self.assertIn("Duplicate", result.stderr)

    def test_old_and_unknown_directories_stay_byte_identical(self):
        # A refused startup must not change any byte, including creating
        # instance.lock or SQLite sidecars.
        released = ROOT / "internal/store/testdata/schema_v10.sql"
        kinds = [(f"schema-{version}", version) for version in range(2, 11)]
        kinds += [("released-0.8.0", "full"), ("unknown", None)]
        for kind, old_schema in kinds:
            with self.subTest(kind=kind):
                data = self.directory / kind
                data.mkdir()
                if old_schema == "full":
                    # The complete released schema 10 with an existing administrator row.
                    db = sqlite3.connect(data / "state.sqlite")
                    db.executescript(released.read_text())
                    db.execute("INSERT INTO admin VALUES(1,'keep',1)")
                    db.commit()
                    db.close()
                elif old_schema is not None:
                    db = sqlite3.connect(data / "state.sqlite")
                    db.executescript(
                        "CREATE TABLE schema_version(version INTEGER NOT NULL);"
                        f"INSERT INTO schema_version VALUES({old_schema});"
                    )
                    db.close()
                else:
                    (data / "keep-me.txt").write_text("unrelated original data")
                (data / "state.sqlite-wal").write_bytes(b"preserve existing WAL bytes")
                (data / "state.sqlite-shm").write_bytes(b"preserve existing SHM bytes")
                before = fingerprint(data)
                # File-free startup must refuse to modify old data as well.
                result = self.invoke([], {"REDAPP_DATA": str(data)})
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn("new empty data directory", result.stderr)
                self.assertEqual(fingerprint(data), before, "refused startup changed files")
                self.assertFalse((data / "instance.lock").exists())

    def test_read_only_data_directory_fails_without_fallback(self):
        # Linux /sys is read-only: initialization must fail, not fall back elsewhere.
        self.write_config({**self.config, "data_dir": "/sys/redapp-permission-test"})
        result = self.invoke(["serve", "--config", str(self.config_path)])
        self.assertEqual(result.returncode, 1, result.stderr)
        stderr = result.stderr.lower()
        self.assertTrue(
            "read-only" in stderr or "permission denied" in stderr,
            f"unexpected failure message: {result.stderr!r}",
        )
        self.assertFalse(self.data.exists())


if __name__ == "__main__":
    main()
