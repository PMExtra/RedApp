#!/usr/bin/env python3
"""Exercise optional configuration, explicit failures and untouched old directories."""
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
binary = str(root / "bin/redapp")
env = {key: value for key, value in os.environ.items() if not key.startswith("REDAPP_")}


def invoke(args, directory, overrides=None):
    return subprocess.run([binary, *args], cwd=directory,
                          env={**env, **(overrides or {})}, capture_output=True,
                          text=True, timeout=10)


def fingerprint(directory):
    return {str(path.relative_to(directory)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in directory.rglob("*") if path.is_file()}


with tempfile.TemporaryDirectory(prefix="redapp-data-cli-") as temp:
    directory = Path(temp)
    config_path = directory / "config.json"
    data = directory / "new-data"
    config = {"schema_version": 1, "data_dir": str(data),
              "download_limits": {"max_writers": 16, "max_readers": 512}}
    config_path.write_text(json.dumps(config))
    result = invoke(["config", "validate", "--config", str(config_path)], directory)
    assert result.returncode == 0 and not data.exists(), result.stderr
    result = invoke(["config", "validate"], directory, {"REDAPP_DATA": str(data)})
    assert result.returncode == 0 and not data.exists(), result.stderr
    # Exercise actual CLI/env size parsing before any data initialization.
    for args, overrides, expected in [
        (["--max-artifact-bytes", "4GiB"], {}, 0),
        ([], {"REDAPP_MAX_ARTIFACT_BYTES": "4gb"}, 0),
        (["--max-artifact-bytes", "2TiB"], {}, 1),
        (["--max-artifact-bytes", "4GiB"], {"REDAPP_MAX_ARTIFACT_BYTES": "2TiB"}, 1),
    ]:
        result = invoke(["config", "validate", "--config", str(config_path), *args], directory, overrides)
        assert result.returncode == expected and not data.exists(), result.stderr
        if expected:
            assert "max_artifact_bytes" in result.stderr
    for args, overrides, expected in [
        (["--max-writers", "24", "--max-readers", "768"], {}, 0),
        ([], {"REDAPP_MAX_WRITERS": "24", "REDAPP_MAX_READERS": "768"}, 0),
        (["--max-writers", "1025"], {}, 1),
        (["--max-active-writers", "24"], {}, 1),
        (["--allowed-hosts", "example.com"], {}, 1),
    ]:
        result = invoke(["config", "validate", "--config", str(config_path), *args], directory, overrides)
        assert result.returncode == expected and not data.exists(), result.stderr
    for removed in [{"allowed_hosts": ["example.com"]}, {"download_limits": {"max_active_writers": 24}}]:
        config_path.write_text(json.dumps({**config, **removed}))
        result = invoke(["config", "validate", "--config", str(config_path)], directory)
        assert result.returncode == 1 and "unknown" in result.stderr and not data.exists(), result.stderr
    config_path.write_text(json.dumps(config))
    # Explicit CLI path wins over a missing environment-selected path.
    result = invoke(["config", "validate", "--config", str(config_path)], directory,
                    {"REDAPP_CONFIG": str(directory / "missing.yaml")})
    assert result.returncode == 0 and not data.exists(), result.stderr
    for args, overrides in [
        (["serve", "--config", str(directory / "missing.yaml")], {}),
        ([], {"REDAPP_CONFIG": str(directory / "missing.yaml")}),
    ]:
        result = invoke(args, directory, overrides)
        assert result.returncode == 1 and "missing.yaml" in result.stderr and not data.exists()
    result = invoke(["config", "validate", "--config", str(config_path)], directory,
                    {"REDAPP_PUBLIC_URL": "https://example.test/invalid"})
    assert result.returncode == 1 and not data.exists()
    config_path.write_text(json.dumps(config)[:-1] + ',"schema_version":1}')
    result = invoke(["config", "validate", "--config", str(config_path)], directory)
    assert result.returncode == 1 and "Duplicate" in result.stderr and not data.exists()
    # Old and unknown directories must be byte-identical after a refused startup,
    # including absence of a new instance.lock or SQLite sidecar.
    for kind, old_schema in [("schema-2", 2), ("schema-3", 3), ("unknown", None)]:
        data = directory / kind
        data.mkdir()
        if old_schema is not None:
            db = sqlite3.connect(data / "state.sqlite")
            db.executescript("CREATE TABLE schema_version(version INTEGER NOT NULL);"
                             f"INSERT INTO schema_version VALUES({old_schema});")
            db.close()
        else:
            (data / "keep-me.txt").write_text("unrelated original data")
        before = fingerprint(data)
        # Restored file-free startup must still refuse to modify old data.
        result = invoke([], directory, {"REDAPP_DATA": str(data)})
        assert result.returncode == 1 and "new empty data directory" in result.stderr, result.stderr
        assert fingerprint(data) == before and not (data / "instance.lock").exists()
    # Linux read-only filesystem: failed initialization must not fall back elsewhere.
    config["data_dir"] = "/sys/redapp-permission-test"
    config_path.write_text(json.dumps(config))
    result = invoke(["serve", "--config", str(config_path)], directory)
    assert result.returncode == 1 and ("read-only" in result.stderr.lower() or "permission denied" in result.stderr.lower())
    assert not (directory / "new-data").exists()
print("CLI optional config, explicit JSON/path failures, invalid origin/duplicate keys, unchanged schema-2/schema-3/unknown directories and permission failure passed.")
